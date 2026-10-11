// The hidden-results note for file names a word would find by its letters, with match:fuzzy.
import { element } from "../format";
import type { HiddenNote } from "../protocol.gen";

/** "3 file names have these letters in order", then `action` (Show them, which adds match:fuzzy). */
export function fuzzyNamesNote(note: HiddenNote, action: HTMLElement): HTMLElement {
  const names = note.count === 1 ? "1 file name has" : `${note.count.toLocaleString("en-US")} file names have`;
  return element(
    "div",
    { class: "note", "data-reason": note.reason },
    element("span", {}, `${names} these letters in order`),
    action,
  );
}
