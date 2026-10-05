// The suggestion list under the query box: operators or
// values for the word at the cursor, the way out ("search as plain text"),
// and a glimpse of the results, which keep updating without that word.
import { codeLineRow, codeList, element, plural } from "../format";
import type { Layout } from "../layout";
import { opNameFor, operatorEntry, toneForLabel } from "../operators";
import { textTerms } from "../parsedQuery";
import type { Completion, OpName, ResultItem } from "../protocol.gen";
import type { ViewState } from "../state";

/** Result lines shown under the suggestions. */
const GLIMPSE_LINES = 2;

/** What an operator's value suggestions are called, and a line on what else it takes. */
const VALUE_LISTS: Partial<Record<OpName, { heading: string; hint?: string }>> = {
  since: {
    heading: "Time windows",
    hint: "Or type a number and a unit: 45min, 3h, 10d, 3w, 2m, 1y. m is months; minutes are min.",
  },
  f: { heading: "Paths", hint: String.raw`file: takes a glob (*.go, src/**/*.ts) or a regex (_test\.py$).` },
  repo: { heading: "Repos", hint: "repo: also takes a glob (web-*) or a regex (^pay)." },
  lang: { heading: "Languages" },
  type: { heading: "Result kinds" },
  case: { heading: "Case" },
  count: { heading: "Results per page" },
  author: { heading: "Authors" },
};

/** What picking a suggestion, or dismissing them, does. */
interface CompletionHandlers {
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

  const typed = typedWord(input.value, first);
  // Values replace the whole operator ("since:2" → "since:2h"); the operator comes before the colon.
  const operator = first.group === "operator" ? undefined : typed.slice(0, typed.indexOf(":"));
  const name = operator === undefined ? undefined : opNameFor(operator);
  const list = name === undefined ? undefined : VALUE_LISTS[name];
  completions.append(
    element("div", { class: "completion-group", role: "presentation" }, listHeading(typed, operator, list?.heading)),
    ...state.completions.flatMap((completion, index) => {
      const option = renderOption(completion, { index, selected: index === state.completionIndex, operator }, handlers);
      if (!completion.section) return [option];
      return [element("div", { class: "completion-section", role: "presentation" }, completion.section), option];
    }),
  );
  if (list?.hint)
    completions.append(element("div", { class: "completion-hint", "data-testid": "value-hint" }, list.hint));
  if (first.group === "operator") completions.append(renderSearchAsTyped(state, handlers));
  if (glimpse && state.searchText) completions.append(renderGlimpse(state.searchText, glimpse));
}

/** "Operators starting with “s”", "Time windows", "Time windows matching “2”". */
function listHeading(typed: string, operator: string | undefined, heading = "Values"): string {
  if (operator === undefined) return `Operators starting with “${typed}”`;
  const value = typed.slice(operator.length + 1);
  return value ? `${heading} matching “${value}”` : heading;
}

/** The text the suggestions would replace, as typed. */
function typedWord(text: string, completion: Completion): string {
  const span = completion.insert.edits[0]?.span;
  return span ? text.slice(span.start, span.end) : "";
}

/** Where a suggestion sits in the list, and which operator's value it is, if any. */
interface OptionPlace {
  index: number;
  selected: boolean;
  operator: string | undefined;
}

/** One suggestion: an operator, or a value of `place.operator`. */
function renderOption(
  completion: Completion,
  { index, selected, operator }: OptionPlace,
  handlers: CompletionHandlers,
): HTMLElement {
  // An operator suggestion borrows the reference's description and example values.
  const entry = completion.group === "operator" ? operatorEntry(completion.label.replace(/:$/, "")) : undefined;
  const values = selected && entry ? exampleValues(entry.label) : [];
  // A value takes its operator's color; an operator its own.
  const tone = toneForLabel(operator === undefined ? completion.label : `${operator}:`);
  const secondLine = entry?.summary ?? completion.context;
  const option = element(
    "div",
    {
      class: selected ? "completion selected" : "completion",
      role: "option",
      id: `completion-${index}`,
      "aria-selected": String(selected),
      "data-testid": "completion",
    },
    element("code", { class: `completion-operator tone-${tone}` }, completion.label),
    element(
      "span",
      { class: "completion-text" },
      element("span", { class: "title" }, completion.detail),
      secondLine ? element("span", { class: "detail" }, secondLine) : null,
    ),
    completion.note ? element("span", { class: "completion-note" }, completion.note) : null,
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
  const phrase = codeList(words);
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
      { class: "completion-group spread" },
      element("span", {}, "Results for ", element("code", {}, searchText), " keep updating"),
      element("span", {}, plural(glimpse.total, "match", "matches")),
    ),
    ...glimpse.lines.slice(0, GLIMPSE_LINES).map((item) => {
      if (item.kind !== "line") return null;
      return codeLineRow(item);
    }),
  );
}
