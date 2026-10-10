// Which result is selected: ↑↓ and clicks move it, the selected row is
// highlighted and scrolled into view, and the host is asked for its preview.
import { clamp, scrollIntoContainer } from "./format";
import { send } from "./host";
import type { ResultsView } from "./render/results";
import type { ViewState } from "./state";

/** Holding ↓ shouldn't request a preview for every row it passes. */
const PREVIEW_DEBOUNCE_MS = 30;

/** What the selection needs from the panel. */
interface SelectionHooks {
  /** The results on screen, if any. */
  results(): ResultsView | undefined;
  /** The selection moved: screen readers are pointed at the new row. */
  onSelected(): void;
}

/** The selected result of the panel's results: one per panel. */
export class ResultSelection {
  private previewTimer: ReturnType<typeof setTimeout> | undefined;

  /** Keeps the selection in `state.selectedRef`. */
  constructor(
    private readonly state: ViewState,
    private readonly hooks: SelectionHooks,
  ) {}

  /** Highlights a row and asks the host for its preview. */
  select(ref: string): void {
    if (this.state.selectedRef === ref) return;
    const results = this.hooks.results();
    // Only the two rows that change: holding ↓ through thousands of results stays smooth.
    const previous = this.state.selectedRef ? results?.rowFor(this.state.selectedRef) : undefined;
    previous?.classList.remove("selected");
    previous?.setAttribute("aria-selected", "false");
    this.state.selectedRef = ref;
    const row = results?.rowFor(ref);
    if (row && results) {
      row.classList.add("selected");
      row.setAttribute("aria-selected", "true");
      scrollIntoContainer(results.list, row, "nearest");
    }
    this.hooks.onSelected();
    clearTimeout(this.previewTimer);
    this.previewTimer = setTimeout(() => {
      send("result.select", { ref });
    }, PREVIEW_DEBOUNCE_MS);
  }

  /** Results don't wrap: ↓ on the last row stays there. */
  move(step: 1 | -1): void {
    const rows = this.hooks.results()?.rows() ?? [];
    if (rows.length === 0) return;
    const current = rows.findIndex((row) => row.dataset["ref"] === this.state.selectedRef);
    const next = current === -1 ? 0 : clamp(current + step, 0, rows.length - 1);
    const ref = rows[next]?.dataset["ref"];
    if (ref) this.select(ref);
  }

  /** Selects the first result, unless one is selected already. */
  selectFirst(): void {
    if (this.state.selectedRef) return;
    const first = this.hooks.results()?.rows()[0]?.dataset["ref"];
    if (first) this.select(first);
  }
}
