// Which lines of code the panel shows, so the host can color them: a code
// result's lines and the lines around it, and a preview's file excerpt or diff.
import type { HostToWebview, Preview, ResultItem, SyntaxLinesMessage } from "./protocol.gen";

/** Lines of one file to color: consecutive (`continued`), so each picks up where the one above left off, or each on its own. */
export interface FileLines {
  path: string;
  lines: string[];
  continued: boolean;
}

/** Colors lines of code; undefined when nothing colors them (syntax colors are off, or no grammar knows the files). */
export interface SyntaxSource {
  color(files: readonly FileLines[]): Promise<SyntaxLinesMessage | undefined>;
}

/** The lines of code `items` show: each code result's lines, with its context lines, in file order. */
function resultLines(items: readonly ResultItem[]): FileLines[] {
  return items.flatMap((item) =>
    item.kind === "line"
      ? [
          {
            path: item.path,
            lines: [
              ...(item.contextBefore ?? []),
              item.text,
              ...(item.spanLines ?? []).map((line) => line.text),
              ...(item.contextAfter ?? []),
            ],
            continued: true,
          },
        ]
      : [],
  );
}

/** The lines of code `preview` shows: a file's excerpt, or each line of a commit's diff on its own. */
function previewLines(preview: Preview | null): FileLines[] {
  if (!preview) return [];
  if (preview.kind === "file") return [{ path: preview.path, lines: preview.lines, continued: true }];
  return preview.hunks.map((hunk) => ({
    path: hunk.path,
    lines: hunk.lines.map((line) => line.text),
    continued: false,
  }));
}

/**
 * Colors the code of the results and previews posted to the panel, after
 * them: the panel draws code plain, then in color once its colors arrive,
 * so coloring never holds results up.
 */
export class SyntaxPosts {
  private searchId = "";
  private results: ResultItem[] = [];
  private preview: Preview | null = null;
  /** The colors posted last, which end-to-end tests read. */
  last: SyntaxLinesMessage | undefined;

  /** `post` sends the panel the colors `source` gives. */
  constructor(
    private readonly source: SyntaxSource,
    private readonly post: (message: SyntaxLinesMessage) => void,
  ) {}

  /** Sees a message posted to the panel, and colors the code it carries. */
  posted<K extends keyof HostToWebview>(type: K, payload: HostToWebview[K]): void {
    if (type === "search.batch") {
      const { searchId, items } = payload as HostToWebview["search.batch"];
      if (searchId !== this.searchId) this.results = [];
      this.searchId = searchId;
      this.results.push(...items);
      void this.color(resultLines(items));
    } else if (type === "preview.result") {
      this.preview = (payload as HostToWebview["preview.result"]).preview;
      void this.color(previewLines(this.preview));
    }
  }

  /** The theme, its grammars or the syntax colors setting changed: colors the code on screen again. */
  recolor(): void {
    void this.color([...resultLines(this.results), ...previewLines(this.preview)], true);
  }

  /** Posts the colors of `files`; a reset drops the old colors, even when nothing new is colored. */
  private async color(files: FileLines[], reset = false): Promise<void> {
    if (files.length === 0 && !reset) return;
    try {
      const colored = await this.source.color(files);
      if (!colored && !reset) return;
      this.last = { ...(colored ?? { colors: [], lines: [] }), ...(reset ? { reset } : {}) };
      this.post(this.last);
    } catch {
      // A grammar that fails to load leaves its code plain.
    }
  }
}
