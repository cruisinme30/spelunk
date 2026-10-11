// What the result menu copies and exports.
import assert from "node:assert/strict";
import { test } from "node:test";
import type { ResultItem } from "../protocol.gen";
import {
  absolutePath,
  asCsv,
  asJson,
  asMarkdown,
  asText,
  copyText,
  type ExportInput,
  exportFileName,
  relativePath,
  shaAndSubject,
} from "../resultText";

const SHA = "4421bd636aec0b0706c0789e8894e475746c561b";
const LINE: ResultItem = {
  kind: "line",
  ref: "r-line",
  repoId: "r1",
  path: "src/payments/client.py",
  line: 46,
  text: "        for attempt in range(self.retry_policy.max_attempts):",
  hits: [],
};
const FILE: ResultItem = {
  kind: "file",
  ref: "r-file",
  repoId: "r2",
  path: "docs/retry_policy.md",
  nameHits: [],
  dirty: false,
};
const SYMBOL: ResultItem = {
  kind: "symbol",
  ref: "r-sym",
  repoId: "r1",
  path: "src/payments/retry_policy.py",
  line: 3,
  name: "RetryPolicy",
  symbolKind: "class",
  hits: [],
};
const COMMIT: ResultItem = {
  kind: "commit",
  ref: "r-commit",
  repoId: "r1",
  sha: SHA,
  subject: "Retry charges with the shared retry_policy",
  author: { name: "Jane Doe", email: "jane@acme.dev" },
  at: "2026-10-01T15:30:00Z",
  files: [],
  diffHits: 1,
  matchedTerms: [0],
  subjectHits: [],
  inMessage: true,
};

test("copying one result: with its code, its full path, or its path from the folder", () => {
  // @covers command:result.copy command:result.copyPath command:result.copyRelativePath command:result.copySha command:result.copyShaAndSubject
  assert.equal(copyText(LINE), "src/payments/client.py:46: for attempt in range(self.retry_policy.max_attempts):");
  assert.equal(copyText(SYMBOL), "src/payments/retry_policy.py:3: RetryPolicy");
  assert.equal(copyText(FILE), "docs/retry_policy.md");
  assert.equal(absolutePath(LINE, "/Users/dana/payments-api"), "/Users/dana/payments-api/src/payments/client.py:46");
  assert.equal(
    absolutePath(FILE, String.raw`C:\work\shared-libs`, "\\"),
    String.raw`C:\work\shared-libs\docs\retry_policy.md`,
  );
  assert.equal(absolutePath(FILE, "/work/shared-libs/"), "/work/shared-libs/docs/retry_policy.md");
  assert.equal(relativePath(LINE), "src/payments/client.py:46");
  assert.equal(relativePath(FILE), "docs/retry_policy.md");
  assert.equal(shaAndSubject(COMMIT), "4421bd6 Retry charges with the shared retry_policy");
});

/** The four results, from repos r1 (payments-api) and r2 (shared-libs, which has no remote). */
const INPUT: ExportInput = {
  items: [FILE, SYMBOL, LINE, COMMIT],
  repoName: (id) => (id === "r1" ? "payments-api" : "shared-libs"),
  query: "retry_policy",
  searchLink: "vscode://spelunk.spelunk/search?q=retry_policy",
  link: (item) => (item.repoId === "r1" ? `https://example.test/${item.ref}` : undefined),
};

test("every result as text: one line each, grep-style", () => {
  // @covers command:results.copyAll command:results.exportText
  assert.equal(
    asText(INPUT),
    [
      "# Spelunk: retry_policy",
      "shared-libs/docs/retry_policy.md",
      "payments-api/src/payments/retry_policy.py:3: RetryPolicy",
      "payments-api/src/payments/client.py:46: for attempt in range(self.retry_policy.max_attempts):",
      "payments-api 4421bd6 2026-10-01 Jane Doe: Retry charges with the shared retry_policy",
      "",
    ].join("\n"),
  );
});

test("every result as Markdown: linked to its permalink, code lines under their file", () => {
  // @covers command:results.exportMarkdown
  assert.equal(
    asMarkdown(INPUT),
    [
      "## Spelunk: `retry_policy`",
      "",
      "[Open this search in Spelunk](vscode://spelunk.spelunk/search?q=retry_policy)",
      "",
      "- shared-libs/docs/retry_policy.md",
      "",
      "**payments-api/src/payments/retry_policy.py**",
      "",
      "- [L3](https://example.test/r-sym): `RetryPolicy`",
      "",
      "**payments-api/src/payments/client.py**",
      "",
      "- [L46](https://example.test/r-line): `for attempt in range(self.retry_policy.max_attempts):`",
      "- [`4421bd6`](https://example.test/r-commit) Retry charges with the shared retry_policy (Jane Doe)",
      "",
    ].join("\n"),
  );
  // Backticks in code keep the code span whole.
  const ticked = asMarkdown({ ...INPUT, items: [{ ...LINE, text: "say `hi`" }] });
  assert.match(ticked, /: `` say `hi` ``$/mu);
});

test("every result as CSV and JSON, quoting what needs it", () => {
  // @covers command:results.exportCsv command:results.exportJson
  const csv = asCsv({ ...INPUT, items: [{ ...LINE, text: 'log("a, b")' }, COMMIT] });
  assert.equal(
    csv,
    [
      "kind,repo,path,line,text,sha,author,date,url",
      'line,payments-api,src/payments/client.py,46,"log(""a, b"")",,,,https://example.test/r-line',
      `commit,payments-api,,,Retry charges with the shared retry_policy,${SHA},Jane Doe,2026-10-01T15:30:00Z,https://example.test/r-commit`,
      "",
    ].join("\n"),
  );
  const json = JSON.parse(asJson(INPUT)) as { query: string; results: Record<string, unknown>[] };
  assert.equal(json.query, "retry_policy");
  assert.deepEqual(json.results[0], { kind: "file", repo: "shared-libs", path: "docs/retry_policy.md" });
  assert.deepEqual(json.results[2], {
    kind: "line",
    repo: "payments-api",
    path: "src/payments/client.py",
    line: 46,
    text: LINE.text,
    url: "https://example.test/r-line",
  });
});

test("an export's file is named after its query", () => {
  assert.equal(exportFileName("retry_policy", "md"), "spelunk-retry_policy.md");
  assert.equal(exportFileName('author:jane "retry later"', "csv"), "spelunk-author-jane-retry-later.csv");
  assert.equal(exportFileName("///", "txt"), "spelunk-results.txt");
});
