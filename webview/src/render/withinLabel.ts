// What shows that a code result was found inside a definition: its label on the row, and kind:'s note on the
// matches outside one.
import { element } from "../format";
import type { HiddenNote, ResultItem } from "../protocol.gen";

/** "in function build" after a code line found inside the definitions a query asked for; [] for any other. */
export function withinLabel(item: Extract<ResultItem, { kind: "line" }>): HTMLElement[] {
  const within = item.within;
  if (!within) return [];
  return [
    element(
      "span",
      {
        class: "within",
        title: `Inside the ${within.kind} ${within.name}, which starts on line ${String(within.line)}`,
      },
      "in ",
      element("span", { class: `badge kind-${within.kind}` }, within.kind),
      " ",
      element("code", { class: "name" }, within.name),
    ),
  ];
}

/** "a function", "an interface": a kind of definition with its article. */
function withArticle(kind: string): string {
  return `${kind === "interface" || kind === "other" ? "an" : "a"} ${kind}`;
}

/** Whether `note` counts the matches kind: left out because they were outside every definition it keeps. */
export function outsideDefinitions(note: HiddenNote): boolean {
  return note.reason === "kind" && note.unit === "matches";
}

/** "a method", for kind:method's note. */
export function definitionKind(note: HiddenNote): string {
  return withArticle(note.filter.slice(note.filter.indexOf(":") + 1));
}
