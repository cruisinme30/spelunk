// The results on screen, indexed so that nothing done per keystroke or per
// batch scans them all: a row by its ref, the next row ↑↓ go to, the code
// lines a replace changes, the commits a watch found.
import { lineKey } from "../format";
import type { ResultItem } from "../protocol.gen";

type ResultKind = ResultItem["kind"];
type LineItem = Extract<ResultItem, { kind: "line" }>;

/** The sections in the order the list shows them, which is the order ↑↓ go through the results. */
const SECTION_ORDER: readonly ResultKind[] = ["file", "symbol", "line", "commit"];

/** A result on screen: its row, the chunk of the list it is in, and its place in its section. */
interface Placed {
  item: ResultItem;
  row: HTMLElement;
  chunk: HTMLElement;
  index: number;
}

/** Every result appended to a ResultsView. */
export class ResultIndex {
  /** Every result, in arrival order. */
  readonly items: ResultItem[] = [];
  private readonly byRef = new Map<string, Placed>();
  /** Each section's refs, top to bottom. */
  private readonly order: Record<ResultKind, string[]> = { file: [], symbol: [], line: [], commit: [] };
  /** The code results on each line (lineKey), which a replace previews together. */
  private readonly linesByKey = new Map<string, LineItem[]>();
  /** The refs of each commit, by sha. */
  private readonly commitsBySha = new Map<string, string[]>();
  /** Each result's ref by what it is (identityOf), which a search run again finds under a new ref. */
  private readonly byIdentity = new Map<string, string>();

  /** Adds a result, whose `row` went into `chunk` at the end of its section. */
  add(item: ResultItem, row: HTMLElement, chunk: HTMLElement): void {
    this.items.push(item);
    const refs = this.order[item.kind];
    this.byRef.set(item.ref, { item, row, chunk, index: refs.length });
    refs.push(item.ref);
    if (!this.byIdentity.has(identityOf(item))) this.byIdentity.set(identityOf(item), item.ref);
    if (item.kind === "line") listAt(this.linesByKey, lineKey(item.repoId, item.path, item.line)).push(item);
    else if (item.kind === "commit") listAt(this.commitsBySha, item.sha).push(item.ref);
  }

  /** The result `ref` names, its row and its chunk, if it is on screen. */
  placed(ref: string): Placed | undefined {
    return this.byRef.get(ref);
  }

  /** The ref of the result `identity` (identityOf) names, if it is on screen. */
  refWithIdentity(identity: string): string | undefined {
    return this.byIdentity.get(identity);
  }

  /** The code results on the line `key` (lineKey) names. */
  linesAt(key: string): LineItem[] {
    return this.linesByKey.get(key) ?? [];
  }

  /** The rows of the commit `sha`. */
  commitRows(sha: string): HTMLElement[] {
    return (this.commitsBySha.get(sha) ?? []).flatMap((ref) => this.byRef.get(ref)?.row ?? []);
  }

  /** The first `count` code results. */
  firstLines(count: number): LineItem[] {
    return this.order.line.slice(0, count).flatMap((ref) => {
      const item = this.byRef.get(ref)?.item;
      return item?.kind === "line" ? [item] : [];
    });
  }

  /** The top result of the list. */
  firstRef(): string | undefined {
    for (const kind of SECTION_ORDER) {
      const [first] = this.order[kind];
      if (first !== undefined) return first;
    }
    return undefined;
  }

  /**
   * The result `step` rows below (or above) `ref`, across sections, staying
   * on the first or last row; the top result when `ref` isn't on screen.
   */
  refFrom(ref: string, step: 1 | -1): string | undefined {
    const placed = this.byRef.get(ref);
    if (!placed) return this.firstRef();
    const next = this.order[placed.item.kind][placed.index + step];
    if (next !== undefined) return next;
    for (
      let section = SECTION_ORDER.indexOf(placed.item.kind) + step;
      section >= 0 && section < SECTION_ORDER.length;
      section += step
    ) {
      const refs = this.order[SECTION_ORDER[section] ?? "file"];
      const edge = step > 0 ? refs[0] : refs.at(-1);
      if (edge !== undefined) return edge;
    }
    return ref;
  }
}

/** The list `map` keeps under `key`, added empty the first time. */
function listAt<T>(map: Map<string, T[]>, key: string): T[] {
  let list = map.get(key);
  if (!list) {
    list = [];
    map.set(key, list);
  }
  return list;
}

/**
 * What a result is, independent of the search that found it: refs are
 * opaque and name their search, so the same file, line or commit found by
 * the same query run again has a new ref but the same identity.
 */
export function identityOf(item: ResultItem): string {
  switch (item.kind) {
    case "commit": {
      return ["commit", item.repoId, item.sha].join("\0");
    }
    case "file": {
      return ["file", item.repoId, item.path].join("\0");
    }
    case "line":
    case "symbol": {
      return [item.kind, item.repoId, item.path, String(item.line)].join("\0");
    }
  }
}
