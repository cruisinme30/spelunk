// Which result is selected: ↑↓ and clicks move it, the selected row is
// highlighted and scrolled into view, and the host is asked for its preview
// while the preview pane shows.
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
  /** The result whose preview was last asked for, so a pane that shows again asks only once. */
  private previewed: string | undefined;
  /** Notices the preview pane showing again (a narrow panel hides it), to ask for the preview it missed. */
  private readonly paneWatcher: ResizeObserver;

  /** Keeps the selection in `state.selectedRef`. */
  constructor(
    private readonly state: ViewState,
    private readonly hooks: SelectionHooks,
  ) {
    this.paneWatcher = new ResizeObserver(() => {
      this.requestPreview();
    });
  }

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
      this.requestPreview();
    }, PREVIEW_DEBOUNCE_MS);
  }

  /** Watches the preview pane of a new search's results, which have no preview asked for yet. */
  watchPane(pane: HTMLElement): void {
    this.paneWatcher.disconnect();
    this.previewed = undefined;
    this.paneWatcher.observe(pane);
  }

  /**
   * Asks the host for the selected result's preview, unless the pane is
   * hidden (a panel 640px wide or less has no room for it): reading files
   * nobody sees would only slow the host. It asks once the pane shows.
   */
  private requestPreview(): void {
    const ref = this.state.selectedRef;
    const pane = this.hooks.results()?.preview;
    if (!ref || ref === this.previewed || !pane || pane.getClientRects().length === 0) return;
    this.previewed = ref;
    send("result.select", { ref });
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
