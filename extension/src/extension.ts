// VS Code glue: activation, commands, settings, the status bar, file events,
// and opening results in editors. The logic it wires together lives in
// daemon.ts (the daemon process), controller.ts (the search panel's host
// side) and panel.ts (the panel's editor tab).
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import * as vscode from "vscode";
import { CommitDocuments } from "./commitDocuments";
import {
  type ControllerOptions,
  type PersistedState,
  SearchController,
  type Ui,
  type WebviewMessage,
} from "./controller";
import { Daemon, type DaemonState } from "./daemon";
import { HelpPanel } from "./helpPanel";
import { SearchPanel } from "./panel";
import type {
  FileChange,
  IndexState,
  IndexStatusResult,
  OpenTarget,
  OpenWhere,
  ResultItem,
  Root,
  WelcomeChooseMsg as WelcomeChoice,
  WelcomeStateMsg as WelcomeState,
} from "./protocol.gen";
import { makeRoot } from "./roots";
import { DEFAULTS, daemonSettings, uiSettings, welcomeSettings } from "./settings";
import { WELCOMED_KEY, WelcomePanel } from "./welcomePanel";

/** The globalState key that keeps the query and recent queries across sessions. */
const STATE_KEY = "unifiedSearch.state";
const READY_STATUS = "$(search) Unified Search";
/** Where the status bar item sits among the other right-aligned items; higher is further left. */
const STATUS_BAR_PRIORITY = 100;
/** File events are forwarded to the daemon in batches this far apart. */
const FILE_EVENT_BATCH_MS = 50;

/** The running daemon, kept for deactivate(). */
let activeDaemon: Daemon | undefined;

/** What activate() returns: the extension's exports, which end-to-end tests can reach. */
export interface ExtensionApi {
  daemon: Daemon;
  controller: SearchController;
}

/** Starts the daemon and registers the panel, commands and listeners. */
export function activate(context: vscode.ExtensionContext): ExtensionApi {
  const log = vscode.window.createOutputChannel("Unified Search");
  const logError = (source: string) => (error: unknown) => {
    log.appendLine(`[${source}] ${String(error)}`);
  };
  const daemon = new Daemon({
    binary: daemonBinary(context.extensionPath),
    roots: workspaceRoots,
    settings: () => daemonSettings(configuration(), homedir()),
    log: (line) => {
      log.appendLine(line);
    },
  });
  activeDaemon = daemon;
  const commitDocuments = new CommitDocuments(daemon);
  const openedStatus = createOpenedStatus();
  // The panel forwards messages only after show(), by which time `controller` below exists.
  const panel = new SearchPanel(
    context.extensionUri,
    (message) => void controller.handle(message).catch(logError("panel")),
    (open) => {
      setContext("unifiedSearch.panelOpen", open);
      if (open) openedStatus.hide();
    },
  );
  const ui = createUi({ panel, daemon, commitDocuments, openedStatus, globalState: context.globalState, logError });
  const controller = new SearchController(
    daemon,
    ui,
    controllerOptions(),
    context.globalState.get<PersistedState>(STATE_KEY),
  );
  // The help page's Try runs its example in the search panel; its settings button opens Settings.
  const help = new HelpPanel(context.extensionUri, (message) => {
    if (message.type === "help.try") void vscode.commands.executeCommand("unifiedSearch.open", message.payload);
    else if (message.type === "settings.open") ui.openSettings();
  });
  const welcome = createWelcomePanel(context.extensionUri, daemon, ui);
  forwardDaemonEventsToPanel(daemon, panel);

  context.subscriptions.push(
    log,
    panel,
    help,
    welcome,
    openedStatus,
    vscode.workspace.registerTextDocumentContentProvider(CommitDocuments.scheme, commitDocuments),
    createStatusBar(daemon),
    ...registerCommands(daemon, controller, panel, help),
    vscode.commands.registerCommand("unifiedSearch.showWelcome", () => {
      welcome.show();
    }),
    forwardFileChanges(daemon),
    vscode.workspace.onDidChangeWorkspaceFolders(() => {
      daemon.setRoots(workspaceRoots());
    }),
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (!event.affectsConfiguration("unifiedSearch")) return;
      daemon.updateSettings(daemonSettings(configuration(), homedir()));
      if (panel.isOpen) controller.restore(); // sends the new settings to the panel
      welcome.refresh();
    }),
  );

  daemon.start().catch(logError("daemon start"));
  if (!context.globalState.get<boolean>(WELCOMED_KEY)) {
    welcome.show();
    void context.globalState.update(WELCOMED_KEY, true);
  }
  return { daemon, controller };
}

