// at: (board 41): the files as they were at a revision. The mode chip names
// the commit each repo read, results say which files changed or were deleted
// since, repos without the revision say why, and the preview reads as
// read-only with a Compare with now button.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  FILE_NAME_RESULT,
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

const SHA = "3f9c2e1a".repeat(5);
const OTHER_SHA = "9b0d44c7".repeat(5);

/** retry_after at `at`, as the daemon parses it. */
function atQuery(at) {
  const raw = `at:${at} retry_after`;
  const atNode = { kind: "op", op: "at", value: at, match: "literal", span: { start: 0, end: at.length + 3 } };
  const root = {
    kind: "and",
    children: [atNode, textNode("retry_after", at.length + 4)],
    span: { start: 0, end: raw.length },
  };
  return { raw, parsed: parsedQuery(raw, root, { globals: { case: null, count: null, type: null, at } }) };
}

/** A panel that searched at:`at`, found `items`, and ended with `done`. */
async function searchedAt(t, at, items, done = {}) {
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [
      { repoId: "r1", name: "payments-api", tree: "ready", history: "ready" },
      { repoId: "r2", name: "web-checkout", tree: "ready", history: "ready" },
    ],
  });
  const { raw, parsed } = atQuery(at);
  const seq = await typeAndParse(page, raw, parsed);
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  await searchDone(page, seq, { total: items.length, ...done });
  return { page, seq };
}

const mode = (page) => page.locator('[data-testid="mode"]');

test("the mode chip names the revision, then the commit and its date once the search read it", async (t) => {
  // @covers screen:past-revision op:at msg:search.done
  const page = await openPanel(t);
  await restore(page);
  const { raw, parsed } = atQuery("v1.0.0");
  const seq = await typeAndParse(page, raw, parsed);
  assert.equal(await mode(page).textContent(), "At v1.0.0");
  assert.match((await mode(page).getAttribute("class")) ?? "", /\brevision\b/);
  await searchDone(page, seq, { revisions: [{ repoId: "r1", sha: SHA, date: "2026-03-14T09:00:00Z" }] });
  assert.equal(await mode(page).textContent(), "At v1.0.0 · 3f9c2e1 · 14 Mar 2026");
  assert.deepEqual(pageErrors(page), []);
});

test("the chip counts the commits when the repos read different ones, and doesn't repeat a typed sha", async (t) => {
  // @covers screen:past-revision
  const both = [
    { repoId: "r1", sha: SHA, date: "2026-04-03T08:00:00Z" },
    { repoId: "r2", sha: OTHER_SHA, date: "2026-04-01T08:00:00Z" },
  ];
  const { page } = await searchedAt(t, "6m", [], { revisions: both });
  assert.equal(await mode(page).textContent(), "At 6m · 2 commits");
  const typed = await searchedAt(t, "3f9c2e1", [], {
    revisions: [{ repoId: "r1", sha: SHA, date: "2026-03-14T09:00:00Z" }],
  });
  assert.equal(await mode(typed.page).textContent(), "At 3f9c2e1 · 14 Mar 2026");
  // A tag that happens to begin the sha isn't one.
  const tag = await searchedAt(t, "3", [], { revisions: [{ repoId: "r1", sha: SHA, date: "2026-03-14T09:00:00Z" }] });
  assert.equal(await mode(tag.page).textContent(), "At 3 · 3f9c2e1 · 14 Mar 2026");
});

test("a query without at: searches the working tree, with no revision anywhere", async (t) => {
  // @covers screen:past-revision
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "retry_after", parsedQuery("retry_after", textNode("retry_after", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [CODE_LINE_RESULT] });
  await searchDone(page, seq, { total: 1 });
  assert.equal(await mode(page).textContent(), "Working tree");
  await waitForSent(page, "result.select", { ref: "l1" });
  await fromHost(page, "preview.result", linePreview());
  assert.equal(await page.locator('[data-testid="compare"]').count(), 0);
  assert.equal(await page.locator('[data-testid="preview-revision"]').count(), 0);
});

