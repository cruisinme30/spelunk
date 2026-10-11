// VS Code glue for the result menu: its commands (on VS Code's right-click
// menu of a result in the panel, see package.json), what they act through
// (clipboard, browser, pop-ups, save dialog), telling the panel which Git
// host each repo is on, and search links (vscode://spelunk.spelunk/search?q=…).
// What each item does lives in resultActions.ts.
import { homedir } from "node:os";
import { sep } from "node:path";
import * as vscode from "vscode";
import { AllResults } from "./allResults";
import type { SearchController } from "./controller";
import type { Daemon } from "./daemon";
import type { SearchPanel } from "./panel";
import { remotesState } from "./permalinks";
import { type ActionsUi, queryOfSearchLink, ResultActions, searchLink, type ShownResults } from "./resultActions";
import type { ExportFormat } from "./resultText";
import { makeRoot } from "./roots";
import { permalinkHosts } from "./settings";
import { MESSAGE_VERSION } from "./webviewMessages";

/** What the result menu works with. */
interface MenuParts {
  daemon: Daemon;
  controller: SearchController;
  panel: SearchPanel;
  shown: ShownResults;
  log: vscode.OutputChannel;
}

/** The save dialog's name for each export format. */
const FORMAT_NAMES: Record<ExportFormat, string> = { txt: "Text", md: "Markdown", csv: "CSV", json: "JSON" };

/** How long a "Copied …" note stays in the status bar. */
const DONE_MS = 4000;

const configuration = () => vscode.workspace.getConfiguration("spelunk");

/** The workspace folders, as the daemon knows them (as extension.ts's workspaceRoots makes them). */
const roots = () =>
  (vscode.workspace.workspaceFolders ?? [])
    .filter((folder) => folder.uri.scheme === "file")
    .map((folder) => makeRoot(folder.uri.fsPath, folder.name));

/** The files on disk open in editor tabs, for an export's is:open. */
function openFiles(): string[] {
  return vscode.window.tabGroups.all.flatMap((group) =>
    group.tabs.flatMap(({ input }) =>
      input instanceof vscode.TabInputText && input.uri.scheme === "file" ? [input.uri.fsPath] : [],
    ),
  );
}

/** Registers the result menu's commands and the search-link handler, and keeps the panel told each repo's host. */
export function registerResultMenu(parts: MenuParts, extensionId: string): vscode.Disposable[] {
  const { daemon, controller, panel, shown, log } = parts;
  const logError = (error: unknown) => {
    log.appendLine(`[result menu] ${String(error)}`);
  };
  let posts = 0;
  shown.onRestore = () => {
    const post = ++posts;
    remotesState(daemon, roots(), permalinkHosts(configuration())).then((message) => {
      if (post === posts) panel.post("remotes.state", message); // only the newest
    }, logError);
  };
  const actions = createActions(parts, extensionId);
  return [
    ...registerCommands(controller, shown, actions, logError),
    vscode.window.registerUriHandler({
      handleUri: (uri) => {
        // vscode://spelunk.spelunk/search?q=… opens the panel on the query, which then runs.
        const query = queryOfSearchLink(uri.path, uri.query);
        if (query !== undefined) void vscode.commands.executeCommand("spelunk.open", { query });
      },
    }),
  ];
}

/** The menu's actions, on VS Code's clipboard, browser, pop-ups and save dialog. */
function createActions({ daemon, controller, shown, log }: MenuParts, extensionId: string): ResultActions {
  const all = new AllResults(daemon);
  const ui: ActionsUi = {
    copy: async (text, done) => {
      await vscode.env.clipboard.writeText(text);
      vscode.window.setStatusBarMessage(`$(check) ${done}`, DONE_MS);
    },
    openExternal: async (url) => {
      await vscode.env.openExternal(vscode.Uri.parse(url, true));
    },
    inform: (message, settings) => {
      const button = settings ? ["Open Settings"] : [];
      vscode.window.showInformationMessage(message, ...button).then(
        (choice) => {
          if (choice) void vscode.commands.executeCommand("workbench.action.openSettings", "spelunk.permalinks.hosts");
        },
        (error: unknown) => {
          log.appendLine(`[result menu] ${String(error)}`);
        },
      );
    },
    pickRemote: async (remotes) => {
      const items = remotes.map((remote) => ({ label: remote.name, description: remote.url }));
      const picked = await vscode.window.showQuickPick(items, { placeHolder: "Link to which remote?" });
      return picked?.label;
    },
    save: saveExport,
  };
  return new ResultActions(
    {
      backend: daemon,
      item: (ref) => controller.results.find((result) => result.ref === ref),
      selectedRef: () => shown.selectedRef,
      root: (repoId) => roots().find((root) => root.id === repoId),
      hosts: () => permalinkHosts(configuration()),
      query: () => controller.finishedSearchText,
      shown: () => ({ items: controller.results, total: shown.total }),
      all: async (limit) => {
        const query = controller.finishedSearchText;
        if (query === undefined) return [];
        if (controller.results.length >= shown.total) return controller.results.slice(0, limit);
        return all.of(query, openFiles(), limit);
      },
      searchLink: (query) => searchLink(vscode.env.uriScheme, extensionId, query),
      separator: sep,
    },
    ui,
  );
}

