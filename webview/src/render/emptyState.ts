// The empty box: recent queries and the operator cheat sheet.
import { element } from "../format";
import { OPERATOR_GROUPS, type OperatorGroup } from "../operators";
import type { ViewState } from "../state";

/** A small clock, marking a recent query. */
const CLOCK_ICON =
  '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>';

/** What clicking a recent query or a cheat-sheet entry does. */
export interface EmptyStateHandlers {
  onRunRecent: (query: string) => void;
  onInsert: (snippet: string) => void;
}

/** Fills `body` with the recent queries and the operator cheat sheet. */
export function renderEmptyState(body: HTMLElement, state: ViewState, handlers: EmptyStateHandlers): void {
  const className = state.sheetOpen ? "empty sheet-open" : "empty";
  body.replaceChildren(
    element("div", { class: className }, renderRecent(state, handlers.onRunRecent), renderSheet(handlers.onInsert)),
  );
}

function clockIcon(): HTMLElement {
  const icon = element("span", { class: "icon", "aria-hidden": "true" });
  icon.innerHTML = CLOCK_ICON;
  return icon;
}

function recentRow(query: string, selected: boolean, onRunRecent: (query: string) => void): HTMLElement {
  const row = element(
    "button",
    {
      type: "button",
      class: selected ? "recent-row selected" : "recent-row",
      "data-recent": query,
      "data-testid": "recent",
    },
    clockIcon(),
    element("code", {}, query),
  );
  row.addEventListener("click", () => {
    onRunRecent(query);
  });
  return row;
}

/** A reminder of how terms combine, under the recent queries. */
function combineCard(): HTMLElement {
  return element(
    "div",
    { class: "card" },
    element("span", { class: "section-title" }, "Combine freely"),
    element("code", {}, "author:jane (timeout OR retry) -f:vendor/ since:6m"),
    element("span", { class: "muted" }, "Spaces mean AND. AND binds tighter than OR."),
  );
}

function renderRecent(state: ViewState, onRunRecent: (query: string) => void): HTMLElement {
  const rows = state.recent.map((query, index) => recentRow(query, index === state.recentIndex, onRunRecent));
  const placeholder = rows.length === 0 ? element("p", { class: "muted pad" }, "Queries you run show up here.") : null;
  return element(
    "div",
    { class: "recent" },
    element("div", { class: "section-title" }, "Recent"),
    ...rows,
    placeholder,
    combineCard(),
  );
}

/** One group of the cheat sheet: a card of entries that insert their operator when clicked. */
function sheetCard(group: OperatorGroup, onInsert: (snippet: string) => void): HTMLElement {
  const tone = `tone-${group.tone}`;
  const entries = group.entries.map((entry) => {
    const button = element(
      "button",
      { type: "button", class: "sheet-entry", "data-testid": "sheet-op" },
      element("code", { class: tone }, entry.label),
      element("span", { class: "muted" }, entry.summary),
    );
    button.addEventListener("click", () => {
      onInsert(entry.insert);
    });
    return button;
  });
  return element("div", { class: "card" }, element("span", { class: `card-title ${tone}` }, group.name), ...entries);
}

function renderSheet(onInsert: (snippet: string) => void): HTMLElement {
  const title = element(
    "div",
    { class: "section-title spread" },
    element("span", {}, "Operators"),
    element("span", { class: "muted" }, "Click one to insert it"),
  );
  const groups = element("div", { class: "groups" }, ...OPERATOR_GROUPS.map((group) => sheetCard(group, onInsert)));
  return element("div", { class: "sheet", "data-testid": "sheet" }, title, groups);
}
