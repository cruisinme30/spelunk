// VS Code glue: activation, commands, settings, the status bar, file events,
// and opening results in editors. The logic it wires together lives in
// daemon.ts (the daemon process), controller.ts (the search panel's host
// side) and panel.ts (the panel's editor tab).
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import * as vscode from "vscode";
import { CommitDocuments } from "./commitDocuments";
import { RevisionDocuments } from "./revisionDocuments";
import { revisionDiffTitle } from "./revisionPath";
import {
  type ControllerOptions,
  type PersistedState,
  SearchController,
  SHOW_MATCHES,
  STOP_WATCHING,
  type Ui,
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
  PinnedQuery,
  ResultItem,
  Root,
  WelcomeChooseMessage as WelcomeChoice,
  WelcomeStateMessage as WelcomeState,
} from "./protocol.gen";
import type { DocumentLines, LineEdit } from "./replaceEdits";
import { makeRoot } from "./roots";
import {
  closeOnOpen,
  daemonSettings,
  MOVED_SETTINGS,
  movedValue,
  USER_ONLY_SETTINGS,
  pinnedQueries,
  previewContextLines,
  recentQueriesLimit,
  uiSettings,
  watchedQueries,
  watchNotify,
  welcomeSettings,
} from "./settings";
import { UnseenWatches } from "./watches";
import { messageGuard, parseWebviewMessage, type WebviewMessage } from "./webviewMessages";
import { WELCOMED_KEY, WelcomePanel } from "./welcomePanel";

/** The globalState key that keeps the query and recent queries across sessions. */
const STATE_KEY = "spelunk.state";
/** The workspaceState key that keeps what query watches found that hasn't been looked at. */
const UNSEEN_KEY = "spelunk.unseenWatches";
const READY_STATUS = "$(search) Spelunk";
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
  const log = vscode.window.createOutputChannel("Spelunk");
  const logError = (source: string) => (error: unknown) => {
    log.appendLine(`[${source}] ${String(error)}`);
  };
  const daemon = new Daemon({
    binary: daemonBinary(context.extensionPath, context.extensionMode),
    roots: workspaceRoots,
    settings: () => daemonSettings(configuration(), homedir()),
    watches: () => watchedQueries(configuration()),
    log: (line) => {
      log.appendLine(line);
    },
  });
  activeDaemon = daemon;
  const documents = { commits: new CommitDocuments(daemon), revisions: new RevisionDocuments(daemon) };
  const openedStatus = createOpenedStatus();
  // The panel forwards messages only after show(), by which time `controller` below exists.
  const panel = new SearchPanel(
    context.extensionUri,
    (message) => void controller.handle(message).catch(logError("panel")),
    (open) => {
      setContext("spelunk.panelOpen", open);
      if (open) openedStatus.hide();
    },
  );
  const ui = createUi({ panel, daemon, documents, openedStatus, globalState: context.globalState, logError });
  const unseen = new UnseenWatches(context.workspaceState.get<unknown>(UNSEEN_KEY), (list) => {
    context.workspaceState.update(UNSEEN_KEY, list.length > 0 ? list : undefined).then(undefined, logError("watches"));
  });
  const controller = new SearchController(
    daemon,
    ui,
    { ...controllerOptions(), unseen },
    context.globalState.get<PersistedState>(STATE_KEY),
  );
  // The help page's Try runs its example in the search panel; its settings button opens Settings.
  const help = new HelpPanel(context.extensionUri, (message) => {
    if (message.type === "help.try") void vscode.commands.executeCommand("spelunk.open", message.payload);
    else if (message.type === "settings.open") ui.openSettings();
  });
  const welcome = createWelcomePanel(context.extensionUri, daemon, ui, logError("welcome"));
  forwardDaemonEventsToPanel(daemon, panel);

  context.subscriptions.push(
    log,
    panel,
    help,
    welcome,
    openedStatus,
    vscode.workspace.registerTextDocumentContentProvider(CommitDocuments.scheme, documents.commits),
    vscode.workspace.registerTextDocumentContentProvider(RevisionDocuments.scheme, documents.revisions),
    createStatusBar(daemon),
    ...registerCommands(daemon, controller, panel, help),
    vscode.commands.registerCommand("spelunk.showWelcome", () => {
      welcome.show();
    }),
    forwardFileChanges(daemon),
    vscode.workspace.onDidChangeWorkspaceFolders(() => {
      daemon.setRoots(workspaceRoots());
    }),
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (!event.affectsConfiguration("spelunk")) return;
      // A value saved under an old key (by hand, or synced from an older version) moves at once.
      if (touchesMovedSetting(event)) moveSettingsSoon(logError("settings"));
      daemon.updateSettings(daemonSettings(configuration(), homedir()));
      daemon.setWatches(watchedQueries(configuration()));
      if (panel.isOpen) controller.restore(); // sends the new settings to the panel
      welcome.refresh();
    }),
  );

  moveSettingsSoon(logError("settings"));
  daemon.start().catch(logError("daemon start"));
  if (!context.globalState.get<boolean>(WELCOMED_KEY)) {
    welcome.show();
    void context.globalState.update(WELCOMED_KEY, true);
  }
  return { daemon, controller };
}

