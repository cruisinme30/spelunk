// Jupyter notebooks (board 60): results in a notebook sit under a heading
// naming their cell and number their lines within it; the preview draws the
// cells, their run counts and what their outputs are; a notebook's commit
// diff names each change's cell. The daemon reads notebooks by cell; these
// tests give the panel what it sends.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fromHost,
  openPanel,
  pageErrors,
  parsedQuery,
  restore,
  searchDone,
  textNode,
  typeAndParse,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

const NOTEBOOK = "notebooks/churn_model.ipynb";

/** A code result for retry_policy on line `line` of the notebook's text, in `cell` (and `extra` over it). */
const cellResult = (ref, [line, text], cell, extra = {}) => ({
  kind: "line",
  ref,
  repoId: "r1",
  path: NOTEBOOK,
  line,
  text,
  hits: [{ start: text.indexOf("retry_policy"), end: text.indexOf("retry_policy") + 12, termIndex: 0 }],
  ...(cell ? { cell } : {}),
  ...extra,
});

const MARKDOWN = { number: 1, line: 3, kind: "markdown", language: "markdown" };
const CODE = (line) => ({ number: 2, line, kind: "code", language: "python" });

/** The panel searching retry_policy, with results in two cells of a notebook and in a .py file. */
async function notebookResults(t, items) {
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "ml-research", tree: "ready", history: "ready" }],
  });
  const seq = await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  await searchDone(page, seq, { total: items.length, ms: 2 });
  return page;
}

test("results in a notebook sit under their cell's heading, numbered within the cell", async (t) => {
  // @covers screen:notebook-cells
  const page = await notebookResults(t, [
    cellResult("n1", [3, "Retries use the shared retry_policy."], MARKDOWN),
    cellResult("n2", [5, "from payments.retry_policy import RetryPolicy"], CODE(2)),
    cellResult("n3", [7, "retry_policy = RetryPolicy(max_tries=5)"], CODE(4)),
    { ...cellResult("p1", [3, "retry_policy = 1"]), path: "src/features.py" },
  ]);
  const headings = page.locator('[data-testid="cell-heading"]');
  assert.deepEqual(
    await headings.allTextContents(),
    ["Cell 1Markdown", "Cell 2Python"],
    "one heading per cell, not per line",
  );
  const numbers = await page.locator(".row.line .line-number").allTextContents();
  assert.deepEqual(numbers, ["3", "2", "4", "3"], "lines in the cell, then the .py file's own line");
  assert.deepEqual(pageErrors(page), []);
});

test("context lines of a notebook result are numbered within its cell, and cells don't join", async (t) => {
  const page = await openPanel(t);
  await restore(page, [], { contextLines: 1 });
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "ml-research", tree: "ready", history: "ready" }],
  });
  const seq = await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [
      cellResult("n1", [6, "retry_policy = 1"], CODE(3), { contextBefore: ["import x"], contextAfter: [] }),
      cellResult(
        "n2",
        [7, "use(retry_policy)"],
        { number: 3, line: 1, kind: "code", language: "python" },
        { contextAfter: ["done()"] },
      ),
    ],
  });
  await searchDone(page, seq, { total: 2, ms: 2 });
  const numbers = await page.locator(".contents .line-number").allTextContents();
  // Lines 6 and 7 of the text touch, but are cells 2 and 3: two runs, each under its heading.
  assert.deepEqual(numbers, ["2", "3", "1", "2"]);
  assert.equal(await page.locator('[data-testid="cell-heading"]').count(), 2);
});

test("a definition in a notebook says its cell and line", async (t) => {
  const page = await notebookResults(t, [
    {
      kind: "symbol",
      ref: "s1",
      repoId: "r1",
      path: NOTEBOOK,
      line: 7,
      name: "load_events",
      symbolKind: "function",
      hits: [],
      cell: { number: 3, line: 1, kind: "code", language: "python" },
    },
  ]);
  assert.equal(await page.locator(".row.symbol .path").textContent(), `${NOTEBOOK} · cell 3, line 1`);
});

