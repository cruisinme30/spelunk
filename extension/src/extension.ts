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
import { CommitDocuments } from "./commitDocuments";

const STATE_KEY = "unifiedSearch.state";

/** The running daemon, kept for deactivate(). */
let activeDaemon: Daemon | undefined;

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
  const roots = () =>
    (vscode.workspace.workspaceFolders ?? [])
      .filter((f) => f.uri.scheme === "file")
      .map((f) => makeRoot(f.uri.fsPath, f.name));

  const daemon = new Daemon({
    binary: daemonBinary(context.extensionPath),
    roots,
    settings: () => daemonSettings(config(), homedir()),
    log: (line) => log.appendLine(line),
  });
  activeDaemon = daemon;

  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  status.command = "unifiedSearch.open";
  context.subscriptions.push(status, log);

  const commitDocuments = new CommitDocuments(daemon);
  context.subscriptions.push(
    vscode.workspace.registerTextDocumentContentProvider(CommitDocuments.scheme, commitDocuments),
  );

  let controller: SearchController;
  const panel = new SearchPanel(
    context.extensionUri,
    (msg) => void controller.handle(msg).catch((e) => log.appendLine(`[panel] ${String(e)}`)),
    (open) => void vscode.commands.executeCommand("setContext", "unifiedSearch.panelOpen", open),
  );
  context.subscriptions.push(panel);

  const ui: Ui = {
    post: (type, payload) => panel.post(type, payload),
    openTarget: (target, where, item) => openTarget(target, where, item, commitDocuments),
    hidePanel: () => panel.hide(),
    openHelp: () => void vscode.commands.executeCommand("unifiedSearch.openHelp"),
    openSettings: () =>
      void vscode.commands.executeCommand("workbench.action.openSettings", "@ext:unified-search.unified-search"),
    restartDaemon: () => void daemon.restart().catch((e) => log.appendLine(`[daemon] restart failed: ${String(e)}`)),
    setContext: (key, value) => void vscode.commands.executeCommand("setContext", key, value),
    saveState: (s: PersistedState) => void context.globalState.update(STATE_KEY, s),
  };
  controller = new SearchController(
    daemon,
    ui,
    {
      recentLimit: () => config().get("ui.recentQueries", 20),
      closeOnOpen: () => config().get("open.closeOnOpen", true),
      uiSettings: () => uiSettings(config()),
    },
    context.globalState.get<PersistedState>(STATE_KEY),
  );

  daemon.on("state", (state, message) => {
    // "starting" has no banner; every other state maps to one of the same name.
    if (state !== "starting") panel.post("banner", { state, message });
    status.text =
      state === "ok"
        ? "$(search) Unified Search"
        : state === "stopped"
          ? "$(error) Search stopped"
          : "$(sync~spin) Search starting";
    status.tooltip = message;
    status.show();
    if (state === "protocolMismatch" && message) void vscode.window.showErrorMessage(message);
  });
  daemon.on("progress", (progress) => {
    panel.post("index.status", progress);
    const busy = progress.repos.filter(
      (repo) => repo.tree !== "ready" || (repo.history !== "ready" && repo.history !== "off"),
    );
    status.text = busy.length
      ? `$(sync~spin) Indexing ${busy.length} of ${progress.repos.length}`
      : "$(search) Unified Search";
  });

  const registerCommand = (id: string, run: (...args: any[]) => unknown) =>
    context.subscriptions.push(vscode.commands.registerCommand(id, run));
  registerCommand("unifiedSearch.open", (arg?: { query?: string }) => {
    panel.show();
    controller.restore(typeof arg?.query === "string" ? arg.query : undefined);
  });
  registerCommand("unifiedSearch.nextResult", () => controller.step(1));
  registerCommand("unifiedSearch.prevResult", () => controller.step(-1));
  registerCommand("unifiedSearch.restartDaemon", () => daemon.restart());
  registerCommand("unifiedSearch.rebuildIndex", async () => {
    const all = { label: "All repos", repoId: undefined as string | undefined };
    const picks = [
      all,
      ...roots().map((r) => ({ label: r.name, description: r.path, repoId: r.id as string | undefined })),
    ];
    const pick = await vscode.window.showQuickPick(picks, { placeHolder: "Rebuild the index for…" });
    if (pick) await daemon.request("index/rebuild", pick.repoId ? { repoId: pick.repoId } : {});
  });
  // Test-only: panel state, last results, and the active editor (test plan, "Test-only hooks").
  if (context.extensionMode !== vscode.ExtensionMode.Production || process.env.UNIFIED_SEARCH_TEST) {
    registerCommand("unifiedSearch._testState", () => {
      const ed = vscode.window.activeTextEditor;
      return {
        panelOpen: panel.isOpen,
        text: controller.text,
        results: controller.results.map((r) => r.ref),
        daemon: daemon.state,
        editor: ed && {
          path: ed.document.uri.fsPath,
          line: ed.selection.start.line + 1,
          column: ed.selection.start.character + 1,
          endColumn: ed.selection.end.character + 1,
        },
      };
    });
  }

  context.subscriptions.push(
    vscode.workspace.onDidChangeWorkspaceFolders(() => daemon.setRoots(roots())),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (!e.affectsConfiguration("unifiedSearch")) return;
      daemon.updateSettings(daemonSettings(config(), homedir()));
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
      daemon.didChangeFiles(pending.splice(0));
    }, 50);
  };
  context.subscriptions.push(
    watcher,
    watcher.onDidChange(queue("changed")),
    watcher.onDidCreate(queue("created")),
    watcher.onDidDelete(queue("deleted")),
  );

  daemon.start().catch((e) => log.appendLine(`[daemon] start failed: ${String(e)}`));
  return { daemon, controller };
}

async function openTarget(
  target: OpenTarget,
  where: "current" | "side",
  item: ResultItem | undefined,
  commitDocuments: CommitDocuments,
): Promise<void> {
  const config = vscode.workspace.getConfiguration("unifiedSearch");
  const viewColumn = where === "side" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
  const preview = config.get("open.preview", true);
  if (target.sha && item) {
    const uri = commitDocuments.uriFor(item.ref, target.sha, item.kind === "commit" ? item.subject : undefined);
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
  await activeDaemon?.stop();
}
