// VS Code glue: activation, commands, settings, the status bar, file events,
// and opening results in editors. The logic it wires lives in daemon.ts,
// controller.ts and panel.ts.
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import * as vscode from "vscode";
import { CommitDocuments } from "./commitDocuments";
import { SearchController, type PersistedState, type Ui } from "./controller";
import { Daemon, type DaemonState } from "./daemon";
import { SearchPanel } from "./panel";
import type { FileChange, OpenTarget, OpenWhere, ResultItem } from "./protocol.gen";
import { makeRoot } from "./roots";
import { DEFAULTS, daemonSettings, uiSettings } from "./settings";

const STATE_KEY = "unifiedSearch.state";
const READY_STATUS = "$(search) Unified Search";
/** File events are forwarded to the daemon in batches this far apart. */
const FILE_EVENT_BATCH_MS = 50;

/** The running daemon, kept for deactivate(). */
let activeDaemon: Daemon | undefined;

/** Where the daemon binary is: bundled per platform, or the development build. */
export function daemonBinary(extensionPath: string): string {
  const executable = process.platform === "win32" ? "unified-search-daemon.exe" : "unified-search-daemon";
  const candidates = [
    process.env.UNIFIED_SEARCH_DAEMON,
    join(extensionPath, "bin", `${process.platform}-${process.arch}`, executable),
    join(extensionPath, "bin", executable),
    join(extensionPath, "..", "daemon", "bin", executable), // development checkout
  ].filter((path): path is string => !!path);
  return candidates.find((path) => existsSync(path)) ?? candidates[candidates.length - 1];
}

export async function activate(context: vscode.ExtensionContext): Promise<unknown> {
  const log = vscode.window.createOutputChannel("Unified Search");
  const config = () => vscode.workspace.getConfiguration("unifiedSearch");
  const roots = () =>
    (vscode.workspace.workspaceFolders ?? [])
      .filter((folder) => folder.uri.scheme === "file")
      .map((folder) => makeRoot(folder.uri.fsPath, folder.name));

  const daemon = new Daemon({
    binary: daemonBinary(context.extensionPath),
    roots,
    settings: () => daemonSettings(config(), homedir()),
    log: (line) => log.appendLine(line),
  });
  activeDaemon = daemon;

  const commitDocuments = new CommitDocuments(daemon);
  // The panel calls back only after show(), by which time the controller exists.
  let controller: SearchController | undefined;
  const panel = new SearchPanel(
    context.extensionUri,
    (message) => void controller?.handle(message).catch((error) => log.appendLine(`[panel] ${String(error)}`)),
    (open) => void vscode.commands.executeCommand("setContext", "unifiedSearch.panelOpen", open),
  );
  const ui: Ui = {
    post: (type, payload) => panel.post(type, payload),
    openTarget: (target, where, item) => openTarget(target, where, item, commitDocuments),
    hidePanel: () => panel.close(),
    openHelp: () => void vscode.commands.executeCommand("unifiedSearch.openHelp"),
    openSettings: () =>
      void vscode.commands.executeCommand("workbench.action.openSettings", "@ext:unified-search.unified-search"),
    restartDaemon: () =>
      void daemon.restart().catch((error) => log.appendLine(`[daemon] restart failed: ${String(error)}`)),
    setContext: (key, value) => void vscode.commands.executeCommand("setContext", key, value),
    saveState: (state: PersistedState) => void context.globalState.update(STATE_KEY, state),
  };
  const searchController = new SearchController(
    daemon,
    ui,
    {
      recentLimit: () => config().get("ui.recentQueries", DEFAULTS.recentQueries),
      closeOnOpen: () => config().get("open.closeOnOpen", DEFAULTS.closeOnOpen),
      uiSettings: () => uiSettings(config()),
    },
    context.globalState.get<PersistedState>(STATE_KEY),
  );
  controller = searchController;

  context.subscriptions.push(
    log,
    panel,
    vscode.workspace.registerTextDocumentContentProvider(CommitDocuments.scheme, commitDocuments),
    createStatusBar(daemon, panel),
    ...registerCommands(daemon, searchController, panel, roots),
    forwardFileChanges(daemon),
    vscode.workspace.onDidChangeWorkspaceFolders(() => daemon.setRoots(roots())),
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (!event.affectsConfiguration("unifiedSearch")) return;
      daemon.updateSettings(daemonSettings(config(), homedir()));
      if (panel.isOpen) searchController.restore();
    }),
  );

  daemon.start().catch((error) => log.appendLine(`[daemon] start failed: ${String(error)}`));
  return { daemon, controller: searchController };
}

export async function deactivate(): Promise<void> {
  await activeDaemon?.stop();
}

function statusText(state: DaemonState): string {
  switch (state) {
    case "ok":
      return READY_STATUS;
    case "starting":
    case "restarting":
      return "$(sync~spin) Search starting";
    case "stopped":
      return "$(error) Search stopped";
    case "protocolMismatch":
      return "$(error) Search needs reinstalling";
  }
}