test("a notebook's preview draws its cells, run counts and what their outputs are", async (t) => {
  // @covers screen:notebook-cells
  const page = await notebookResults(t, [cellResult("n3", [7, "retry_policy = RetryPolicy(max_tries=5)"], CODE(4))]);
  await fromHost(page, "preview.result", {
    ref: "n3",
    preview: {
      kind: "file",
      path: NOTEBOOK,
      firstLine: 3,
      lines: [
        "Retries use the shared retry_policy.",
        "import pandas as pd",
        "",
        "",
        "retry_policy = RetryPolicy(max_tries=5)",
        "print(retry_policy)",
      ],
      focusLine: 7,
      hits: [],
      dirtyLines: [],
      symbols: [{ name: "load_events", line: 8, kind: "function", depth: 0 }],
      cells: [
        { number: 1, kind: "markdown", language: "markdown", startLine: 1, lineCount: 3 },
        {
          number: 2,
          kind: "code",
          language: "python",
          startLine: 4,
          lineCount: 4,
          executionCount: 1,
          outputs: ["image"],
        },
        { number: 3, kind: "code", language: "python", startLine: 8, lineCount: 1, outputs: ["text", "other"] },
      ],
    },
  });
  const preview = page.locator('[data-testid="preview"]');
  assert.deepEqual(await preview.locator('[data-testid="cell-heading"]').allTextContents(), [
    "Cell 1Markdown",
    "[1]Cell 2Python",
    "Cell 3Python",
  ]);
  assert.deepEqual(await preview.locator(".code-line .line-number").allTextContents(), ["3", "1", "2", "3", "4", "1"]);
  assert.deepEqual(await preview.locator('[data-testid="cell-outputs"]').allTextContents(), [
    "Output: image · not searched",
    "Output: text, other · not searched",
  ]);
  assert.match(await preview.locator(".preview-header").textContent(), /cell 2, line 4/);
  assert.equal(await page.locator(".outline-row .outline-line").textContent(), "3:1");
  // A double-click still opens the notebook at that line of its text, which the host finds the cell of.
  await page.locator('.code-line[data-line="7"] code').dblclick();
  const { position } = await waitForSent(page, "result.open", { where: "side" });
  assert.equal(position.line, 7);
});

test("with outputs searched, a cell's text output is a cell of its own and the note names only the rest", async (t) => {
  const page = await notebookResults(t, [
    cellResult("o1", [9, "loaded with retry_policy"], { number: 2, line: 1, kind: "output" }),
  ]);
  await fromHost(page, "preview.result", {
    ref: "o1",
    preview: {
      kind: "file",
      path: NOTEBOOK,
      firstLine: 7,
      lines: ["plot(retry_policy)", "", "loaded with retry_policy"],
      focusLine: 9,
      hits: [],
      dirtyLines: [],
      cells: [
        { number: 1, kind: "code", language: "python", startLine: 7, lineCount: 2, outputs: ["text", "image"] },
        { number: 1, kind: "output", startLine: 9, lineCount: 1 },
      ],
    },
  });
  const preview = page.locator('[data-testid="preview"]');
  assert.deepEqual(await preview.locator('[data-testid="cell-heading"]').allTextContents(), [
    "Cell 1Python",
    "Cell 1Output",
  ]);
  assert.deepEqual(await preview.locator('[data-testid="cell-outputs"]').allTextContents(), [
    "Output: image · not searched",
  ]);
  assert.deepEqual(await page.locator('[data-testid="results"] [data-testid="cell-heading"]').allTextContents(), [
    "Cell 2Output",
  ]);
});

test("a notebook's commit diff names each change's cell and numbers lines within it", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "ml-research", tree: "ready", history: "ready" }],
  });
  const raw = "max_tries type:added";
  const seq = await typeAndParse(page, raw, parsedQuery(raw, textNode("max_tries", 0), { mode: "history" }));
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [
      {
        kind: "commit",
        ref: "c1",
        repoId: "r1",
        sha: "0d074f9aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        subject: "Retry event reads five times",
        author: { name: "Maya Chen", email: "maya@acme.dev" },
        at: "2026-10-02T15:30:00Z",
        files: [{ path: NOTEBOOK, added: 1, removed: 1 }],
        diffHits: 1,
        matchedTerms: [0],
        subjectHits: [],
      },
    ],
  });
  await searchDone(page, seq, { total: 1, ms: 2 });
  await fromHost(page, "preview.result", {
    ref: "c1",
    preview: {
      kind: "commit",
      sha: "0d074f9aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      subject: "Retry event reads five times",
      body: "",
      author: "Maya Chen <maya@acme.dev>",
      at: "2026-10-02T15:30:00Z",
      files: [{ path: NOTEBOOK, added: 1, removed: 1 }],
      hunks: [
        {
          path: NOTEBOOK,
          header: "@@ -5,3 +5,3 @@",
          lines: [
            { kind: "ctx", oldNo: 5, newNo: 5, text: "import pandas as pd", hits: [], cell: CODE(1) },
            { kind: "del", oldNo: 7, text: "retry_policy = RetryPolicy(max_tries=3)", hits: [], cell: CODE(3) },
            { kind: "add", newNo: 7, text: "retry_policy = RetryPolicy(max_tries=5)", hits: [], cell: CODE(3) },
          ],
        },
      ],
      subjectHits: [],
      bodyHits: [],
    },
  });
  const preview = page.locator('[data-testid="preview"]');
  assert.deepEqual(await preview.locator('[data-testid="cell-heading"]').allTextContents(), ["Cell 2Python"]);
  assert.deepEqual(await preview.locator(".code-line .line-number").allTextContents(), ["1", "3", "3"]);
  assert.equal(
    await preview.locator(".hunk").textContent(),
    NOTEBOOK,
    "the hunk's @@ numbers count the cells' text: left out",
  );
});
