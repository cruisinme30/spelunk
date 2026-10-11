// Jupyter notebooks' cells in results and previews. The daemon searches a
// notebook as its cells' text, each cell's source in turn, and says which
// cell each line is in: rows show a "Cell 2 · Python" heading above a
// cell's lines and number lines within their cell, as Jupyter does.
import { element } from "../format";
import type { CellPlace, NotebookCell } from "../protocol.gen";

/** How people write the languages Spelunk names in lower case; the rest take a capital. */
const LANGUAGE_TITLES: Readonly<Record<string, string>> = {
  "c++": "C++",
  csharp: "C#",
  javascript: "JavaScript",
  typescript: "TypeScript",
  sql: "SQL",
  php: "PHP",
  html: "HTML",
  css: "CSS",
  json: "JSON",
  yaml: "YAML",
  toml: "TOML",
};

/** What a cell holds, as its heading names it: its language for code, else Markdown, Raw or Output. */
function cellKindLabel({ kind, language }: Pick<CellPlace, "kind" | "language">): string {
  if (kind === "markdown") return "Markdown";
  if (kind === "raw") return "Raw";
  if (kind === "output") return "Output";
  if (!language) return "Code";
  return LANGUAGE_TITLES[language] ?? language.charAt(0).toUpperCase() + language.slice(1);
}

/** A cell's heading: "Cell 2 · Python", after Jupyter's [3] when it ran and has a count. */
export function cellHeading(
  cell: Pick<CellPlace, "number" | "kind" | "language">,
  executionCount?: number,
): HTMLElement {
  return element(
    "div",
    { class: `cell-heading cell-${cell.kind}`, "data-testid": "cell-heading", "data-cell": String(cell.number) },
    executionCount === undefined ? null : element("span", { class: "cell-run" }, `[${String(executionCount)}]`),
    element("span", { class: "cell-number" }, `Cell ${String(cell.number)}`),
    element("span", { class: "muted" }, cellKindLabel(cell)),
  );
}

/** The cell that line (1-based, counting the notebook's text) is in. */
export function cellOfLine(cells: readonly NotebookCell[], line: number): NotebookCell | undefined {
  return cells.find((cell) => line >= cell.startLine && line < cell.startLine + cell.lineCount);
}

/** A line's number within its cell. */
export function lineInCell(cell: NotebookCell, line: number): number {
  return line - cell.startLine + 1;
}

/**
 * Under a code cell, the outputs the preview doesn't show: "Output: text, image · not searched". With
 * `textSearched` (index.notebookOutputs), its text is shown as an output cell of its own, so only the rest are named.
 */
function outputsNote(cell: NotebookCell, textSearched: boolean): HTMLElement | null {
  const kinds = [...new Set(cell.outputs ?? [])].filter((kind) => !(textSearched && kind === "text"));
  if (kinds.length === 0) return null;
  return element(
    "div",
    { class: "cell-outputs muted", "data-testid": "cell-outputs" },
    `Output: ${kinds.join(", ")} · not searched`,
  );
}

/** A cell's heading above line (1-based) of a notebook's text when the cell starts there, or `first` line shown. */
export function cellHeadingAt(cells: readonly NotebookCell[], line: number, first: boolean): HTMLElement[] {
  const cell = cellOfLine(cells, line);
  if (!cell || (line !== cell.startLine && !first)) return [];
  return [cellHeading(cell, cell.kind === "code" ? cell.executionCount : undefined)];
}

/** How a preview numbers line (1-based) of a file: within its cell, in a notebook. */
export function shownLine(cells: readonly NotebookCell[], line: number): number {
  const cell = cellOfLine(cells, line);
  return cell ? lineInCell(cell, line) : line;
}

/**
 * Under the last line of a code cell, what its outputs are that the preview doesn't show (see outputsNote). Outputs
 * that are searched (index.notebookOutputs) are cells of their own.
 */
export function outputsNoteAt(cells: readonly NotebookCell[], line: number): HTMLElement[] {
  const cell = cellOfLine(cells, line);
  if (cell?.kind !== "code" || line !== cell.startLine + cell.lineCount - 1) return [];
  const note = outputsNote(
    cell,
    cells.some((other) => other.kind === "output"),
  );
  return note ? [note] : [];
}
