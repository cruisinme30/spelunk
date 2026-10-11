// The notes under the results on a folder Search in Folder picked: why its
// files aren't searched, or, in history, why its commits aren't.
import { button, element } from "../format";
import { send } from "../host";
import type { FolderNote, ParsedQuery, SearchDoneMessage } from "../protocol.gen";

/** Why a folder's files aren't searched, and the setting that would change it, if one does. */
const SKIPPED: Record<NonNullable<FolderNote["skipped"]>, { why: string; fix?: { label: string; setting: string } }> = {
  ignored: {
    why: "isn't searched: .gitignore leaves it out",
    fix: { label: "Include ignored files", setting: "spelunk.index.includeIgnored" },
  },
  excluded: {
    why: "isn't searched: Spelunk's index.exclude setting leaves it out",
    fix: { label: "Change the setting", setting: "spelunk.index.exclude" },
  },
  link: { why: "isn't searched: it's a link to another folder, and Spelunk doesn't follow links" },
  empty: { why: "has no files to search" },
};

/**
 * "dist/ isn't searched: .gitignore leaves it out · Include ignored files",
 * or, in a history search, "plugins/stripe-adapter/ is a Git repo of its
 * own: its files are searched, but not its commits".
 */
export function folderNotes(note: FolderNote, history: boolean): HTMLElement[] {
  const notes: HTMLElement[] = [];
  if (note.skipped) {
    const { why, fix } = SKIPPED[note.skipped];
    notes.push(
      element(
        "div",
        { class: "note", "data-reason": "folder", "data-testid": "folder-note" },
        element("span", {}, element("code", {}, note.folder), ` ${why}`),
        ...(fix
          ? [
              button(
                { class: "btn link", "data-testid": "folder-note-setting" },
                () => {
                  send("settings.open", { setting: fix.setting });
                },
                fix.label,
              ),
            ]
          : []),
      ),
    );
  }
  if (note.gitRepo && history) {
    notes.push(
      element(
        "div",
        { class: "note", "data-reason": "folder", "data-testid": "folder-note" },
        element(
          "span",
          {},
          element("code", {}, note.gitRepo),
          " is a Git repo of its own: its files are searched, but not its commits",
        ),
      ),
    );
  }
  return notes;
}

/** What No results says when a picked folder explains it: its files, or in history its commits, aren't searched. */
export function folderHint(done: SearchDoneMessage | undefined, parsed: ParsedQuery | undefined): string | undefined {
  const notes = done?.folderNotes ?? [];
  if (notes.some((note) => note.skipped !== undefined)) return "That folder isn't searched; see below.";
  if (parsed?.mode === "history" && notes.some((note) => note.gitRepo !== undefined)) {
    return "That folder's commits aren't searched; see below.";
  }
  return undefined;
}
