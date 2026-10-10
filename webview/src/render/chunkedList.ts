// A section's rows in chunks of CHUNK_ROWS. main.css gives a full chunk
// `content-visibility: auto`: the browser lays out and paints only the
// chunks near the view, so each streamed batch costs its own rows, not the
// whole list again, and work on rows out of sight waits until they show.
// The chunk still filling is laid out as usual, so a list that fits in one
// (most of them) is drawn exactly as rows appended to the list would be.
import { element } from "../format";

/** Rows (a result with its file header or context lines) per chunk. */
const CHUNK_ROWS = 100;

/** The event the browser fires when it starts or stops skipping a `content-visibility: auto` element. */
const SKIP_EVENT = "contentvisibilityautostatechange";

/** The list of one section of results, appended to as batches stream in. */
export class ChunkedList {
  private chunk: HTMLElement | undefined;
  private rowsInChunk = 0;
  /** The chunks the browser skips, being out of sight. */
  private readonly skipped = new Set<HTMLElement>();
  /** Redraws held back until their chunk shows, by chunk and then by what they redraw. */
  private readonly pending = new Map<HTMLElement, Map<string, () => void>>();

  /** Fills `list`, the section's listbox. */
  constructor(private readonly list: HTMLElement) {}

  /** Adds a row at the end, and returns the chunk it went into. */
  append(row: Node): HTMLElement {
    if (!this.chunk || this.rowsInChunk >= CHUNK_ROWS) {
      this.chunk = this.newChunk();
      this.rowsInChunk = 0;
    }
    this.chunk.append(row);
    this.rowsInChunk++;
    if (this.rowsInChunk === CHUNK_ROWS) {
      this.chunk.classList.add("full");
      // The browser starts skipping it, and says so only once it doesn't: when it is in view, by the next frame.
      this.skipped.add(this.chunk);
    }
    return this.chunk;
  }

  /**
   * Runs `redraw` now if `chunk` shows, or else once it does; a later
   * redraw with the same `key` replaces one still waiting.
   */
  redraw(chunk: HTMLElement, key: string, redraw: () => void): void {
    if (!this.skipped.has(chunk)) {
      this.pending.get(chunk)?.delete(key);
      redraw();
      return;
    }
    const waiting = this.pending.get(chunk) ?? new Map<string, () => void>();
    waiting.set(key, redraw);
    this.pending.set(chunk, waiting);
  }

  private newChunk(): HTMLElement {
    const chunk = element("div", { class: "chunk" });
    chunk.addEventListener(SKIP_EVENT, (event) => {
      if ((event as Event & { skipped: boolean }).skipped) {
        this.skipped.add(chunk);
        return;
      }
      this.skipped.delete(chunk);
      const waiting = this.pending.get(chunk);
      this.pending.delete(chunk);
      for (const redraw of waiting?.values() ?? []) redraw();
    });
    this.list.append(chunk);
    return chunk;
  }
}
