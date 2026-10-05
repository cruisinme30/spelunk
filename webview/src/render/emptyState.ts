// The empty box: recent queries and the operator cheat sheet.
import { element } from "../format";
import { OPERATOR_GROUPS, type OperatorGroup, shortName } from "../operators";
import type { ViewState } from "../state";

/** A small clock, marking a recent query. */
const CLOCK_ICON =
  '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>';

/** A small ×, on the recent query under the mouse or selected with ↑↓. */
const REMOVE_ICON =
  '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>';

/** What clicking a recent query, its ×, or a cheat-sheet entry does. */
export interface EmptyStateHandlers {
  onRunRecent: (query: string) => void;
  onRemoveRecent: (query: string) => void;
  onInsert: (snippet: string) => void;
  /** The divider settled at a new width for the recent queries; no width after a reset. */
  onResizeRecent: (width?: number) => void;
}

/** The narrowest the recent queries and the cheat sheet can be dragged. */
const MIN_RECENT_WIDTH = 200;
const MIN_SHEET_WIDTH = 320;
/** How far one ← or → moves the divider. */
const KEY_STEP = 24;

/** Fills `body` with the recent queries and the operator cheat sheet. */
export function renderEmptyState(body: HTMLElement, state: ViewState, handlers: EmptyStateHandlers): void {
  const className = state.sheetOpen ? "empty sheet-open" : "empty";
  const recent = renderRecent(state, handlers);
  const empty = element("div", { class: className }, recent);
  empty.append(renderDivider(empty, recent, handlers.onResizeRecent), renderSheet(handlers.onInsert));
  setRecentWidth(empty, state.recentWidth);
  body.replaceChildren(empty);
}

/** Sizes the recent queries to `width`, or back to the default split when undefined. */
function setRecentWidth(empty: HTMLElement, width: number | undefined): void {
  if (width === undefined) {
    empty.classList.remove("sized");
    empty.style.removeProperty("--recent-width");
  } else {
    empty.classList.add("sized");
    empty.style.setProperty("--recent-width", `${width}px`);
  }
}

/** The widest the recent queries can be while the cheat sheet keeps its minimum. */
function maxRecentWidth(empty: HTMLElement, divider: HTMLElement): number {
  return empty.clientWidth - divider.offsetWidth - MIN_SHEET_WIDTH;
}

/** Keeps a width between the minimums of both sides, given the room the two share. */
function clampWidth(width: number, empty: HTMLElement, divider: HTMLElement): number {
  return Math.round(Math.max(MIN_RECENT_WIDTH, Math.min(width, maxRecentWidth(empty, divider))));
}

/** Tells assistive technology where the divider is: the recent queries' width, and its range. */
function announcePosition(divider: HTMLElement, empty: HTMLElement, recent: HTMLElement): void {
  divider.setAttribute("aria-valuemin", String(MIN_RECENT_WIDTH));
  divider.setAttribute("aria-valuemax", String(Math.max(MIN_RECENT_WIDTH, maxRecentWidth(empty, divider))));
  divider.setAttribute("aria-valuenow", String(Math.round(recent.getBoundingClientRect().width)));
}

/**
 * The line between the recent queries and the cheat sheet: drag it, or
 * focus it and press ← →, to resize; double-click it to reset. It shows
 * only while the two sit side by side.
 */
function renderDivider(empty: HTMLElement, recent: HTMLElement, onResize: (width?: number) => void): HTMLElement {
  const divider = element(
    "div",
    {
      class: "recent-divider",
      role: "separator",
      tabindex: 0,
      "aria-orientation": "vertical",
      "aria-label": "Resize recent queries",
      title: "Drag to resize · double-click to reset",
      "data-testid": "recent-divider",
    },
    element("span", { class: "grip", "aria-hidden": "true" }),
  );
  const resize = (width: number) => {
    const clamped = clampWidth(width, empty, divider);
    setRecentWidth(empty, clamped);
    announcePosition(divider, empty, recent);
    return clamped;
  };
  divider.addEventListener("focus", () => {
    announcePosition(divider, empty, recent);
  });
  divider.addEventListener("pointerdown", (event) => {
    event.preventDefault();
    divider.setPointerCapture(event.pointerId);
    divider.classList.add("active");
    const startX = event.clientX;
    const startWidth = recent.getBoundingClientRect().width;
    let width = startWidth;
    const move = (moved: PointerEvent) => {
      width = resize(startWidth + moved.clientX - startX);
    };
    const end = () => {
      divider.classList.remove("active");
      divider.removeEventListener("pointermove", move);
      divider.removeEventListener("pointerup", end);
      divider.removeEventListener("pointercancel", end);
      if (width !== startWidth) onResize(width);
    };
    divider.addEventListener("pointermove", move);
    divider.addEventListener("pointerup", end);
    divider.addEventListener("pointercancel", end);
  });
  divider.addEventListener("dblclick", () => {
    setRecentWidth(empty, undefined);
    onResize();
  });
  divider.addEventListener("keydown", (event) => {
    const step = { ArrowLeft: -KEY_STEP, ArrowRight: KEY_STEP }[event.key];
    if (step === undefined) return;
    event.preventDefault();
    event.stopPropagation();
    onResize(resize(recent.getBoundingClientRect().width + step));
  });
  return divider;
}

function clockIcon(): HTMLElement {
  const icon = element("span", { class: "icon", "aria-hidden": "true" });
  icon.innerHTML = CLOCK_ICON;
  return icon;
}

/** A recent query: the row runs it, the × at its end takes it off the list. */
function recentRow(query: string, selected: boolean, handlers: EmptyStateHandlers): HTMLElement {
  const run = element(
    "button",
    { type: "button", class: "recent-run", "data-testid": "recent" },
    clockIcon(),
    element("code", {}, query),
  );
  run.addEventListener("click", () => {
    handlers.onRunRecent(query);
  });
  const remove = element("button", {
    type: "button",
    class: "recent-remove",
    title: "Remove from recent",
    "aria-label": `Remove ${query} from recent`,
    "data-testid": "recent-remove",
  });
  remove.innerHTML = REMOVE_ICON;
  remove.addEventListener("click", () => {
    handlers.onRemoveRecent(query);
  });
  return element("div", { class: selected ? "recent-row selected" : "recent-row", "data-recent": query }, run, remove);
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

function renderRecent(state: ViewState, handlers: EmptyStateHandlers): HTMLElement {
  const rows = state.recent.map((query, index) => recentRow(query, index === state.recentIndex, handlers));
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
    const short = shortName(entry);
    const button = element(
      "button",
      { type: "button", class: "sheet-entry", "data-testid": "sheet-op" },
      element(
        "span",
        { class: "sheet-names" },
        element("code", { class: tone }, entry.label),
        short ? element("code", { class: "muted" }, short) : null,
      ),
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
