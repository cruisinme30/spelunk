// Query watches: a pinned query that searches history has a bell. A watched
// pin shows how many new commits matched since it last ran, and those
// commits are tagged New when it runs.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fromHost,
  openPanel,
  pageErrors,
  parsedQuery,
  QUERY,
  restore,
  searchDone,
  sentMessages,
  textNode,
  typeAndParse,
  useBrowser,
} from "./harness.mjs";

useBrowser();

/** The pins of the mock: a watched history query, one that could be, and one over current files. */
const PINNED = [
  { query: "msg:revert f:payments/", name: "Payments reverts", watch: true },
  { query: String.raw`author:jane f:.*test\.py$ timeout`, name: "Flaky payment tests" },
  { query: "sym:RetryPolicy", name: "Retry policy" },
];

/** A panel showing PINNED, with what the host says each watch can do and found. */
async function panelWithPins(
  t,
  unseen = [{ query: "msg:revert f:payments/", count: 2, shas: ["c2f90b1", "7ae31d4"] }],
) {
  const page = await openPanel(t);
  await restore(page, ["since:2w timeout"], {}, PINNED);
  await fromHost(page, "watch.state", {
    watchable: ["msg:revert f:payments/", String.raw`author:jane f:.*test\.py$ timeout`],
    unseen,
  });
  return page;
}

/** The empty box row of `query` (its backslashes escaped for the CSS selector). */
const row = (page, query) => page.locator(`.recent-row[data-recent="${query.replaceAll("\\", "\\\\")}"]`);

/** The bell of the pinned row of `query`. */
const bell = (page, query) => row(page, query).locator('[data-testid="pinned-watch"]');

test("a watched pin shows its bell and how many new commits matched; others offer a bell or say why not", async (t) => {
  // @covers screen:query-watches msg:watch.state
  const page = await panelWithPins(t);
  assert.deepEqual(await page.locator('[data-testid="pinned-unseen"]').allTextContents(), ["2 new"]);
  assert.equal(
    await page.locator('[data-testid="pinned-unseen"]').getAttribute("title"),
    "2 commits matched since you last ran it",
  );

  const watched = bell(page, "msg:revert f:payments/");
  assert.equal(await watched.getAttribute("aria-pressed"), "true");
  assert.equal(await watched.getAttribute("aria-label"), "Stop watching Payments reverts");
  assert.ok(await watched.isVisible(), "a watched bell shows without hovering");
  assert.equal(await watched.evaluate((node) => getComputedStyle(node).opacity), "1");

  const watchable = bell(page, String.raw`author:jane f:.*test\.py$ timeout`);
  assert.equal(await watchable.getAttribute("aria-pressed"), "false");
  assert.equal(await watchable.evaluate((node) => getComputedStyle(node).opacity), "0", "shown on hover only");

  const cannot = bell(page, "sym:RetryPolicy");
  assert.equal(await cannot.getAttribute("aria-disabled"), "true", "a query over current files can't be watched");
  assert.match(await cannot.getAttribute("title"), /Only history queries can be watched/);
  await cannot.focus();
  assert.ok(
    await cannot.evaluate((node) => document.activeElement === node),
    "it still takes focus, so its tooltip can be read",
  );
  assert.equal(await page.locator('[data-testid="recent"] [data-testid="pinned-watch"]').count(), 0);
  assert.deepEqual(pageErrors(page), []);
});

test("the bell starts and stops a watch at once, and stopping drops the count", async (t) => {
  // @covers msg:pinned.watch screen:query-watches
  const page = await panelWithPins(t);
  await row(page, String.raw`author:jane f:.*test\.py$ timeout`).hover();
  await bell(page, String.raw`author:jane f:.*test\.py$ timeout`).click();
  assert.equal(await bell(page, String.raw`author:jane f:.*test\.py$ timeout`).getAttribute("aria-pressed"), "true");

  await bell(page, "msg:revert f:payments/").click();
  assert.equal(await bell(page, "msg:revert f:payments/").getAttribute("aria-pressed"), "false");
  assert.equal(await page.locator('[data-testid="pinned-unseen"]').count(), 0, "not watched: no count");

  await row(page, "sym:RetryPolicy").hover();
  await bell(page, "sym:RetryPolicy").click({ force: true });
  assert.deepEqual(
    (await sentMessages(page, "pinned.watch")).map((message) => message.payload),
    [
      { query: String.raw`author:jane f:.*test\.py$ timeout`, watch: true },
      { query: "msg:revert f:payments/", watch: false },
    ],
    "the bell that can't watch sends nothing",
  );
  assert.equal((await sentMessages(page, "query.changed")).length, 0, "watching doesn't run the query");
});

