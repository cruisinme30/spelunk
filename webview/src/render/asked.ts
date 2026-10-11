// The line under the box after a question was asked in plain words: what
// was asked and the query it became, a question back when it was too vague
// to search, what Ask needs first (a provider in settings, an API key), or
// why it failed; and the Broaden note under no results.
import { button, element } from "../format";
import type { Layout } from "../layout";
import type { AskedState, ViewState } from "../state";

/** What the line's buttons do. */
export interface AskedHandlers {
  /** Puts the question back in the box, to change it. */
  onEditQuestion: () => void;
  /** Asks the same question again. */
  onAskAgain: () => void;
  /** Searches the question's words as a query, as the box would without Ask. */
  onSearchWords: () => void;
  /** Opens the API key prompt, or Ask's settings. */
  onSetup: (action: "key" | "settings") => void;
}

const SPARKLE =
  '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/><path d="M19 17l.7 2 2 .7-2 .7-.7 2-.7-2-2-.7 2-.7z"/></svg>';

/** A text button on the line. */
function link(label: string, onClick: () => void, testId: string): HTMLButtonElement {
  return button({ class: "btn link", "data-testid": testId }, onClick, label);
}

/** The buttons at the line's end. */
function actions(...buttons: HTMLElement[]): HTMLElement {
  return element("span", { class: "actions" }, ...buttons);
}

/** A thin bar between the line's parts. */
const separator = () => element("span", { class: "sep", "aria-hidden": "true" });

/** Renders the asked line into its region, or clears it when nothing was asked. */
export function renderAsked(layout: Layout, state: ViewState, handlers: AskedHandlers): void {
  const asked = state.asked;
  layout.asked.replaceChildren();
  if (!asked) return;
  const sparkle = element("span", { class: "sparkle", "aria-hidden": "true" });
  sparkle.innerHTML = SPARKLE;
  const [tone, ...parts] = lineParts(asked, handlers);
  layout.asked.append(element("div", { class: `asked ${tone}`, "data-testid": "asked-line" }, sparkle, ...parts));
}

/** The line's tone (a class) and its parts, for what became of the question. */
function lineParts(asked: AskedState, handlers: AskedHandlers): [string, ...(Node | string)[]] {
  const searchWords = () => link("Search these words instead", handlers.onSearchWords, "ask-search-words");
  if (asked.pending) {
    return ["asking", element("span", {}, `Asking: ${asked.question}?`), element("span", { class: "muted" }, "…")];
  }
  if (asked.setup === "off") {
    const turnOn = link(
      "Turn on",
      () => {
        handlers.onSetup("settings");
      },
      "ask-setup",
    );
    return [
      "warn",
      element("span", {}, "Ask is off. Turn it on in Settings to turn a question into a search."),
      actions(turnOn, searchWords()),
    ];
  }
  if (asked.setup === "noKey") {
    const setKey = link(
      "Set API key",
      () => {
        handlers.onSetup("key");
      },
      "ask-setup",
    );
    return [
      "warn",
      element("span", {}, "Ask needs an Anthropic API key to turn your question into a search."),
      actions(setKey, searchWords()),
    ];
  }
  if (asked.clarify !== undefined) {
    return [
      "warn",
      element("span", { class: "label" }, "Spelunk asks"),
      element("span", { class: "question", "data-testid": "asked-clarify" }, asked.clarify),
      actions(link("Edit question", handlers.onEditQuestion, "ask-edit"), searchWords()),
    ];
  }
  if (asked.query === undefined) {
    return [
      "error",
      element("span", {}, `Couldn't ask: ${asked.error ?? "no answer"}`),
      actions(link("Ask again", handlers.onAskAgain, "ask-again"), searchWords()),
    ];
  }
  return [asked.error === undefined ? "ok" : "warn", ...answerParts(asked, handlers)];
}

/** "You asked … · Searched as … · explanation · (problem)" and Edit question / Ask again. */
function answerParts(asked: AskedState, handlers: AskedHandlers): (Node | string)[] {
  const explanation = [asked.explanation ?? "", ...asked.assumptions].filter((text) => text.trim() !== "");
  return [
    element("span", { class: "label" }, "You asked"),
    element("span", { class: "question" }, `${asked.question}?`),
    separator(),
    element("span", { class: "muted" }, "Searched as"),
    element("code", { "data-testid": "asked-query" }, asked.query ?? ""),
    ...(explanation.length > 0
      ? [separator(), element("span", { class: "muted", "data-testid": "asked-explanation" }, explanation.join(" · "))]
      : []),
    ...(asked.error === undefined
      ? []
      : [separator(), element("span", { class: "problem", "data-testid": "asked-error" }, asked.error)]),
    actions(
      link("Edit question", handlers.onEditQuestion, "ask-edit"),
      link("Ask again", handlers.onAskAgain, "ask-again"),
    ),
  ];
}

/** The note under no results for a question: Broaden asks for a looser query. */
export function broadenNote(onBroaden: () => void): HTMLElement {
  return element(
    "div",
    { class: "note", "data-reason": "ask", "data-testid": "broaden-note" },
    element("span", {}, "Try a looser search for your question?"),
    link("Broaden", onBroaden, "broaden"),
  );
}

/** The question a box holds, without its final "?"; undefined when the box isn't a question. */
export function questionIn(text: string): string | undefined {
  const trimmed = text.trim();
  if (!trimmed.endsWith("?") || trimmed.length < 2) return undefined;
  const question = trimmed.slice(0, -1).trim();
  return question === "" ? undefined : question;
}