/** Asks where to save an export, then writes it there while a notification says it's exporting. */
async function saveExport(name: string, format: ExportFormat, text: () => Promise<string>): Promise<boolean> {
  const folder = vscode.workspace.workspaceFolders?.[0]?.uri ?? vscode.Uri.file(homedir());
  const target = await vscode.window.showSaveDialog({
    defaultUri: vscode.Uri.joinPath(folder, name),
    filters: { [FORMAT_NAMES[format]]: [format] },
    saveLabel: "Export",
  });
  if (!target) return false;
  await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title: "Exporting the results…" },
    async () => {
      await vscode.workspace.fs.writeFile(target, new TextEncoder().encode(await text()));
    },
  );
  const saved = target.path.split("/").pop() ?? name;
  vscode.window.setStatusBarMessage(`$(check) Exported the results to ${saved}`, DONE_MS);
  return true;
}

/** The ref in a menu command's argument: the clicked row's data-vscode-context. */
function refArgument(argument: unknown): string | undefined {
  if (typeof argument !== "object" || argument === null || !("ref" in argument)) return undefined;
  return typeof argument.ref === "string" ? argument.ref : undefined;
}

/** Runs `act` on the ref a menu command got, or, from a key, on none (the selected result). */
const withReference = (act: (ref?: string) => Promise<void>) => (argument?: unknown) => act(refArgument(argument));

/** The menu's commands: each acts on the clicked result, or, from a key, the selected one. */
function registerCommands(
  controller: SearchController,
  shown: ShownResults,
  actions: ResultActions,
  logError: (error: unknown) => void,
): vscode.Disposable[] {
  const open = (where: "current" | "side") => async (argument?: unknown) => {
    const ref = refArgument(argument) ?? shown.selectedRef;
    if (ref === undefined) return;
    await controller.handle({ v: MESSAGE_VERSION, type: "result.open", payload: { ref, where } });
  };
  const commands: Record<string, (argument?: unknown) => Promise<void>> = {
    "spelunk.result.open": open("current"),
    "spelunk.result.openToSide": open("side"),
    "spelunk.result.copy": withReference((ref) => actions.copy(ref)),
    "spelunk.result.copyPath": withReference((ref) => actions.copyPath(ref)),
    "spelunk.result.copyRelativePath": withReference((ref) => actions.copyRelativePath(ref)),
    "spelunk.result.copySha": withReference((ref) => actions.copySha(ref)),
    "spelunk.result.copyShaAndSubject": withReference((ref) => actions.copyShaAndSubject(ref)),
    "spelunk.results.copyAll": () => actions.copyAll(),
    "spelunk.results.exportText": () => actions.export("txt"),
    "spelunk.results.exportMarkdown": () => actions.export("md"),
    "spelunk.results.exportCsv": () => actions.export("csv"),
    "spelunk.results.exportJson": () => actions.export("json"),
    "spelunk.results.copySearchLink": () => actions.copySearchLink(),
  };
  // The link items: a pair per kind of host (which only names the item), and the pair for any other.
  for (const suffix of ["", ".github", ".gitlab", ".bitbucket", ".gitea", ".azure"]) {
    commands[`spelunk.result.copyPermalink${suffix}`] = withReference((ref) => actions.copyPermalink(ref));
    commands[`spelunk.result.openOnRemote${suffix}`] = withReference((ref) => actions.openOnRemote(ref));
  }
  return Object.entries(commands).map(([id, run]) =>
    vscode.commands.registerCommand(id, (argument?: unknown) =>
      run(argument).catch((error: unknown) => {
        logError(error);
        void vscode.window.showErrorMessage(`Spelunk couldn't do that: ${errorText(error)}`);
      }),
    ),
  );
}

const errorText = (error: unknown) => (error instanceof Error ? error.message : String(error));