test("results say which files changed or were deleted since the revision", async (t) => {
  // @covers screen:past-revision op:at
  const items = [
    { ...FILE_NAME_RESULT, path: "src/payments/backoff.py", sinceRevision: "deleted" },
    { ...CODE_LINE_RESULT, sinceRevision: "changed" },
    { ...CODE_LINE_RESULT, ref: "l2", path: "README.md", line: 3 },
    {
      kind: "symbol",
      ref: "s1",
      repoId: "r1",
      path: "src/payments/backoff.py",
      line: 8,
      name: "parse_retry_after",
      symbolKind: "function",
      hits: [{ start: 6, end: 17, termIndex: 0 }],
      sinceRevision: "deleted",
    },
  ];
  const { page } = await searchedAt(t, "v1.0.0", items);
  const badges = page.locator('[data-testid="since-revision"]');
  // File names, then definitions, then code.
  assert.deepEqual(await badges.allTextContents(), ["deleted since", "deleted since", "changed since"]);
  assert.equal(await badges.nth(2).getAttribute("title"), "This file is different in the current files");
  assert.deepEqual(pageErrors(page), []);
});

test("each repo without the revision says why, and an empty result points at them", async (t) => {
  // @covers screen:past-revision
  const revisions = [
    { repoId: "r1", sha: SHA, date: "2026-03-14T09:00:00Z" },
    { repoId: "r2", problem: "v1.0.0 isn't a tag, branch or commit in web-checkout" },
  ];
  const { page } = await searchedAt(t, "v1.0.0", [], { revisions });
  assert.deepEqual(await page.locator('[data-testid="revision-problem"]').allTextContents(), [
    "v1.0.0 isn't a tag, branch or commit in web-checkout",
  ]);
  assert.match(await page.locator('[data-testid="no-results"]').innerText(), /Some repos don't have that revision/);
});

/** The preview of CODE_LINE_RESULT. */
function linePreview() {
  return {
    ref: "l1",
    preview: {
      kind: "file",
      path: "src/payments/client.py",
      firstLine: 41,
      lines: ["def charge(self):", "    self.retry_policy = RetryPolicy()"],
      focusLine: 42,
      hits: [],
      dirtyLines: [],
    },
  };
}

test("the preview reads as the revision, read-only, and Compare with now opens a diff of a changed file", async (t) => {
  // @covers screen:past-revision msg:result.open
  const { page } = await searchedAt(t, "v1.0.0", [
    { ...CODE_LINE_RESULT, sinceRevision: "changed" },
    { ...CODE_LINE_RESULT, ref: "l2", path: "src/payments/backoff.py", sinceRevision: "deleted" },
    { ...CODE_LINE_RESULT, ref: "l3", path: "README.md" },
  ]);
  await waitForSent(page, "result.select", { ref: "l1" });
  await fromHost(page, "preview.result", linePreview());
  assert.equal(await page.locator('[data-testid="preview-revision"]').textContent(), "v1.0.0 · read-only");
  const compare = page.locator('[data-testid="compare"]');
  assert.equal(await compare.isEnabled(), true);
  await compare.click();
  await waitForSent(page, "result.open", { ref: "l1", where: "compare" });

  // A file deleted since, or the same now, has nothing to compare with.
  for (const [ref, title] of [
    ["l2", "Deleted since: there's no current file to compare with"],
    ["l3", "The same as the current file"],
  ]) {
    await page.locator(`[data-ref="${ref}"]`).click();
    await waitForSent(page, "result.select", { ref });
    await fromHost(page, "preview.result", { ...linePreview(), ref });
    assert.equal(await compare.isEnabled(), false, `${ref}: Compare is off`);
    assert.equal(await compare.getAttribute("title"), title);
  }
  assert.deepEqual(pageErrors(page), []);
});
