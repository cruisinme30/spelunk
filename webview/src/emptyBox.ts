// What the empty box's recent and pinned queries do: run, pin, name and
// remove them, and move the selection through them. The panel draws the
// empty box (render/emptyState.ts); this keeps the state, the rows and the
// host in step.
import { clamp, wrapIndex } from "./format";
import { saveRecentWidth, send } from "./host";
import type { Layout } from "./layout";
import { addPin, forgetQuery, namePin, watchPin } from "./pinnedQueries";
import type { EmptyBoxRow } from "./render/chrome";
import type { EmptyStateHandlers } from "./render/emptyState";
import { emptyBoxQueries, type ViewState } from "./state";

/** What the empty box needs from the panel. */
interface EmptyBoxPanel {
  /** Puts a query in the box and searches it. */
  setQuery(text: string): void;
  /** Inserts a cheat-sheet snippet at the cursor. */
  insertAtCursor(snippet: string): void;
  /** Draws the empty box again. */
  showEmptyState(): void;
  /** Draws the key hints again, for the newly selected row. */
  renderFooter(): void;
}

/** The empty box's queries: one per panel. */
export class EmptyBox {
  /** Works on the panel's `state`, in its query box and body. */
  constructor(
    private readonly state: ViewState,
    private readonly layout: Pick<Layout, "input" | "body" | "announcer">,
    private readonly panel: EmptyBoxPanel,
  ) {}

  /** What the empty box's rows, divider and cheat sheet do. */
  handlers(): EmptyStateHandlers {
    return {
      onRunRecent: (query) => {
        this.run(query);
      },
      onRemoveRecent: (query) => {
        this.forget(query);
      },
      onPin: (query) => {
        this.pin(query);
      },
      onUnpin: (query) => {
        this.forget(query);
      },
      onRename: (query) => {
        this.rename(query);
      },
      onWatch: (query, watch) => {
        this.watch(query, watch);
      },
      onNamed: (query, name) => {
        this.onNamed(query, name);
      },
      onInsert: (snippet) => {
        this.panel.insertAtCursor(snippet);
      },
      onResizeRecent: (width) => {
        this.state.recentWidth = width;
        saveRecentWidth(width);
      },
    };
  }

  /** Runs a recent or pinned query. */
  run(query: string | undefined): void {
    if (query) this.panel.setQuery(query);
  }

  /** ↵ on an empty box runs the selected query. */
  runSelected(): void {
    this.run(emptyBoxQueries(this.state)[this.state.recentIndex]);
  }

  /** Keeps the empty box's selection on its rows, which may have got fewer. */
  clampIndex(): void {
    const rows = emptyBoxQueries(this.state).length;
    this.state.recentIndex = clamp(this.state.recentIndex, 0, Math.max(rows - 1, 0));
  }

  /** ⇧⌫ on an empty box unpins the selected query, or removes it from recent, as browsers do for their history. */
  removeSelected(event: KeyboardEvent): boolean {
    const selected = emptyBoxQueries(this.state)[this.state.recentIndex];
    if (!event.shiftKey || this.layout.input.value || !selected) return false;
    this.forget(selected);
    return true;
  }

  /** ⌥W on an empty box starts or stops watching the selected pinned query. */
  watchSelected(event: KeyboardEvent): boolean {
    const selected = emptyBoxQueries(this.state)[this.state.recentIndex];
    const pin = this.state.pinned.find((entry) => entry.query === selected);
    if (!event.altKey || event.code !== "KeyW" || this.layout.input.value || !pin) return false;
    this.watch(pin.query, !pin.watch);
    return true;
  }

  /** The selected row, for the key hints: a pin, and what ⌥W would do on it; undefined when there are no rows. */
  selectedRow(): EmptyBoxRow | undefined {
    const query = emptyBoxQueries(this.state)[this.state.recentIndex];
    if (query === undefined) return undefined;
    const pin = this.state.pinned.find((entry) => entry.query === query);
    if (!pin) return { pinned: false };
    if (pin.watch) return { pinned: true, watch: "stop" };
    return this.state.watchable.has(query) ? { pinned: true, watch: "start" } : { pinned: true };
  }

  /** ↑↓ on an empty box move through its queries. */
  moveSelection(step: 1 | -1): void {
    const rows = [...this.layout.body.querySelectorAll<HTMLElement>("[data-recent]")];
    if (rows.length === 0) return;
    this.state.recentIndex = wrapIndex(this.state.recentIndex, step, rows.length);
    for (const [index, row] of rows.entries()) row.classList.toggle("selected", index === this.state.recentIndex);
    // The box keeps the focus, so nothing else tells a screen reader which query ↵ would run.
    const query = rows[this.state.recentIndex]?.dataset["recent"];
    if (query !== undefined) this.layout.announcer.textContent = query;
    this.panel.renderFooter();
  }

  /** Unpins a query, or takes a recent one off the list; the selection stays on the same row, now the next query. */
  private forget(query: string): void {
    const removed = forgetQuery(this.state, query);
    if (!removed) return;
    send(removed, { query });
    this.clampIndex();
    this.panel.showEmptyState();
    this.layout.input.focus();
  }

  /** Starts or stops watching a pinned query; the bell changes at once, and the host saves it. */
  private watch(query: string, watch: boolean): void {
    const changed = watchPin(this.state, query, watch);
    if (!changed) return;
    send("pinned.watch", changed);
    this.panel.showEmptyState();
    this.layout.input.focus();
  }

  /** Pins a recent query at the end of the pinned list and opens its name field. */
  private pin(query: string): void {
    const saved = addPin(this.state, query);
    if (!saved) return;
    send("pinned.save", saved);
    this.rename(query);
  }

  /** Opens the name field on a pinned query. */
  private rename(query: string): void {
    this.state.naming = query;
    this.panel.showEmptyState();
    this.layout.body.querySelector<HTMLInputElement>(".pinned-name-field")?.focus();
  }

  /** The name field closed (the row redrew itself): saves the name, and gives the box the focus back unless it moved on. */
  private onNamed(query: string, name: string | undefined): void {
    const saved = namePin(this.state, query, name);
    if (saved) send("pinned.save", saved);
    const leftFor = document.activeElement;
    if (!leftFor || leftFor === document.body || !leftFor.isConnected) this.layout.input.focus();
  }
}