/** The welcome page, fed the settings it shows and the daemon's indexing progress. */
function createWelcomePanel(extensionUri: vscode.Uri, daemon: Daemon, ui: Ui): WelcomePanel {
  const welcome = new WelcomePanel(
    extensionUri,
    {
      settings: (): WelcomeState => welcomeSettings(configuration(), process.platform === "darwin"),
      indexStatus: () => daemon.request("index/status", {}),
    },
    (message) => {
      handleWelcome(message, welcome, () => {
        ui.openSettings();
      });
    },
  );
  daemon.on("progress", (progress) => {
    welcome.indexStatus(progress);
  });
  return welcome;
}

/**
 * Acts on the welcome page: a choice is saved in the user's settings (the
 * page then shows it, through onDidChangeConfiguration), "Choose my own"
 * opens Keyboard Shortcuts filtered to this extension, and Start opens the
 * search panel.
 */
function handleWelcome(message: WebviewMessage, welcome: WelcomePanel, openSettings: () => void): void {
  switch (message.type) {
    case "welcome.choose": {
      void saveWelcomeChoice(message.payload);
      return;
    }
    case "welcome.shortcut": {
      void configuration().update("shortcut.preset", "none", vscode.ConfigurationTarget.Global);
      void vscode.commands.executeCommand("workbench.action.openGlobalKeybindings", "unifiedSearch");
      return;
    }
    case "welcome.start": {
      welcome.dispose();
      void vscode.commands.executeCommand("unifiedSearch.open");
      return;
    }
    case "help.open": {
      void vscode.commands.executeCommand("unifiedSearch.openHelp");
      return;
    }
    case "settings.open": {
      openSettings();
      return;
    }
    case "daemon.restart":
    case "help.try":
    case "panel.close":
    case "query.changed":
    case "ready":
    case "result.open":
    case "result.select":
    case "results.more": {
      // The search panel's messages; the page doesn't send them.
      return;
    }
  }
}

/** Saves the welcome page's choices as user settings. */
async function saveWelcomeChoice(choice: WelcomeChoice): Promise<void> {
  const config = configuration();
  const global = vscode.ConfigurationTarget.Global;
  if (choice.preset !== undefined) await config.update("shortcut.preset", choice.preset, global);
  if (choice.historyDepth !== undefined) await config.update("index.historyDepth", choice.historyDepth, global);
  if (choice.symbols !== undefined) await config.update("index.symbols", choice.symbols, global);
}

/** Stops the daemon gracefully when VS Code shuts the extension down. */
export async function deactivate(): Promise<void> {
  await activeDaemon?.stop();
}

/** Where the daemon binary is: bundled per platform, or the development build. */
function daemonBinary(extensionPath: string): string {
  const executable = process.platform === "win32" ? "unified-search-daemon.exe" : "unified-search-daemon";
  const developmentCheckout = join(extensionPath, "..", "daemon", "bin", executable);
  const candidates = [
    process.env["UNIFIED_SEARCH_DAEMON"],
    join(extensionPath, "bin", `${process.platform}-${process.arch}`, executable),
    join(extensionPath, "bin", executable),
    developmentCheckout,
  ].filter((path): path is string => !!path);
  return candidates.find((path) => existsSync(path)) ?? developmentCheckout;
}

