// The suggestion list under the query box: operators or
// values for the word at the cursor, the way out ("search as plain text"),
// and a glimpse of the results, which keep updating without that word.
import { el, highlight, plural, trimIndent } from "../format";
import type { Layout } from "../layout";
import type { Completion, ResultItem } from "../protocol.gen";
import { SHEET, toneForLabel, type SheetItem } from "../sheet";
import type { ViewState } from "../state";
import { textTerms } from "./results";

/** Result lines shown under the suggestions. */
const GLIMPSE_LINES = 2;

const GROUP_TITLE: Record<Completion["group"], string> = {
  operator: "Operators starting with",
  author: "Authors matching",
  repo: "Repos matching",
  lang: "Languages matching",
  value: "Values for",
};

export interface CompletionHandlers {
  onPick(index: number): void;
  onSearchAsTyped(): void;
}

/** What the results show while suggestions are open. */
export interface ResultsGlimpse {
  total: number;
  lines: ResultItem[];
}

/** Whether the list is showing: suggestions exist, they weren't dismissed, and the box has focus. */
export function completionsVisible(state: ViewState, input: HTMLInputElement): boolean {
  return state.completionsOpen && state.completions.length > 0 && document.activeElement === input;
}

export function renderCompletions(
  layout: Layout,
  state: ViewState,
  glimpse: ResultsGlimpse | undefined,
  handlers: CompletionHandlers,
): void {
  const { completions, input, shell } = layout;
  const visible = completionsVisible(state, input);
  completions.hidden = !visible;
  shell.classList.toggle("completing", visible);
  input.setAttribute("aria-expanded", String(visible));
  completions.replaceChildren();
  const [first] = state.completions;
  if (!visible || !first) return;

  const group = first.group;
  const typed = typedWord(input.value, first);
  completions.append(
    el("div", { class: "cgroup", role: "presentation" }, `${GROUP_TITLE[group]} “${typed}”`),
    ...state.completions.map((completion, index) => renderOption(completion, index, state.completionIndex, handlers)),
  );
  if (group === "operator") completions.append(renderSearchAsTyped(state, handlers));
  if (glimpse && state.searchText) completions.append(renderGlimpse(state.searchText, glimpse));
}

/** The text the suggestions would replace, as typed. */
function typedWord(text: string, completion: Completion): string {
  const span = completion.insert.edits[0]?.span;
  return span ? text.slice(span.start, span.end) : "";
}

function renderOption(
  completion: Completion,
  index: number,
  selectedIndex: number,
  handlers: CompletionHandlers,
): HTMLElement {
  const selected = index === selectedIndex;
  const sheetItem = completion.group === "operator" ? sheetItemFor(completion.label) : undefined;
  const values = selected && sheetItem ? exampleValues(sheetItem.label) : [];
  const option = el(
    "div",
    {
      class: selected ? "copt selected" : "copt",
      role: "option",
      id: `c${index}`,
      "aria-selected": String(selected),
      "data-testid": "completion",
    },
    el("code", { class: `op tone-${toneForLabel(completion.label)}` }, completion.label),
    el(
      "span",
      { class: "ctext" },
      el("span", { class: "title" }, completion.detail),
      sheetItem ? el("span", { class: "detail" }, sheetItem.detail) : null,
    ),
    ...values.map((value) => el("kbd", { class: "value" }, value)),
    selected ? el("kbd", {}, "Tab") : null,
  );
  // mousedown, not click: picking must not move focus out of the query box.
  option.addEventListener("mousedown", (event) => {
    event.preventDefault();
    handlers.onPick(index);
  });
  return option;
}

/** The operator's cheat-sheet entry, for its longer description and example values. "since:" → since's entry. */
function sheetItemFor(label: string): SheetItem | undefined {
  const operator = label.replace(/:$/, "");
  return SHEET.flatMap((group) => group.items).find((item) => item.operator === operator);
}

/** "since:30d|2w|6m|1y" → ["30d", "2w", "6m", "1y"]; no list for a bare "f:". */
function exampleValues(sheetLabel: string): string[] {
  const values = sheetLabel.slice(sheetLabel.indexOf(":") + 1);
  return values.includes("|") ? values.split("|") : [];
}

/** "Search for timeout and s as plain text ↵". */
function renderSearchAsTyped(state: ViewState, handlers: CompletionHandlers): HTMLElement {
  const terms = textTerms(state.parsed);
  const words = terms.length ? terms : [state.parsed?.raw ?? ""];
  const phrase = words.flatMap((word, index) => (index ? [" and ", el("code", {}, word)] : [el("code", {}, word)]));
  const row = el(
    "div",
    { class: "copt astyped", role: "option", "data-testid": "search-as-typed" },
    el("span", { class: "ctext" }, el("span", { class: "title" }, "Search for ", ...phrase, " as plain text")),
    el("kbd", {}, "↵"),
  );
  row.addEventListener("mousedown", (event) => {
    event.preventDefault();
    handlers.onSearchAsTyped();
  });
  return row;
}

/** "Results for timeout keep updating · 41 matches", and the first lines. */
function renderGlimpse(searchText: string, glimpse: ResultsGlimpse): HTMLElement {
  return el(
    "div",
    { class: "glimpse", "data-testid": "results-glimpse" },
    el(
      "div",
      { class: "cgroup split" },
      el("span", {}, "Results for ", el("code", {}, searchText), " keep updating"),
      el("span", {}, plural(glimpse.total, "match", "matches")),
    ),
    ...glimpse.lines.slice(0, GLIMPSE_LINES).map((item) => {
      if (item.kind !== "line") return null;
      const shown = trimIndent(item.text, item.hits);
      return el(
        "div",
        { class: "row line" },
        el("span", { class: "ln" }, String(item.line)),
        el("code", { class: "text" }, highlight(shown.text, shown.hits)),
      );
    }),
  );
}
