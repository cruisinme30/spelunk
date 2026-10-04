// The empty box: recent queries and the operator cheat sheet (mock 4).
import { el } from "../format";
import { SHEET } from "../sheet";
import type { ViewState } from "../state";

export interface EmptyStateHandlers {
  onRunRecent(query: string): void;
  onInsert(snippet: string): void;
}

export function renderEmptyState(body: HTMLElement, state: ViewState, handlers: EmptyStateHandlers): void {
  const recent = el("div", { class: "recent" }, el("div", { class: "section-title" }, "Recent"));
  state.recent.forEach((query, index) => {
    const row = el(
      "button",
      {
        type: "button",
        class: "recent-row" + (index === state.recentIndex ? " selected" : ""),
        "data-recent": query,
        "data-testid": "recent",
      },
      el("code", {}, query),
    );
    row.addEventListener("click", () => handlers.onRunRecent(query));
    recent.append(row);
  });
  if (!state.recent.length) recent.append(el("p", { class: "muted pad" }, "Queries you run show up here."));
  recent.append(
    el(
      "div",
      { class: "card" },
      el("span", { class: "section-title" }, "Combine freely"),
      el("code", {}, "author:jane (timeout OR retry) -f:vendor/ since:6m"),
      el("span", { class: "muted" }, "Spaces mean AND. AND binds tighter than OR."),
    ),
  );

  const groups = el("div", { class: "groups" });
  for (const group of SHEET) {
    const card = el("div", { class: "card" }, el("span", { class: `gname tone-${group.tone}` }, group.name));
    for (const item of group.items) {
      const button = el(
        "button",
        { type: "button", class: "op", "data-testid": "sheet-op" },
        el("code", { class: `tone-${group.tone}` }, item.label),
        el("span", { class: "muted" }, item.detail),
      );
      button.addEventListener("click", () => handlers.onInsert(item.insert));
      card.append(button);
    }
    groups.append(card);
  }
  const sheet = el(
    "div",
    { class: "sheet", "data-testid": "sheet" },
    el(
      "div",
      { class: "section-title split" },
      el("span", {}, "Operators"),
      el("span", { class: "muted" }, "Click one to insert it"),
    ),
    groups,
  );
  body.replaceChildren(el("div", { class: "empty" + (state.sheetOpen ? " sheet-open" : "") }, recent, sheet));
}