/** The welcome page, fed the settings it shows and the daemon's indexing progress. */
function createWelcomePanel(
  extensionUri: vscode.Uri,
  daemon: Daemon,
  ui: Ui,
  onError: (error: unknown) => void,
): WelcomePanel {
  const welcome = new WelcomePanel(
    extensionUri,
    {
      settings: (): WelcomeState => welcomeSettings(configuration(), process.platform === "darwin"),
      indexStatus: () => daemon.request("index/status", {}),
    },
    (message) => {
      handleWelcome(message, welcome, onError, () => {
        ui.openSettings();
      });
    },
  );
  daemon.on("progress", (progress) => {
    welcome.indexStatus(progress);
  });
  return welcome;
}

/** The messages the welcome page sends; the others are the search panel's and the help page's. */
const fromWelcome = messageGuard(["welcome.choose", "welcome.shortcut", "welcome.start", "help.open", "settings.open"]);

/**
 * Acts on the welcome page: a choice is saved in the user's settings (the
 * page then shows it, through onDidChangeConfiguration), "Choose my own"
 * opens Keyboard Shortcuts filtered to this extension, and Start opens the
 * search panel.
 */
function handleWelcome(
  message: WebviewMessage,
  welcome: WelcomePanel,
  onError: (error: unknown) => void,
  openSettings: () => void,
): void {
  if (!fromWelcome(message)) return;
  switch (message.type) {
    case "welcome.choose": {
      saveWelcomeChoice(message.payload).catch(onError);
      return;
    }
    case "welcome.shortcut": {
      configuration().update("shortcut.preset", "none", vscode.ConfigurationTarget.Global).then(undefined, onError);
      void vscode.commands.executeCommand("workbench.action.openGlobalKeybindings", "spelunk");
      return;
    }
    case "welcome.start": {
      welcome.dispose();
      void vscode.commands.executeCommand("spelunk.open");
      return;
    }
    case "help.open": {
      void vscode.commands.executeCommand("spelunk.openHelp");
      return;
    }
    case "settings.open": {
      openSettings();
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

/** The moves of old setting keys, one after another: each write starts another, and two at once would race. */
let settingsMoved: Promise<void> = Promise.resolve();

/** Moves the settings saved under old keys once any move already running is done. */
function moveSettingsSoon(onError: (error: unknown) => void): void {
  settingsMoved = settingsMoved.then(() => moveSettings(onError));
}

/**
 * Moves each value saved under a setting's old key (MOVED_SETTINGS) to its
 * new key, in the user's, the workspace's and each folder's settings, and
 * removes the old key there. A value already under the new key wins. A new
 * key only the user's settings may hold (USER_ONLY_SETTINGS) takes no
 * workspace or folder value: the old key is only removed there. A move that
 * fails is reported and the others go on.
 */
async function moveSettings(onError: (error: unknown) => void): Promise<void> {
  const targets = vscode.ConfigurationTarget;
  const scopes = [undefined, ...(vscode.workspace.workspaceFolders ?? []).map((folder) => folder.uri)];
  for (const scope of scopes) {
    const config = vscode.workspace.getConfiguration("spelunk", scope);
    for (const [oldKey, newKey] of Object.entries(MOVED_SETTINGS)) {
      const old = config.inspect(oldKey);
      const now = config.inspect(newKey);
      const values = scope
        ? ([[old?.workspaceFolderValue, now?.workspaceFolderValue, targets.WorkspaceFolder]] as const)
        : ([
            [old?.globalValue, now?.globalValue, targets.Global],
            [old?.workspaceValue, now?.workspaceValue, targets.Workspace],
          ] as const);
      for (const [saved, current, target] of values) {
        if (saved !== undefined) await moveSetting(config, [oldKey, newKey], [saved, current], target).catch(onError);
      }
    }
  }
}

/** Moves one saved value from its old key to its new key at target (see moveSettings). */
async function moveSetting(
  config: vscode.WorkspaceConfiguration,
  [oldKey, newKey]: [string, string],
  [saved, current]: [unknown, unknown],
  target: vscode.ConfigurationTarget,
): Promise<void> {
  const moved = movedValue(oldKey, saved, current);
  const takesIt = target === vscode.ConfigurationTarget.Global || !USER_ONLY_SETTINGS.has(newKey);
  if (moved !== undefined && takesIt) await config.update(newKey, moved, target);
  await config.update(oldKey, undefined, target);
}

/** Whether a settings change touched a key that has moved, so moveSettings has work. */
function touchesMovedSetting(event: vscode.ConfigurationChangeEvent): boolean {
  return Object.keys(MOVED_SETTINGS).some((key) => event.affectsConfiguration(`spelunk.${key}`));
}

/**
 * Writes spelunk.ui.pinnedQueries where the list in use comes from: the
 * workspace's settings when they set it (so a shared list stays shared),
 * otherwise the user's. The settings change then refreshes the panel.
 */
async function savePinned(pinned: PinnedQuery[]): Promise<void> {
  const config = configuration();
  const inWorkspace = config.inspect("ui.pinnedQueries")?.workspaceValue !== undefined;
  const target = inWorkspace ? vscode.ConfigurationTarget.Workspace : vscode.ConfigurationTarget.Global;
  // An empty workspace list stays, as []: removing it would show the user's own pins in its place.
  await config.update("ui.pinnedQueries", pinned.length > 0 || inWorkspace ? pinned : undefined, target);
}

/**
 * Writes spelunk.results.contextLines where its value comes from: the
 * workspace's settings when they set it, otherwise the user's. The settings
 * change then sends it to the daemon and runs the query again.
 */
async function saveContextLines(lines: number): Promise<void> {
  const config = configuration();
  const inWorkspace = config.inspect("results.contextLines")?.workspaceValue !== undefined;
  const target = inWorkspace ? vscode.ConfigurationTarget.Workspace : vscode.ConfigurationTarget.Global;
  await config.update("results.contextLines", lines, target);
}

/** Stops the daemon gracefully when VS Code shuts the extension down. */
export async function deactivate(): Promise<void> {
  await activeDaemon?.stop();
}

/**
 * Where the daemon binary is: bundled per platform, or, when the extension
 * runs from a checkout (development or tests), SPELUNK_DAEMON or the
 * checkout's build. An installed extension never looks outside its folder:
 * next to it is every other extension's.
 */
function daemonBinary(extensionPath: string, mode: vscode.ExtensionMode): string {
  const executable = process.platform === "win32" ? "spelunk-daemon.exe" : "spelunk-daemon";
  const bundled = join(extensionPath, "bin", `${process.platform}-${process.arch}`, executable);
  const fromCheckout = mode !== vscode.ExtensionMode.Production;
  const candidates = [
    fromCheckout ? process.env["SPELUNK_DAEMON"] : undefined,
    bundled,
    join(extensionPath, "bin", executable),
    fromCheckout ? join(extensionPath, "..", "daemon", "bin", executable) : undefined,
  ].filter((path): path is string => !!path);
  return candidates.find((path) => existsSync(path)) ?? bundled;
}

function configuration(): vscode.WorkspaceConfiguration {
  return vscode.workspace.getConfiguration("spelunk");
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
    recentLimit: () => recentQueriesLimit(configuration()),
    pinnedQueries: () => pinnedQueries(configuration()),
    closeOnOpen: () => closeOnOpen(configuration()),
    previewContextLines: () => previewContextLines(configuration()),
    uiSettings: () => uiSettings(configuration()),
    openFiles: openEditorFiles,
    watchNotify: () => watchNotify(configuration()),
  };
}

/** The files on disk open in editor tabs, a diff's changed side included, for is:open. */
function openEditorFiles(): string[] {
  const paths = new Set<string>();
  for (const group of vscode.window.tabGroups.all) {
    for (const { input } of group.tabs) {
      let uri: vscode.Uri | undefined;
      if (input instanceof vscode.TabInputText) uri = input.uri;
      else if (input instanceof vscode.TabInputTextDiff) uri = input.modified;
      if (uri?.scheme === "file") paths.add(uri.fsPath);
    }
  }
  return [...paths];
}

/** What the controller's Ui is built from. */
interface UiParts {
  panel: SearchPanel;
  daemon: Daemon;
  documents: ReadOnlyDocuments;
  openedStatus: vscode.StatusBarItem;
  globalState: vscode.Memento;
  logError: (source: string) => (error: unknown) => void;
}

/** The controller's view of VS Code. */
function createUi({ panel, daemon, documents, openedStatus, globalState, logError }: UiParts): Ui {
  return {
    post: (type, payload) => {
      panel.post(type, payload);
    },
    openTarget: (target, where, preview, item) => openTarget({ target, where, preview, item }, documents),
    hidePanel: () => {
      panel.close();
    },
    openHelp: () => void vscode.commands.executeCommand("spelunk.openHelp"),
    openSettings: () => void vscode.commands.executeCommand("workbench.action.openSettings", "@ext:spelunk.spelunk"),
    restartDaemon: () => void daemon.restart().catch(logError("daemon restart")),
    setContext,
    saveState: (state) => void globalState.update(STATE_KEY, state),
    saveContextLines: (lines) => {
      saveContextLines(lines).catch(logError("settings"));
    },
    savePinned: (pinned) => savePinned(pinned).catch(logError("settings")),
    showOpened: (position, total) => {
      openedStatus.text = `$(search) Opened from search · result ${position} of ${total}`;
      openedStatus.show();
    },
    readDocument,
    applyEdits,
    showWatchMatch: async (message) => vscode.window.showInformationMessage(message, SHOW_MATCHES, STOP_WATCHING),
    showQuery: (query) => {
      vscode.commands.executeCommand("spelunk.open", { query }).then(undefined, logError("watches"));
    },
    saveFiles: (files) => {
      for (const file of files) {
        // As URIs, which normalize the drive letter's case that a path may spell either way.
        const uri = vscode.Uri.file(file).toString();
        const document = vscode.workspace.textDocuments.find((open) => open.uri.toString() === uri);
        if (document?.isDirty) void document.save().then(undefined, logError("save after replace"));
      }
    },
  };
}

/** A file as the editor has it, unsaved changes included; undefined when it isn't a text file that opens. */
async function readDocument(file: string): Promise<DocumentLines | undefined> {
  try {
    const document = await vscode.workspace.openTextDocument(vscode.Uri.file(file));
    return { lineCount: document.lineCount, lineText: (index) => document.lineAt(index).text };
  } catch {
    return undefined;
  }
}

/**
 * Applies a replace's edits as one WorkspaceEdit: a single step that ⌘Z
 * undoes in every file at once. The files stay unsaved, as after a rename.
 */
async function applyEdits(edits: LineEdit[], label: string): Promise<boolean> {
  const edit = new vscode.WorkspaceEdit();
  const metadata: vscode.WorkspaceEditEntryMetadata = { label, needsConfirmation: false };
  for (const { file, line, start, end, newText } of edits) {
    edit.replace(vscode.Uri.file(file), new vscode.Range(line, start, line, end), newText, metadata);
  }
  return vscode.workspace.applyEdit(edit);
}

/**
 * The status bar item that says which search result is open. Clicking it
 * goes back to the results; it hides when the panel opens.
 */
function createOpenedStatus(): vscode.StatusBarItem {
  const item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, STATUS_BAR_PRIORITY);
  item.command = "spelunk.open";
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
  status.command = "spelunk.open";
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
    "spelunk.open": (argument) => {
      panel.show();
      controller.restore(queryArgument(argument));
    },
    "spelunk.openHelp": () => {
      help.show();
    },
    "spelunk.nextResult": () => controller.step(1),
    "spelunk.prevResult": () => controller.step(-1),
    "spelunk.restartDaemon": () => daemon.restart(),
    "spelunk.rebuildIndex": () => pickAndRebuildIndex(daemon),
  };
  // Test builds only: lets end-to-end tests read what the panel and editor show, and act as the panel.
  if (process.env["SPELUNK_TEST"]) {
    commands["spelunk._testState"] = () => testState(daemon, controller, panel);
    commands["spelunk._testPanelMessage"] = async (raw) => {
      const message = parseWebviewMessage(raw);
      if (!message) throw new Error(`not a panel message: ${JSON.stringify(raw)}`);
      await controller.handle(message);
    };
  }
  return Object.entries(commands).map(([id, run]) => vscode.commands.registerCommand(id, run));
}