test("⌥W watches or stops watching the selected pin, and does nothing on a recent query", async (t) => {
  // @covers screen:query-watches
  const page = await panelWithPins(t, []);
  // The key hints offer ⌥W only where it does something, and say what.
  const keys = () => page.locator('[data-testid="keys"]').innerText();
  assert.match(await keys(), /⌥W\s*stop watching/);
  await page.keyboard.press("Alt+KeyW"); // the first row: Payments reverts, watched
  await page.keyboard.press("ArrowDown");
  assert.match(await keys(), /⌥W\s*watch/);
  await page.keyboard.press("Alt+KeyW"); // Flaky payment tests, watchable
  await page.keyboard.press("ArrowDown");
  assert.doesNotMatch(await keys(), /⌥W/, "Retry policy can't be watched");
  assert.match(await keys(), /↵\s*run pinned query\s*⇧⌫\s*unpin/);
  await page.keyboard.press("Alt+KeyW"); // Retry policy: can't
  await page.keyboard.press("ArrowDown");
  assert.doesNotMatch(await keys(), /⌥W/, "a recent query isn't watched");
  assert.match(await keys(), /↵\s*run recent query\s*⇧⌫\s*remove/);
  await page.keyboard.press("Alt+KeyW"); // a recent query
  assert.deepEqual(
    (await sentMessages(page, "pinned.watch")).map((message) => message.payload),
    [
      { query: "msg:revert f:payments/", watch: false },
      { query: String.raw`author:jane f:.*test\.py$ timeout`, watch: true },
    ],
  );
  assert.equal(await page.inputValue(QUERY), "", "⌥W types nothing");
});

/** A commit result; `overrides` replaces any field. */
const commit = (ref, sha, subject) => ({
  kind: "commit",
  ref,
  repoId: "r1",
  sha,
  subject,
  author: { name: "Marta Ruiz", email: "marta@payments.example" },
  at: "2026-10-10T06:30:00Z",
  files: [{ path: "payments/ledger/writer.py", added: 9, removed: 21 }],
  diffHits: 0,
  matchedTerms: [0],
  subjectHits: [{ start: 0, end: 6 }],
  inMessage: true,
});

test("the commits a watch found are tagged New when the query runs, on every page, and only for that search", async (t) => {
  // @covers screen:query-watches
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  const history = parsedQuery("msg:revert", textNode("revert", 4), { mode: "history" });
  const seq = await typeAndParse(page, "msg:revert", history);
  const firstPage = [
    commit("c1", "c2f90b1000000000000000000000000000000000", "Revert ledger batching (#1288)"),
    commit("c2", "41d08ce000000000000000000000000000000000", "Revert idempotency key on payouts"),
  ];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: firstPage });
  const newCommits = ["c2f90b1000000000000000000000000000000000", "7ae31d4000000000000000000000000000000000"];
  await searchDone(page, seq, { total: 3, nextCursor: "2", newCommits });
  assert.equal(await page.locator('[data-ref="c1"] [data-testid="new-commit"]').innerText(), "NEW");
  assert.equal(await page.locator('[data-ref="c2"] [data-testid="new-commit"]').count(), 0, "seen before");

  // Load more: the next page's new commit is tagged as it arrives.
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [commit("c3", "7ae31d4000000000000000000000000000000000", "Revert “Retry refunds on 502”")],
  });
  assert.equal(await page.locator('[data-ref="c3"] [data-testid="new-commit"]').count(), 1);
  await searchDone(page, seq, { total: 3, newCommits });
  assert.equal(await page.locator('[data-testid="new-commit"]').count(), 2, "not tagged twice");

  // Another search starts with nothing tagged.
  const next = await typeAndParse(page, "msg:revert x", history);
  await fromHost(page, "search.batch", {
    seq: next,
    searchId: "s2",
    items: [commit("c4", newCommits[0], "Revert ledger batching (#1288)")],
  });
  assert.equal(await page.locator('[data-testid="new-commit"]').count(), 0);
  assert.deepEqual(pageErrors(page), []);
});
