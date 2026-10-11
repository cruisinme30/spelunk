// Search in Folder: the Explorer's right-click menu opens the panel on the
// folders (and files) picked there, and says under the results what those
// folders can't give.
import * as vscode from "vscode";
import type { SearchController } from "./controller";
import type { Daemon } from "./daemon";
import {
  type FolderScopeNotes,
  joinPaths,
  pathLabel,
  type PickedPath,
  scopedPaths,
  scopeQuery,
  type ScopedPath,
  withScope,
} from "./folderScope";
import type { SearchPanel } from "./panel";
import type { FolderNote, Root } from "./protocol.gen";

/** How long the status bar note about a folder outside the workspace stays. */
const NOTE_MS = 5000;

/** What Search in Folder works with. */
export interface FolderSearchTargets {
  panel: SearchPanel;
  controller: SearchController;
  daemon: Pick<Daemon, "request">;
  roots: () => Root[];
  /** Where the scope and its notes go, for the searches that start with it. */
  notes: FolderScopeNotes;
}

/**
 * spelunk.searchInFolder, as the Explorer calls it: the right-clicked
 * folder, and every one selected with it. A pick while the box still
 * starts with the last pick's scope joins it, so the results come from
 * either; otherwise the new scope goes in front of what was typed.
 */
export function folderSearchCommand(targets: FolderSearchTargets): (clicked?: unknown, selected?: unknown) => void {
  let last: { scope: string; paths: ScopedPath[] } | undefined;
  return (clicked, selected) => {
    void (async () => {
      const roots = targets.roots();
      const added = scopedPaths(await pickedPaths(clicked, selected), roots);
      if (added.length === 0) {
        vscode.window.setStatusBarMessage("$(info) Spelunk: that folder isn't in an open workspace folder", NOTE_MS);
        return;
      }
      const earlier = last && targets.notes.current(targets.controller.text) === last.scope ? last : undefined;
      const paths = earlier ? joinPaths(earlier.paths, added) : joinPaths([], added);
      const scope = scopeQuery(paths, roots.length > 1);
      last = { scope, paths };
      // The notes go first, so the search the new text starts finds them.
      targets.notes.set(scope, await folderNotes(targets.daemon, paths, roots.length > 1));
      targets.panel.show();
      targets.controller.restore(withScope(targets.controller.text, scope, earlier?.scope), "keepTyping");
    })();
  };
}

/**
 * The Explorer passes the right-clicked item and every selected one; from
 * a keybinding or another command only the first may come. Only local
 * files count: the index has nothing else.
 */
async function pickedPaths(clicked: unknown, selected: unknown): Promise<PickedPath[]> {
  const uris = (Array.isArray(selected) && selected.length > 0 ? selected : [clicked]).filter(
    (uri): uri is vscode.Uri => uri instanceof vscode.Uri && uri.scheme === "file",
  );
  const picked = await Promise.all(
    uris.map(async (uri): Promise<PickedPath[]> => {
      try {
        const { type } = await vscode.workspace.fs.stat(uri);
        return [{ path: uri.fsPath, isFolder: (type & vscode.FileType.Directory) !== 0 }];
      } catch {
        return []; // gone since it was picked
      }
    }),
  );
  return picked.flat();
}

/**
 * What each picked folder can't give, from folder/explain: files that
 * aren't searched, or commits that aren't, being in a Git repo of its own.
 * A folder the daemon can't answer for gets no note.
 */
async function folderNotes(
  daemon: Pick<Daemon, "request">,
  paths: ScopedPath[],
  multiRoot: boolean,
): Promise<FolderNote[]> {
  const notes = await Promise.all(
    paths
      .filter((path) => path.isFolder && path.path !== "")
      .map(async (path): Promise<FolderNote | undefined> => {
        try {
          const { skipped, gitRepo } = await daemon.request("folder/explain", {
            repoId: path.root.id,
            path: path.path,
          });
          if (skipped === undefined && gitRepo === undefined) return undefined;
          const repoLabel =
            gitRepo === undefined
              ? undefined
              : pathLabel({ root: path.root, path: gitRepo, isFolder: true }, multiRoot);
          return {
            folder: pathLabel(path, multiRoot),
            ...(skipped === undefined ? {} : { skipped }),
            ...(repoLabel === undefined ? {} : { gitRepo: repoLabel }),
          };
        } catch {
          return undefined;
        }
      }),
  );
  return notes.filter((note) => note !== undefined);
}