/** What the spelunk._testState command returns. */
export interface TestState {
  panelOpen: boolean;
  text: string;
  /** The refs of the current search's results, in order. */
  results: string[];
  /** How many of those results carry context lines. */
  withContext: number;
  /** The text of the search `results` belongs to, once it finished; undefined while one runs. */
  searched: string | undefined;
  daemon: DaemonState;
  /** The active editor's document (its URI's scheme and path) and selection, 1-based; undefined without an editor. */
  editor:
    { scheme: string; path: string; line: number; column: number; endLine: number; endColumn: number } | undefined;
}

function testState(daemon: Daemon, controller: SearchController, panel: SearchPanel): TestState {
  const editor = vscode.window.activeTextEditor;
  return {
    panelOpen: panel.isOpen,
    text: controller.text,
    results: controller.results.map((result) => result.ref),
    withContext: controller.results.filter((result) => result.kind === "line" && result.contextAfter?.length).length,
    searched: controller.finishedSearchText,
    daemon: daemon.state,
    editor: editor && {
      scheme: editor.document.uri.scheme,
      path: editor.document.uri.fsPath,
      line: editor.selection.start.line + 1,
      column: editor.selection.start.character + 1,
      endLine: editor.selection.end.line + 1,
      endColumn: editor.selection.end.character + 1,
    },
  };
}

