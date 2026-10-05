// The empty box: pinned and recent queries, and the operator cheat sheet.
import { button, element, icon } from "../format";
import { OPERATOR_GROUPS, type OperatorGroup, shortName } from "../operators";
import type { PinnedQuery } from "../protocol.gen";
import { emptyBoxQueries, type ViewState } from "../state";

/** A small clock, marking a recent query. */
const CLOCK_ICON =
  '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>';

/** A star: filled on a pinned query, an outline on a recent query's pin button. */
const STAR_PATH = "M12 3.5l2.6 5.3 5.9.9-4.3 4.1 1 5.8-5.2-2.7-5.2 2.7 1-5.8-4.3-4.1 5.9-.9z";
const STAR_ICON = `<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"><path d="${STAR_PATH}"/></svg>`;
const STAR_OUTLINE_ICON = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="${STAR_PATH}"/></svg>`;

/** A pencil, on a pinned query's rename button. */
const RENAME_ICON =
  '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h4L19 9l-4-4L4 16z"/><path d="M13.5 6.5l4 4"/></svg>';

/** A small ×, on the recent query under the mouse or selected with ↑↓. */
const REMOVE_ICON =
  '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>';

/** What clicking a pinned or recent query, its buttons, or a cheat-sheet entry does. */
export interface EmptyStateHandlers {
  onRunRecent: (query: string) => void;
  onRemoveRecent: (query: string) => void;
  onPin: (query: string) => void;
  onUnpin: (query: string) => void;
  /** Opens the name field on a pinned query. */
  onRename: (query: string) => void;
  /** The name field closed: with the typed name, or undefined when Esc left the name as it was. */
  onNamed: (query: string, name: string | undefined) => void;
  onInsert: (snippet: string) => void;
  /** The divider settled at a new width for the recent queries; no width after a reset. */
  onResizeRecent: (width?: number) => void;
}

/** The narrowest the recent queries and the cheat sheet can be dragged. */
const MIN_RECENT_WIDTH = 200;
const MIN_SHEET_WIDTH = 320;
/** How far one ← or → moves the divider. */
const KEY_STEP = 24;

/** Fills `body` with the pinned and recent queries and the operator cheat sheet. */
export function renderEmptyState(body: HTMLElement, state: ViewState, handlers: EmptyStateHandlers): void {
  const recent = renderRecent(state, handlers);
  const empty = element("div", { class: "empty" }, recent);
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

/** What a row's small button shows: its tooltip, its label for screen readers, its test id and its icon. */
interface RowActionLook {
  title: string;
  label: string;
  testId: string;
  svg: string;
}

/** A small button at the end of a row, shown on the row under the mouse or selected with ↑↓. */
function rowAction({ title, label, testId, svg }: RowActionLook, onClick: () => void): HTMLButtonElement {
  const action = button({ class: "recent-action", title, "aria-label": label, "data-testid": testId }, onClick);
  action.innerHTML = svg;
  return action;
}

/** A row of the empty box; `selected` is the one ↵ runs. */
function queryRow(query: string, selected: boolean, children: HTMLElement[], extraClass = ""): HTMLElement {
  const classes = ["recent-row", extraClass, selected ? "selected" : ""].filter(Boolean).join(" ");
  return element("div", { class: classes, "data-recent": query }, ...children);
}

/** A recent query: the row runs it, the star pins it, the × takes it off the list. */
function recentRow(query: string, selected: boolean, handlers: EmptyStateHandlers): HTMLElement {
  const run = button(
    { class: "recent-run", "data-testid": "recent" },
    () => {
      handlers.onRunRecent(query);
    },
    icon(CLOCK_ICON),
    element("code", {}, query),
  );
  const pin = { title: "Pin", label: `Pin ${query}`, testId: "recent-pin", svg: STAR_OUTLINE_ICON };
  const remove = {
    title: "Remove from recent",
    label: `Remove ${query} from recent`,
    testId: "recent-remove",
    svg: REMOVE_ICON,
  };
  return queryRow(query, selected, [
    run,
    rowAction(pin, () => {
      handlers.onPin(query);
    }),
    rowAction(remove, () => {
      handlers.onRemoveRecent(query);
    }),
  ]);
}

/** A pinned query: its name, if it has one, then the query; the pencil renames it and the star unpins it. */
function pinnedRow({ query, name }: PinnedQuery, selected: boolean, handlers: EmptyStateHandlers): HTMLElement {
  const run = button(
    { class: "recent-run", "data-testid": "pinned", title: name ? query : undefined },
    () => {
      handlers.onRunRecent(query);
    },
    icon(STAR_ICON),
    name ? element("span", { class: "pinned-name" }, name) : null,
    element("code", { class: name ? "muted" : undefined }, query),
  );
  return queryRow(
    query,
    selected,
    [
      run,
      rowAction(
        { title: "Rename", label: `Rename ${name ?? query}`, testId: "pinned-rename", svg: RENAME_ICON },
        () => {
          handlers.onRename(query);
        },
      ),
      rowAction({ title: "Unpin", label: `Unpin ${name ?? query}`, testId: "pinned-unpin", svg: STAR_ICON }, () => {
        handlers.onUnpin(query);
      }),
    ],
    "pinned",
  );
}

/**
 * A pinned query's name field: ↵ or leaving it keeps the name typed (none
 * when blank), Esc keeps the name it had. Closing turns the row back into a
 * pinned row in place, so a click that moved the focus elsewhere still lands.
 */
function namingRow({ query, name }: PinnedQuery, selected: boolean, handlers: EmptyStateHandlers): HTMLElement {
  const field = element("input", {
    class: "pinned-name-field",
    type: "text",
    placeholder: "Name it (optional)",
    "aria-label": `Name for ${query}`,
    spellcheck: "false",
    "data-testid": "pinned-name",
  });
  field.value = name ?? "";
  let closed = false;
  const close = (named?: string) => {
    if (closed) return;
    closed = true;
    const kept = named === undefined ? name : named.trim() || undefined;
    row.replaceWith(pinnedRow(kept ? { query, name: kept } : { query }, row.classList.contains("selected"), handlers));
    handlers.onNamed(query, named);
  };
  field.addEventListener("keydown", (event) => {
    if (event.isComposing) return;
    if (event.key === "Enter") close(field.value);
    else if (event.key === "Escape") close();
    else return;
    event.preventDefault();
    event.stopPropagation();
  });
  field.addEventListener("blur", () => {
    close(field.value);
  });
  const row = queryRow(
    query,
    selected,
    [
      icon(STAR_ICON),
      field,
      element("code", { class: "muted" }, query),
      element("span", { class: "muted pinned-name-hint" }, "↵ save · esc skip"),
    ],
    "pinned naming",
  );
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

/** The pinned queries, if any, then the recent ones not pinned, then the combine card. */
function renderRecent(state: ViewState, handlers: EmptyStateHandlers): HTMLElement {
  const rows = emptyBoxQueries(state);
  const selected = (query: string) => rows[state.recentIndex] === query;
  const pinned = state.pinned.map((entry) =>
    entry.query === state.naming
      ? namingRow(entry, selected(entry.query), handlers)
      : pinnedRow(entry, selected(entry.query), handlers),
  );
  const recent = rows.slice(pinned.length).map((query) => recentRow(query, selected(query), handlers));
  const placeholder = rows.length === 0 ? element("p", { class: "muted pad" }, "Queries you run show up here.") : null;
  return element(
    "div",
    { class: "recent" },
    pinned.length > 0 ? element("div", { class: "section-title" }, "Pinned") : null,
    ...pinned,
    recent.length > 0 || pinned.length === 0 ? element("div", { class: "section-title" }, "Recent") : null,
    ...recent,
    placeholder,
    combineCard(),
  );
}

/** One group of the cheat sheet: a card of entries that insert their operator when clicked. */
function sheetCard(group: OperatorGroup, onInsert: (snippet: string) => void): HTMLElement {
  const tone = `tone-${group.tone}`;
  const entries = group.entries.map((entry) => {
    const short = shortName(entry);
    return button(
      { class: "sheet-entry", "data-testid": "sheet-op" },
      () => {
        onInsert(entry.insert);
      },
      element(
        "span",
        { class: "sheet-names" },
        element("code", { class: tone }, entry.label),
        short ? element("code", { class: "muted" }, short) : null,
      ),
      element("span", { class: "muted" }, entry.summary),
    );
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