/** The status bar item, plus daemon health banners and indexing progress in the panel. */
function createStatusBar(daemon: Daemon, panel: SearchPanel): vscode.Disposable {
  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  status.command = "unifiedSearch.open";
  daemon.on("state", (state, message) => {
    // "starting" has no banner; every other state maps to the banner of the same name.
    if (state !== "starting") panel.post("banner", { state, message });
    status.text = statusText(state);
    status.tooltip = message;
    status.show();
    if (state === "protocolMismatch" && message) void vscode.window.showErrorMessage(message);
  });
  daemon.on("progress", (progress) => {
    panel.post("index.status", progress);
    if (daemon.state !== "ok") return; // health messages take priority over progress
    const busy = progress.repos.filter(
      (repo) => repo.tree !== "ready" || (repo.history !== "ready" && repo.history !== "off"),
    );
    status.text = busy.length ? `$(sync~spin) Indexing ${busy.length} of ${progress.repos.length}` : READY_STATUS;
  });
  return status;
}

function registerCommands(
  daemon: Daemon,
  controller: SearchController,
  panel: SearchPanel,
  roots: () => ReturnType<typeof makeRoot>[],
): vscode.Disposable[] {
  const commands: Record<string, (...args: any[]) => unknown> = {
    "unifiedSearch.open": (argument?: { query?: string }) => {
      panel.show();
      controller.restore(typeof argument?.query === "string" ? argument.query : undefined);
    },
    "unifiedSearch.nextResult": () => controller.step(1),
    "unifiedSearch.prevResult": () => controller.step(-1),
    "unifiedSearch.restartDaemon": () => daemon.restart(),
    "unifiedSearch.rebuildIndex": async () => {
      const everything = { label: "All repos", repoId: undefined as string | undefined };
      const picks = [
        everything,
        ...roots().map((root) => ({ label: root.name, description: root.path, repoId: root.id as string | undefined })),
      ];
      const pick = await vscode.window.showQuickPick(picks, { placeHolder: "Rebuild the index for…" });
      if (pick) await daemon.request("index/rebuild", pick.repoId ? { repoId: pick.repoId } : {});
    },
  };
  // Test-only: panel state, last results and the active editor (test plan, "Test-only hooks").
  if (process.env.UNIFIED_SEARCH_TEST) {
    commands["unifiedSearch._testState"] = () => {
      const editor = vscode.window.activeTextEditor;
      return {
        panelOpen: panel.isOpen,
        text: controller.text,
        results: controller.results.map((result) => result.ref),
        daemon: daemon.state,
        editor: editor && {
          path: editor.document.uri.fsPath,
          line: editor.selection.start.line + 1,
          column: editor.selection.start.character + 1,
          endColumn: editor.selection.end.character + 1,
        },
      };
    };
  }
  return Object.entries(commands).map(([id, run]) => vscode.commands.registerCommand(id, run));
}

/** Freshness fast path: VS Code's watcher forwards changes ahead of the daemon's own. */
function forwardFileChanges(daemon: Daemon): vscode.Disposable {
  const watcher = vscode.workspace.createFileSystemWatcher("**/*");
  let pendingChanges: FileChange[] = [];
  let flushTimer: NodeJS.Timeout | undefined;
  const queueChange = (type: FileChange["type"]) => (uri: vscode.Uri) => {
    if (uri.scheme !== "file") return;
    pendingChanges.push({ path: uri.fsPath, type });
    flushTimer ??= setTimeout(() => {
      flushTimer = undefined;
      daemon.didChangeFiles(pendingChanges);
      pendingChanges = [];
    }, FILE_EVENT_BATCH_MS);
  };
  return vscode.Disposable.from(
    watcher,
    watcher.onDidChange(queueChange("changed")),
    watcher.onDidCreate(queueChange("created")),
    watcher.onDidDelete(queueChange("deleted")),
    new vscode.Disposable(() => clearTimeout(flushTimer)),
  );
}

/** Opens a resolved result: a file at the match, or a commit as a diff document. */
async function openTarget(
  target: OpenTarget,
  where: OpenWhere,
  item: ResultItem | undefined,
  commitDocuments: CommitDocuments,
): Promise<void> {
  const config = vscode.workspace.getConfiguration("unifiedSearch");
  const viewColumn = where === "side" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
  const preview = config.get("open.preview", DEFAULTS.openPreview);
  if (target.sha && item) {
    const uri = commitDocuments.uriFor(item.ref, target.sha, item.kind === "commit" ? item.subject : undefined);
    const document = await vscode.workspace.openTextDocument(uri);
    await vscode.languages.setTextDocumentLanguage(document, "diff");
    await vscode.window.showTextDocument(document, { viewColumn, preview });
    return;
  }
  if (!target.path) return;
  // OpenTarget lines and columns are 1-based; VS Code positions are 0-based.
  const line = Math.max(0, (target.line ?? 1) - 1);
  const column = Math.max(0, (target.column ?? 1) - 1);
  const selection = new vscode.Selection(line, column, line, column + (target.length ?? 0));
  const editor = await vscode.window.showTextDocument(vscode.Uri.file(target.path), { viewColumn, preview, selection });
  editor.revealRange(selection, vscode.TextEditorRevealType.InCenterIfOutsideViewport);
}