/** The query in spelunk.open's optional `{ query }` argument, which the help page's Try passes. */
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

/** A resolved result to open: where, whether in a preview editor, and the result it came from. */
interface Opening {
  target: OpenTarget;
  where: OpenWhere;
  preview: boolean;
  item: ResultItem | undefined;
}

/**
 * The selection a file target makes: its match, from its line and column to
 * endLine and endColumn when it spans lines. OpenTarget lines and columns are
 * 1-based; VS Code positions are 0-based.
 */
function selectionOf(target: OpenTarget): vscode.Selection {
  const line = Math.max(0, (target.line ?? 1) - 1);
  const column = Math.max(0, (target.column ?? 1) - 1);
  if (target.endLine === undefined) return new vscode.Selection(line, column, line, column + (target.length ?? 0));
  return new vscode.Selection(
    line,
    column,
    Math.max(line, target.endLine - 1),
    Math.max(0, (target.endColumn ?? 1) - 1),
  );
}

/** The read-only documents results open in: commits as diffs, and files as they were at a revision. */
interface ReadOnlyDocuments {
  commits: CommitDocuments;
  revisions: RevisionDocuments;
}

/**
 * Opens a resolved result: a commit as a diff document; a file at a revision read-only, or compared with the file
 * now; or a file with the match selected, all of it when it spans lines.
 */
