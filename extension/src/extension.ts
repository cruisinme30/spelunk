// VS Code glue: commands, settings, context keys, the daemon, the panel.
import * as vscode from "vscode";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { SearchController, type PersistedState, type Ui } from "./controller";
import { Daemon } from "./daemon";
import { SearchPanel } from "./panel";
import type { OpenTarget, ResultItem } from "./protocol.gen";
import { makeRoot } from "./roots";
import { daemonSettings, uiSettings } from "./settings";
import { CommitDocuments } from "./commitDocs";

const STATE_KEY = "unifiedSearch.state";

let daemon: Daemon | undefined;

export function daemonBinary(extensionPath: string): string {
  const exe = process.platform === "win32" ? "unified-search-daemon.exe" : "unified-search-daemon";
  const candidates = [
    process.env.UNIFIED_SEARCH_DAEMON,
    join(extensionPath, "bin", `${process.platform}-${process.arch}`, exe),
    join(extensionPath, "bin", exe),
    join(extensionPath, "..", "daemon", "bin", exe), // development checkout
  ].filter((p): p is string => !!p);
  return candidates.find((p) => existsSync(p)) ?? candidates[candidates.length - 1];
}

export async function activate(context: vscode.ExtensionContext): Promise<unknown> {
  const log = vscode.window.createOutputChannel("Unified Search");
  const config = () => vscode.workspace.getConfiguration("unifiedSearch");
  const roots = () => (vscode.workspace.workspaceFolders ?? []).filter((f) => f.uri.scheme === "file").map((f) => makeRoot(f.uri.fsPath, f.name));

  daemon = new Daemon({
    binary: daemonBinary(context.extensionPath),
    roots,
    settings: () => daemonSettings(config(), homedir()),
    log: (l) => log.appendLine(l),
  });
  const d = daemon;

  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  status.command = "unifiedSearch.open";
  context.subscriptions.push(status, log);

  const commitDocs = new CommitDocuments(d);
  context.subscriptions.push(vscode.workspace.registerTextDocumentContentProvider(CommitDocuments.scheme, commitDocs));

  let controller: SearchController;
  const panel = new SearchPanel(
    context.extensionUri,
    (msg) => void controller.handle(msg).catch((e) => log.appendLine(`[panel] ${String(e)}`)),
    (open) => void vscode.commands.executeCommand("setContext", "unifiedSearch.panelOpen", open),
  );
  context.subscriptions.push(panel);

  const ui: Ui = {
    post: (type, payload) => panel.post(type, payload),
    openTarget: (target, where, item) => openTarget(target, where, item, commitDocs),
    hidePanel: () => panel.hide(),
    openHelp: () => void vscode.commands.executeCommand("unifiedSearch.openHelp"),
    openSettings: () => void vscode.commands.executeCommand("workbench.action.openSettings", "@ext:unified-search.unified-search"),
    restartDaemon: () => void d.restart().catch((e) => log.appendLine(`[daemon] restart failed: ${String(e)}`)),
    setContext: (key, value) => void vscode.commands.executeCommand("setContext", key, value),
    saveState: (s: PersistedState) => void context.globalState.update(STATE_KEY, s),
  };
  controller = new SearchController(d, ui, {
    recentLimit: () => config().get("ui.recentQueries", 20),
    closeOnOpen: () => config().get("open.closeOnOpen", true),
    uiSettings: () => uiSettings(config()),
  }, context.globalState.get<PersistedState>(STATE_KEY));

  d.on("state", (s, message) => {
    const banner = s === "ok" ? "ok" : s === "restarting" ? "restarting" : s === "protocolMismatch" ? "protocolMismatch" : s === "stopped" ? "stopped" : undefined;
    if (banner) panel.post("banner", { state: banner, message });
    status.text = s === "ok" ? "$(search) Unified Search" : s === "stopped" ? "$(error) Search stopped" : "$(sync~spin) Search starting";
    status.tooltip = message;
    status.show();
    if (s === "protocolMismatch" && message) void vscode.window.showErrorMessage(message);
  });
  d.on("progress", (p) => {
    panel.post("index.status", p);
    const busy = p.repos.filter((r) => r.tree !== "ready" || (r.history !== "ready" && r.history !== "off"));
    status.text = busy.length ? `$(sync~spin) Indexing ${busy.length} of ${p.repos.length}` : "$(search) Unified Search";
  });

  const reg = (id: string, fn: (...args: any[]) => unknown) => context.subscriptions.push(vscode.commands.registerCommand(id, fn));
  reg("unifiedSearch.open", (arg?: { query?: string }) => {
    panel.show();
    controller.restore(typeof arg?.query === "string" ? arg.query : undefined);
  });
  reg("unifiedSearch.nextResult", () => controller.step(1));
  reg("unifiedSearch.prevResult", () => controller.step(-1));
  reg("unifiedSearch.restartDaemon", () => d.restart());
  reg("unifiedSearch.rebuildIndex", async () => {
    const all = { label: "All repos", repoId: undefined as string | undefined };
    const picks = [all, ...roots().map((r) => ({ label: r.name, description: r.path, repoId: r.id as string | undefined }))];
    const pick = await vscode.window.showQuickPick(picks, { placeHolder: "Rebuild the index for…" });
    if (pick) await d.request("index/rebuild", pick.repoId ? { repoId: pick.repoId } : {});
  });
  // Test-only: panel state, last results, and the active editor (test plan, "Test-only hooks").
  if (context.extensionMode !== vscode.ExtensionMode.Production || process.env.UNIFIED_SEARCH_TEST) {
    reg("unifiedSearch._testState", () => {
      const ed = vscode.window.activeTextEditor;
      return {
        panelOpen: panel.isOpen,
        text: controller.text,
        results: controller.results.map((r) => r.ref),
        daemon: d.state,
        editor: ed && { path: ed.document.uri.fsPath, line: ed.selection.start.line + 1, column: ed.selection.start.character + 1, endColumn: ed.selection.end.character + 1 },
      };
    });
  }

  context.subscriptions.push(
    vscode.workspace.onDidChangeWorkspaceFolders(() => d.setRoots(roots())),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (!e.affectsConfiguration("unifiedSearch")) return;
      d.updateSettings(daemonSettings(config(), homedir()));
      if (panel.isOpen) controller.restore();
    }),
  );

  // Fast path for freshness: VS Code's watcher forwards changes ahead of the daemon's own.
  const watcher = vscode.workspace.createFileSystemWatcher("**/*");
  let pending: { path: string; type: "changed" | "created" | "deleted" }[] = [];
  let timer: NodeJS.Timeout | undefined;
  const queue = (type: "changed" | "created" | "deleted") => (uri: vscode.Uri) => {
    if (uri.scheme !== "file") return;
    pending.push({ path: uri.fsPath, type });
    timer ??= setTimeout(() => {
      timer = undefined;
      d.didChangeFiles(pending.splice(0));
    }, 50);
  };
  context.subscriptions.push(watcher, watcher.onDidChange(queue("changed")), watcher.onDidCreate(queue("created")), watcher.onDidDelete(queue("deleted")));

  d.start().catch((e) => log.appendLine(`[daemon] start failed: ${String(e)}`));
  return { daemon: d, controller };
}

async function openTarget(target: OpenTarget, where: "current" | "side", item: ResultItem | undefined, commits: CommitDocuments): Promise<void> {
  const cfg = vscode.workspace.getConfiguration("unifiedSearch");
  const viewColumn = where === "side" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
  const preview = cfg.get("open.preview", true);
  if (target.sha && target.repoId) {
    const uri = commits.uriFor(target.repoId, target.sha, item?.kind === "commit" ? item.subject : undefined);
    const doc = await vscode.workspace.openTextDocument(uri);
    await vscode.languages.setTextDocumentLanguage(doc, "diff");
    await vscode.window.showTextDocument(doc, { viewColumn, preview });
    return;
  }
  if (!target.path) return;
  const line = Math.max(0, (target.line ?? 1) - 1);
  const col = Math.max(0, (target.column ?? 1) - 1);
  const selection = new vscode.Selection(line, col, line, col + (target.length ?? 0));
  const editor = await vscode.window.showTextDocument(vscode.Uri.file(target.path), { viewColumn, preview, selection });
  editor.revealRange(selection, vscode.TextEditorRevealType.InCenterIfOutsideViewport);
}

export async function deactivate(): Promise<void> {
  await daemon?.stop();
}
