// The order control in a results section's title: a menu of how current
// files are sorted (best match, path order, recently changed) and an arrow
// for the direction, plus what a result says about when its file changed.
import { button, element, timeAgo } from "../format";
import type { Globals, ResultItem, ResultOrder } from "../protocol.gen";

/** An order without its direction. */
type BaseOrder = "best" | "path" | "recent";

/** Each order's name in the menu, and what it does. */
const ORDERS: Record<BaseOrder, { name: string; detail: string }> = {
  best: { name: "Best match first", detail: "Definitions and name matches first; tests last" },
  path: { name: "Path order", detail: "Repo by repo, then by path and line" },
  recent: { name: "Recently changed", detail: "By when each file last changed" },
};

/** What the arrow says for path and recent order: ascending, then descending. */
const DIRECTIONS: Record<Exclude<BaseOrder, "best">, [string, string]> = {
  path: ["A to Z", "Z to A"],
  recent: ["Oldest first", "Newest first"],
};

/** An order's base and direction; plain path runs A to Z and plain recent newest first. */
function splitOrder(order: ResultOrder): { base: BaseOrder; descending: boolean } {
  const [base, direction] = order.split(":") as [BaseOrder, string | undefined];
  if (!Object.hasOwn(ORDERS, base)) return { base: "best", descending: false };
  return { base, descending: direction === undefined ? base === "recent" : direction === "desc" };
}

/**
 * The order value for a base and direction: the setting's short form
 * (path, recent:asc), or with `explicit` the form the suggestions offer
 * (path:asc, recent:desc), which a query's order: is rewritten to.
 */
export function joinOrder(base: BaseOrder, descending: boolean, explicit = false): ResultOrder {
  if (base === "best") return "best";
  const usual = (base === "recent") === descending;
  if (usual && !explicit) return base;
  return `${base}:${descending ? "desc" : "asc"}`;
}

/** What the order control shows, and does with a pick. */
interface OrderControl {
  /** The order results come in. */
  order: ResultOrder;
  /** Whether the search is at a past revision, whose files have no last change to sort by. */
  atRevision: boolean;
  /** A pick from the menu, or a click on the arrow. */
  onPick(base: BaseOrder, descending: boolean): void;
}

/** The menu's choices, the current one checked; recently changed is greyed out at a past revision. */
function orderMenu(control: OrderControl, base: BaseOrder): HTMLElement {
  const menu = element("div", { class: "menu order-menu", role: "menu", hidden: true, "data-testid": "order-menu" });
  for (const [choice, { name, detail }] of Object.entries(ORDERS) as [BaseOrder, (typeof ORDERS)[BaseOrder]][]) {
    const unavailable = choice === "recent" && control.atRevision;
    const item = button(
      {
        class: "menuitem",
        role: "menuitemradio",
        "aria-checked": String(choice === base),
        "aria-disabled": unavailable ? "true" : undefined,
        tabindex: "-1",
        "data-order": choice,
      },
      () => {
        // Switching orders keeps the direction each one usually runs.
        if (!unavailable && choice !== base) control.onPick(choice, choice === "recent");
      },
      element("span", { class: "check", "aria-hidden": "true" }, choice === base ? "✓" : ""),
      element(
        "span",
        { class: "name" },
        element("span", {}, name),
        element("span", { class: "detail" }, unavailable ? "Not for a past revision's files" : detail),
      ),
    );
    menu.append(item);
  }
  return menu;
}

/** The arrow: which way the order runs, and a click flips it; greyed out for best match, which runs one way. */
function directionArrow(control: OrderControl, base: BaseOrder, descending: boolean): HTMLElement {
  if (base === "best") {
    return element(
      "span",
      {
        class: "direction",
        "aria-disabled": "true",
        title: "Best match runs one way",
        "data-testid": "order-direction",
      },
      "↓",
    );
  }
  const [ascending, descendingLabel] = DIRECTIONS[base];
  const [now, other] = descending ? [descendingLabel, ascending] : [ascending, descendingLabel];
  return button(
    {
      class: "direction",
      title: `${now}. Click for ${other.toLowerCase()}`,
      "aria-label": `${now}. Click for ${other.toLowerCase()}`,
      "data-testid": "order-direction",
    },
    () => {
      control.onPick(base, !descending);
    },
    `${descending ? "↓" : "↑"} ${now.replace(/ first$/u, "").toLowerCase()}`,
  );
}

/**
 * The order control for a search with `globals`: a button naming the order
 * (the query's order:, otherwise the `setting`), which opens the menu, and
 * the direction arrow. A past revision's files have no last change, so the
 * setting's recently changed falls back to best match there. ↑↓, Home and
 * End move through the open menu, Esc closes it back to its button, and
 * leaving it closes it too.
 */
export function orderControl(
  globals: Globals | undefined,
  setting: ResultOrder,
  onPick: OrderControl["onPick"],
): HTMLElement {
  const atRevision = globals?.at !== undefined;
  const fallback = atRevision && splitOrder(setting).base === "recent" ? "best" : setting;
  const control: OrderControl = { order: globals?.order ?? fallback, atRevision, onPick };
  const { base, descending } = splitOrder(control.order);
  const menu = orderMenu(control, base);
  const items = (): HTMLElement[] => [...menu.querySelectorAll<HTMLElement>('[role="menuitemradio"]')];
  const opener = button(
    { class: "order-button", "aria-haspopup": "menu", "aria-expanded": "false", "data-testid": "order-button" },
    () => {
      toggle(menu.hidden);
    },
    ORDERS[base].name,
  );
  const toggle = (open: boolean) => {
    menu.hidden = !open;
    opener.setAttribute("aria-expanded", String(open));
    if (open) (menu.querySelector<HTMLElement>('[aria-checked="true"]') ?? items()[0])?.focus();
  };
  const wrap = element("span", { class: "order-control" }, opener, directionArrow(control, base, descending), menu);
  menu.addEventListener("keydown", (event) => {
    const list = items();
    const at = list.indexOf(document.activeElement as HTMLElement);
    const next: Record<string, number> = {
      ArrowDown: (at + 1) % list.length,
      ArrowUp: (at - 1 + list.length) % list.length,
      Home: 0,
      End: list.length - 1,
    };
    if (event.key === "Escape") {
      toggle(false);
      opener.focus();
    } else if (event.key in next) {
      list[next[event.key] ?? 0]?.focus();
    } else {
      return;
    }
    event.preventDefault();
    event.stopPropagation(); // Esc closes the menu, not the panel
  });
  wrap.addEventListener("focusout", (event) => {
    if (!wrap.contains(event.relatedTarget as Node | null)) toggle(false);
  });
  return wrap;
}

/**
 * What a result says about when its file changed, in recently-changed
 * order: "Uncommitted" and when it was saved, its newest commit's time (the
 * author in the tooltip), or outside Git when it was saved. `saved` labels
 * a save time ("Modified 3 days ago" on a file row).
 */
export function changeNote(item: ResultItem, saved = (ago: string) => ago): HTMLElement[] {
  if (item.kind !== "file" && item.kind !== "line") return [];
  const notes: HTMLElement[] = [];
  if (item.kind === "line" && item.dirty) notes.push(element("span", { class: "badge warn" }, "Uncommitted"));
  if (item.kind === "line" && item.lastCommit) {
    const { author, at } = item.lastCommit;
    notes.push(element("span", { class: "meta changed", title: `Last commit by ${author}` }, timeAgo(at)));
  }
  if (item.modifiedAt) {
    const title = "When the file was last saved";
    notes.push(element("span", { class: "meta changed", title }, saved(timeAgo(item.modifiedAt))));
  }
  return notes;
}
