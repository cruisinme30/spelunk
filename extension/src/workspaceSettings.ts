// Following the workspace: the daemon gets its folders, and each folder's
// files.exclude and search.exclude, again whenever they change.
import * as vscode from "vscode";
import { editorExcludes } from "./editorExcludes";
import type { Root, Settings } from "./protocol.gen";

/** What the daemon needs to follow the workspace: its folders, and its settings. */
interface Workspace {
  roots(): Root[];
  settings(): Settings;
}
import { usesExcludeSettings, type ConfigReader } from "./settings";

/** VS Code's own settings that hide files from its search. */
const EXCLUDE_SETTINGS = ["files.exclude", "search.exclude"];

/** `settings` with each root's files.exclude and search.exclude, unless spelunk.search.useExcludeSettings is off. */
export function withEditorExcludes(settings: Settings, config: ConfigReader, roots: Root[]): Settings {
  return usesExcludeSettings(config)
    ? { ...settings, editorExcludes: editorExcludes(roots, folderExcludes) }
    : settings;
}

/** A folder's files.exclude or search.exclude, with the user's, the workspace's and the folder's merged. */
function folderExcludes(folder: string, section: "files" | "search"): unknown {
  return vscode.workspace.getConfiguration(section, vscode.Uri.file(folder)).get<unknown>("exclude");
}

/**
 * Gives the daemon the workspace's folders when they change, and its settings again whenever an exclude setting
 * changes or a folder comes or goes (it brings its own excludes). Excludes apply when searching, so an open panel
 * searches again at once.
 */
export function followWorkspace(
  daemon: { setRoots(roots: Root[]): void; updateSettings(settings: Settings): void },
  workspace: Workspace,
  { panel, controller }: { panel: { isOpen: boolean }; controller: { settingsChanged(): void } },
): vscode.Disposable {
  const update = () => {
    daemon.updateSettings(workspace.settings());
    if (panel.isOpen) controller.settingsChanged();
  };
  return vscode.Disposable.from(
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (EXCLUDE_SETTINGS.some((setting) => event.affectsConfiguration(setting))) update();
    }),
    vscode.workspace.onDidChangeWorkspaceFolders(() => {
      daemon.setRoots(workspace.roots());
      update();
    }),
  );
}