function configuration(): vscode.WorkspaceConfiguration {
  return vscode.workspace.getConfiguration("unifiedSearch");
}

/** The workspace folders on disk, as protocol roots. */
function workspaceRoots(): Root[] {
  return (vscode.workspace.workspaceFolders ?? [])
    .filter((folder) => folder.uri.scheme === "file")
    .map((folder) => makeRoot(folder.uri.fsPath, folder.name));
}

function setContext(key: string, value: boolean): void {
  void vscode.commands.executeCommand("setContext", key, value);
}

function controllerOptions(): ControllerOptions {
  return {
    recentLimit: () => configuration().get("ui.recentQueries", DEFAULTS.recentQueries),
    closeOnOpen: () => configuration().get("open.closeOnOpen", DEFAULTS.closeOnOpen),
    uiSettings: () => uiSettings(configuration()),
  };
}

/** What the controller's Ui is built from. */
interface UiParts {
  panel: SearchPanel;
  daemon: Daemon;
  commitDocuments: CommitDocuments;
  openedStatus: vscode.StatusBarItem;
  globalState: vscode.Memento;
  logError: (source: string) => (error: unknown) => void;
}

/** The controller's view of VS Code. */
function createUi({ panel, daemon, commitDocuments, openedStatus, globalState, logError }: UiParts): Ui {
  return {
    post: (type, payload) => {
      panel.post(type, payload);
    },
    openTarget: (target, where, preview, item) => openTarget({ target, where, preview, item }, commitDocuments),
    hidePanel: () => {
      panel.close();
    },
    openHelp: () => void vscode.commands.executeCommand("unifiedSearch.openHelp"),
    openSettings: () =>
      void vscode.commands.executeCommand("workbench.action.openSettings", "@ext:unified-search.unified-search"),
    restartDaemon: () => void daemon.restart().catch(logError("daemon restart")),
    setContext,
    saveState: (state) => void globalState.update(STATE_KEY, state),
    showOpened: (position, total) => {
      openedStatus.text = `$(search) Opened from search · result ${position} of ${total}`;
      openedStatus.show();
    },
  };
}

/**
 * The status bar item that says which search result is open. Clicking it
 * goes back to the results; it hides when the panel opens.
 */
function createOpenedStatus(): vscode.StatusBarItem {
  const item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, STATUS_BAR_PRIORITY);
  item.command = "unifiedSearch.open";
  item.tooltip = "F4: next result · ⇧F4: previous result · Click to go back to the results";
  return item;
}

/** The status bar text for each daemon state. */
function statusText(state: DaemonState): string {
  switch (state) {
    case "ok": {
      return READY_STATUS;
    }
    case "starting":
    case "restarting": {
      return "$(sync~spin) Search starting";
    }
    case "stopped": {
      return "$(error) Search stopped";
    }
    case "protocolMismatch": {
      return "$(error) Search needs reinstalling";
    }
  }
}

function isIndexing(state: IndexState): boolean {
  return state === "queued" || state === "indexing";
}

/** "Indexing 2 of 3" while any repo is indexing, otherwise the ready text. */
function indexingStatusText(progress: IndexStatusResult): string {
  const busy = progress.repos.filter((repo) => isIndexing(repo.tree) || isIndexing(repo.history));
  return busy.length > 0 ? `$(sync~spin) Indexing ${busy.length} of ${progress.repos.length}` : READY_STATUS;
}

/**
 * The status bar item: daemon health, or indexing progress while the daemon
 * is healthy. A protocol mismatch also pops up, since only reinstalling fixes it.
 */
function createStatusBar(daemon: Daemon): vscode.Disposable {
  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, STATUS_BAR_PRIORITY);
  status.command = "unifiedSearch.open";
  daemon.on("state", (state, message) => {
    status.text = statusText(state);
    status.tooltip = message;
    status.show();
    if (state === "protocolMismatch" && message) void vscode.window.showErrorMessage(message);
  });
  daemon.on("progress", (progress) => {
    // Health problems take priority over indexing progress.
    if (daemon.state === "ok") status.text = indexingStatusText(progress);
  });
  return status;
}

