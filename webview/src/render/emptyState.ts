// The empty box: pinned and recent queries, and the operator cheat sheet.
import { button, element, icon } from "../format";
import { OPERATOR_GROUPS, type OperatorGroup, shortName } from "../operators";
import type { PinnedQuery } from "../protocol.gen";
import { emptyBoxQueries, type ViewState } from "../state";
import { sash } from "./sash";

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

/** A bell: filled on a watched query, an outline on one that can be, crossed out on one that can't. */
const BELL_PATHS =
  '<path d="M6 9a6 6 0 0 1 12 0c0 6 2.5 8 2.5 8h-17S6 15 6 9z"/><path d="M10.3 20.5a2 2 0 0 0 3.4 0" fill="none"/>';
const BELL_ICON = `<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${BELL_PATHS}</svg>`;
const BELL_OUTLINE_ICON = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${BELL_PATHS}</svg>`;
const BELL_OFF_ICON = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${BELL_PATHS}<path d="M3 3l18 18"/></svg>`;

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
  /** Starts or stops watching a pinned query for new commits. */
  onWatch: (query: string, watch: boolean) => void;
  /** The name field closed: with the typed name, or undefined when Esc left the name as it was. */
  onNamed: (query: string, name: string | undefined) => void;
  onInsert: (snippet: string) => void;
  /** The divider settled at a new width for the recent queries; no width after a reset. */
  onResizeRecent: (width?: number) => void;
}

/** The narrowest the recent queries and the cheat sheet can be dragged. */
const MIN_RECENT_WIDTH = 200;
const MIN_SHEET_WIDTH = 320;

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

/**
 * The line between the recent queries and the cheat sheet: drag it, or
 * focus it and press ← →, to resize; double-click it to reset. It shows
 * only while the two sit side by side.
 */
function renderDivider(empty: HTMLElement, recent: HTMLElement, onResize: (width?: number) => void): HTMLElement {
  const divider: HTMLElement = sash({
    className: "recent-divider",
    label: "Resize recent queries",
    testId: "recent-divider",
    pane: recent,
    min: MIN_RECENT_WIDTH,
    max: () => empty.clientWidth - divider.offsetWidth - MIN_SHEET_WIDTH,
    apply: (width) => {
      setRecentWidth(empty, width);
    },
    onSettle: onResize,
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

/** "2 new": the commits a watch found since the query last ran. */
function unseenBadge(state: ViewState, query: string): HTMLElement | null {
  const count = state.unseen.get(query)?.count ?? 0;
  if (count === 0) return null;
  return element(
    "span",
    {
      class: "unseen",
      title: `${count === 1 ? "1 commit" : `${count} commits`} matched since you last ran it`,
      "data-testid": "pinned-unseen",
    },
    `${count} new`,
  );
}

/**
 * A pinned query's bell: stops a watch, starts one on a query that searches
 * history, and on any other query only says why it can't. That one is
 * aria-disabled rather than disabled, so it still takes focus and its
 * tooltip still shows. A watched bell always shows; the others show like
 * the row's other buttons.
 */
function watchAction({ query, name, watch }: PinnedQuery, state: ViewState, handlers: EmptyStateHandlers): HTMLElement {
  const label = name ?? query;
  if (watch) {
    const stop = rowAction(
      {
        title: "Watching: tells you when new commits match. Click to stop.",
        label: `Stop watching ${label}`,
        testId: "pinned-watch",
        svg: BELL_ICON,
      },
      () => {
        handlers.onWatch(query, false);
      },
    );
    stop.classList.add("watching");
    stop.setAttribute("aria-pressed", "true");
    return stop;
  }
  if (!state.watchable.has(query)) {
    const cannot = rowAction(
      {
        title: "Only history queries can be watched: add author:, msg: or type:commit.",
        label: `Watch ${label} for new commits (history queries only)`,
        testId: "pinned-watch",
        svg: BELL_OFF_ICON,
      },
      () => {},
    );
    cannot.setAttribute("aria-disabled", "true");
    return cannot;
  }
  const start = rowAction(
    {
      title: "Watch: tell me when new commits match",
      label: `Watch ${label} for new commits`,
      testId: "pinned-watch",
      svg: BELL_OUTLINE_ICON,
    },
    () => {
      handlers.onWatch(query, true);
    },
  );
  start.setAttribute("aria-pressed", "false");
  return start;
}

/**
 * A pinned query: its name, if it has one, then the query and how many new
 * commits its watch found; the bell watches it, the pencil renames it and
 * the star unpins it.
 */
function pinnedRow(pin: PinnedQuery, selected: boolean, state: ViewState, handlers: EmptyStateHandlers): HTMLElement {
  const { query, name } = pin;
  const run = button(
    { class: "recent-run", "data-testid": "pinned", title: name ? query : undefined },
    () => {
      handlers.onRunRecent(query);
    },
    icon(STAR_ICON),
    name ? element("span", { class: "pinned-name" }, name) : null,
    element("code", { class: name ? "muted" : undefined }, query),
    unseenBadge(state, query),
  );
  return queryRow(
    query,
    selected,
    [
      run,
      watchAction(pin, state, handlers),
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
function namingRow(pin: PinnedQuery, selected: boolean, state: ViewState, handlers: EmptyStateHandlers): HTMLElement {
  const { query, name } = pin;
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
    const renamed: PinnedQuery = { query, ...(kept ? { name: kept } : {}), ...(pin.watch ? { watch: true } : {}) };
    row.replaceWith(pinnedRow(renamed, row.classList.contains("selected"), state, handlers));
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
      ? namingRow(entry, selected(entry.query), state, handlers)
      : pinnedRow(entry, selected(entry.query), state, handlers),
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
