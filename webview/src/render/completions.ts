// The suggestion list under the query box: operators or
// values for the word at the cursor, the way out ("search as plain text"),
// and a glimpse of the results, which keep updating without that word.
import { element, highlight, plural, trimIndent } from "../format";
import type { Layout } from "../layout";
import { operatorEntry, toneForLabel } from "../operators";
import { textTerms } from "../parsedQuery";
import type { Completion, ResultItem } from "../protocol.gen";
import type { ViewState } from "../state";

/** Result lines shown under the suggestions. */
const GLIMPSE_LINES = 2;

const GROUP_TITLE: Record<Completion["group"], string> = {
  operator: "Operators starting with",
  author: "Authors matching",
  repo: "Repos matching",
  lang: "Languages matching",
  value: "Values for",
};

/** What picking a suggestion, or dismissing them, does. */
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

/** Renders the suggestion list, or hides it when it shouldn't show. */
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
    element("div", { class: "completion-group", role: "presentation" }, `${GROUP_TITLE[group]} “${typed}”`),
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
  // An operator suggestion borrows the reference's description and example values.
  const entry = completion.group === "operator" ? operatorEntry(completion.label.replace(/:$/, "")) : undefined;
  const values = selected && entry ? exampleValues(entry.label) : [];
  const option = element(
    "div",
    {
      class: selected ? "completion selected" : "completion",
      role: "option",
      id: `completion-${index}`,
      "aria-selected": String(selected),
      "data-testid": "completion",
    },
    element("code", { class: `completion-operator tone-${toneForLabel(completion.label)}` }, completion.label),
    element(
      "span",
      { class: "completion-text" },
      element("span", { class: "title" }, completion.detail),
      entry ? element("span", { class: "detail" }, entry.summary) : null,
    ),
    ...values.map((value) => element("kbd", { class: "value" }, value)),
    selected ? element("kbd", {}, "Tab") : null,
  );
  // mousedown, not click: picking must not move focus out of the query box.
  option.addEventListener("mousedown", (event) => {
    event.preventDefault();
    handlers.onPick(index);
  });
  return option;
}

/** "since:today|2h|30d|6m" → ["today", "2h", "30d", "6m"]; no list for a bare "f:". */
function exampleValues(entryLabel: string): string[] {
  const values = entryLabel.slice(entryLabel.indexOf(":") + 1);
  return values.includes("|") ? values.split("|") : [];
}

/** "Search for timeout and s as plain text ↵". */
function renderSearchAsTyped(state: ViewState, handlers: CompletionHandlers): HTMLElement {
  const terms = textTerms(state.parsed);
  const words = terms.length > 0 ? terms : [state.parsed?.raw ?? ""];
  const phrase = words.flatMap((word, index) =>
    index ? [" and ", element("code", {}, word)] : [element("code", {}, word)],
  );
  const row = element(
    "div",
    { class: "completion search-as-typed", role: "option", "data-testid": "search-as-typed" },
    element(
      "span",
      { class: "completion-text" },
      element("span", { class: "title" }, "Search for ", ...phrase, " as plain text"),
    ),
    element("kbd", {}, "↵"),
  );
  row.addEventListener("mousedown", (event) => {
    event.preventDefault();
    handlers.onSearchAsTyped();
  });
  return row;
}

/** "Results for timeout keep updating · 41 matches", and the first lines. */
function renderGlimpse(searchText: string, glimpse: ResultsGlimpse): HTMLElement {
  return element(
    "div",
    { class: "glimpse", "data-testid": "results-glimpse" },
    element(
      "div",
      { class: "completion-group split" },
      element("span", {}, "Results for ", element("code", {}, searchText), " keep updating"),
      element("span", {}, plural(glimpse.total, "match", "matches")),
    ),
    ...glimpse.lines.slice(0, GLIMPSE_LINES).map((item) => {
      if (item.kind !== "line") return null;
      const shown = trimIndent(item.text, item.hits);
      return element(
        "div",
        { class: "row line" },
        element("span", { class: "line-number" }, String(item.line)),
        element("code", { class: "text" }, highlight(shown.text, shown.hits)),
      );
    }),
  );
}