/** Shows daemon health in the panel as banners, and relays indexing progress. */
function forwardDaemonEventsToPanel(daemon: Daemon, panel: SearchPanel): void {
  daemon.on("state", (state, message) => {
    // "starting" has no banner; every other state maps to the banner of the same name.
    if (state !== "starting") panel.post("banner", message === undefined ? { state } : { state, message });
  });
  daemon.on("progress", (progress) => {
    panel.post("index.status", progress);
  });
}

/** One handler per command declared in package.json. */
function registerCommands(
  daemon: Daemon,
  controller: SearchController,
  panel: SearchPanel,
  help: HelpPanel,
): vscode.Disposable[] {
  const commands: Record<string, (argument?: unknown) => unknown> = {
    "unifiedSearch.open": (argument) => {
      panel.show();
      controller.restore(queryArgument(argument));
    },
    "unifiedSearch.openHelp": () => {
      help.show();
    },
    "unifiedSearch.nextResult": () => controller.step(1),
    "unifiedSearch.prevResult": () => controller.step(-1),
    "unifiedSearch.restartDaemon": () => daemon.restart(),
    "unifiedSearch.rebuildIndex": () => pickAndRebuildIndex(daemon),
  };
  // Test builds only: lets end-to-end tests read what the panel and editor show.
  if (process.env["UNIFIED_SEARCH_TEST"]) {
    commands["unifiedSearch._testState"] = () => testState(daemon, controller, panel);
  }
  return Object.entries(commands).map(([id, run]) => vscode.commands.registerCommand(id, run));
}

/** What the unifiedSearch._testState command returns. */
interface TestState {
  panelOpen: boolean;
  text: string;
  /** The refs of the current search's results, in order. */
  results: string[];
  daemon: DaemonState;
  /** The active editor's file and selection, 1-based; undefined without an editor. */
  editor: { path: string; line: number; column: number; endColumn: number } | undefined;
}

function testState(daemon: Daemon, controller: SearchController, panel: SearchPanel): TestState {
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
}

/** The query in unifiedSearch.open's optional `{ query }` argument, which the help page's Try passes. */
function queryArgument(argument: unknown): string | undefined {
  if (typeof argument !== "object" || argument === null || !("query" in argument)) return undefined;
  return typeof argument.query === "string" ? argument.query : undefined;
}

interface RepoPick extends vscode.QuickPickItem {
  /** Absent for "All repos". */
  repoId?: string;
}

/** Asks which repo to rebuild (or all), then asks the daemon to rebuild its index. */
async function pickAndRebuildIndex(daemon: Daemon): Promise<void> {
  const picks: RepoPick[] = [
    { label: "All repos" },
    ...workspaceRoots().map((root) => ({ label: root.name, description: root.path, repoId: root.id })),
  ];
  const pick = await vscode.window.showQuickPick(picks, { placeHolder: "Rebuild the index for…" });
  if (!pick) return;
  await daemon.request("index/rebuild", pick.repoId === undefined ? {} : { repoId: pick.repoId });
}

/** Forwards VS Code's file events, so the index catches up before the daemon's own watcher would. */
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
    new vscode.Disposable(() => {
      clearTimeout(flushTimer);
    }),
  );
}

/** Opens a resolved result: a commit as a diff document, or a file with the match selected. */
/** A resolved result to open: where, whether in a preview editor, and the result it came from. */
interface Opening {
  target: OpenTarget;
  where: OpenWhere;
  preview: boolean;
  item: ResultItem | undefined;
}

async function openTarget({ target, where, preview, item }: Opening, commitDocuments: CommitDocuments): Promise<void> {
  const viewColumn = where === "side" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
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
