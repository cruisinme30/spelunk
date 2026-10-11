// The hidden-results note for VS Code's files.exclude and search.exclude.
import { element } from "../format";
import type { HiddenNote, SearchDoneMessage } from "../protocol.gen";

/** ["8 hidden by excludes"] for the result summary when VS Code's excludes hid results; [] otherwise. */
export function hiddenByExcludes(done: SearchDoneMessage | undefined): string[] {
  const note = done?.hidden.find((hidden) => hidden.reason === "excludes" && !hidden.shown);
  return note && note.count > 0 ? [`${note.count.toLocaleString("en-US")} hidden by excludes`] : [];
}

/**
 * "3 matches hidden by your VS Code excludes: build, dist", or with
 * excludes:no, "Showing 3 matches your VS Code excludes hide: build, dist",
 * then `action` (Show them or Hide them). `what` counts the results.
 */
export function excludesNote(note: HiddenNote, what: string, action: HTMLElement): HTMLElement {
  const patterns = (note.patterns ?? []).flatMap((pattern, index) => [
    index > 0 ? ", " : "",
    element("code", {}, pattern),
  ]);
  const said = note.shown ? `Showing ${what} your VS Code excludes hide` : `${what} hidden by your VS Code excludes`;
  return element(
    "div",
    { class: "note", "data-reason": note.reason, "data-testid": "excludes-note" },
    element("span", {}, said, patterns.length > 0 ? ": " : "", ...patterns),
    action,
  );
}