async function openTarget({ target, where, preview, item }: Opening, documents: ReadOnlyDocuments): Promise<void> {
  const viewColumn = where === "side" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
  if (target.revision && target.path && item && "path" in item) {
    await openAtRevision(documents.revisions, { target, where, preview, item }, item.path);
    return;
  }
  if (target.sha && item) {
    const uri = documents.commits.uriFor(item.ref, target.sha, item.kind === "commit" ? item.subject : undefined);
    const document = await vscode.workspace.openTextDocument(uri);
    await vscode.languages.setTextDocumentLanguage(document, "diff");
    await vscode.window.showTextDocument(document, { viewColumn, preview });
    return;
  }
  if (!target.path) return;
  const selection = selectionOf(target);
  const editor = await vscode.window.showTextDocument(vscode.Uri.file(target.path), { viewColumn, preview, selection });
  editor.revealRange(selection, vscode.TextEditorRevealType.InCenterIfOutsideViewport);
}

/**
 * Opens a result at a revision: its file as it was, read-only, with the match selected; or, for compare, a diff of
 * that file against the file now. A file deleted since has nothing to compare with, so it opens read-only instead.
 * `path` is the file's repo-relative path.
 */
async function openAtRevision(revisions: RevisionDocuments, opening: Opening, path: string): Promise<void> {
  const { target, where, preview, item } = opening;
  const label = target.revisionLabel ?? target.revision?.slice(0, 7) ?? "";
  const uri = revisions.uriFor(item?.ref ?? "", label, path);
  if (where === "compare" && target.path && existsSync(target.path)) {
    await vscode.commands.executeCommand(
      "vscode.diff",
      uri,
      vscode.Uri.file(target.path),
      revisionDiffTitle(label, path),
      {
        preview,
      },
    );
    return;
  }
  const selection = selectionOf(target);
  const viewColumn = where === "side" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
  const editor = await vscode.window.showTextDocument(uri, { viewColumn, preview, selection });
  editor.revealRange(selection, vscode.TextEditorRevealType.InCenterIfOutsideViewport);
}
