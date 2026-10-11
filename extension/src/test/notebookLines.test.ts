// How a notebook's lines are numbered for opening and replacing in VS Code's
// notebook editor: as the daemon numbers them, each cell's lines in turn.
import assert from "node:assert/strict";
import { test } from "node:test";
import { cellLineOf, isNotebookPath, notebookLines } from "../notebookLines";

/** Cells holding these lines each. */
const cells = (...texts: string[][]) =>
  texts.map((lines) => ({ lineCount: lines.length, lineAt: (n: number) => lines[n] ?? "" }));

test("a line of a notebook's text is in the cell whose lines it falls among", () => {
  // @covers screen:notebook-cells
  const notebook = cells(["# Churn model", "", "Retries…"], ["import pandas as pd", "retry_policy = 5"], [""]);
  assert.deepEqual(cellLineOf(notebook, 0), { cell: 0, line: 0 });
  assert.deepEqual(cellLineOf(notebook, 2), { cell: 0, line: 2 });
  assert.deepEqual(cellLineOf(notebook, 3), { cell: 1, line: 0 });
  assert.deepEqual(cellLineOf(notebook, 4), { cell: 1, line: 1 });
  assert.deepEqual(cellLineOf(notebook, 5), { cell: 2, line: 0 }, "an empty cell is one line");
  assert.equal(cellLineOf(notebook, 6), undefined, "past the last cell");
  assert.equal(cellLineOf(notebook, -1), undefined);
});

test("replace's check reads a notebook as its cells' lines in turn", () => {
  const lines = notebookLines(cells(["a", "b"], ["c"]));
  assert.equal(lines.lineCount, 3);
  assert.deepEqual(
    [0, 1, 2, 3].map((line) => lines.lineText(line)),
    ["a", "b", "c", ""],
  );
});

test("notebooks are told apart by their extension", () => {
  assert.ok(isNotebookPath("/w/ml/churn.ipynb"));
  assert.ok(isNotebookPath(String.raw`C:\w\Churn.IPYNB`));
  assert.ok(!isNotebookPath("/w/ml/churn.py"));
  assert.ok(!isNotebookPath("/w/ml/churn.ipynb.bak"));
});
