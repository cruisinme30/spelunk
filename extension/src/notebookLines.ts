// How the daemon numbers a Jupyter notebook's lines, for opening and
// replacing in VS Code's notebook editor: the notebook's text is each cell's
// source in turn, so line n is in the cell whose lines it falls among. A
// cell of n lines is n lines of the text, as it is n lines in the editor.
// No VS Code imports, so tests can run it.

/** A notebook's cell, as far as numbering lines goes. */
export interface CellText {
  lineCount: number;
  lineAt(line: number): string;
}

/** Where a line of a notebook's text is: the index of its cell, and the line in that cell, both 0-based. */
export interface CellLine {
  cell: number;
  line: number;
}

/** Whether the file at `path` is a Jupyter notebook, by its extension. */
export function isNotebookPath(path: string): boolean {
  return path.toLowerCase().endsWith(".ipynb");
}

/** The cell and line in it that line (0-based) of a notebook's text is; undefined past its last cell. */
export function cellLineOf(cells: readonly { lineCount: number }[], line: number): CellLine | undefined {
  let start = 0;
  for (const [index, cell] of cells.entries()) {
    if (line < start + cell.lineCount) return line < start ? undefined : { cell: index, line: line - start };
    start += cell.lineCount;
  }
  return undefined;
}

/** A notebook's text, as replace's check reads it: each cell's lines in turn. */
export function notebookLines(cells: readonly CellText[]): { lineCount: number; lineText: (line: number) => string } {
  const lineCount = cells.reduce((total, cell) => total + cell.lineCount, 0);
  return {
    lineCount,
    lineText: (line) => {
      const at = cellLineOf(cells, line);
      return at === undefined ? "" : (cells[at.cell]?.lineAt(at.line) ?? "");
    },
  };
}
