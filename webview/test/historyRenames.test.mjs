// History through renames (board 59): a commit that renamed a file shows
// "old → new" and says renamed, a file renamed later says its name now, a
// preview of a rename that changed no line says so, and with f: the mode
// chip says history follows renames.
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
} from "./harness.mjs";

useBrowser();

const SHA = "d746de8a".repeat(5);

/** f:retry_policy.py type:commit, as the daemon parses it. */
function renamedFileQuery() {
  const raw = "f:retry_policy.py type:commit";
  const root = {
    kind: "and",
    children: [
      { kind: "op", op: "f", value: "retry_policy.py", match: "literal", span: { start: 0, end: 17 } },
      { kind: "op", op: "type", value: "commit", match: "literal", span: { start: 18, end: 29 } },
    ],
    span: { start: 0, end: raw.length },
  };
  return {
    raw,
    parsed: parsedQuery(raw, root, { mode: "history", globals: { case: null, count: null, type: "commit" } }),
  };
}

/** A commit result in payments-api. */
const commit = (ref, subject, files) => ({
  kind: "commit",
  ref,
  repoId: "r1",
  sha: SHA,
  subject,
  author: { name: "Jane Doe", email: "jane@example.com" },
  at: "2026-09-01T00:00:00Z",
  files,
  diffHits: 0,
  matchedTerms: [],
  subjectHits: [],
  inMessage: false,
});

async function searched(t) {
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  const { raw, parsed } = renamedFileQuery();
  const seq = await typeAndParse(page, raw, parsed);
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [
      commit("c1", "Move policy into retry_policy", [
        { path: "src/payments/retry_policy.py", oldPath: "src/payments/policy.py", added: 0, removed: 0 },
      ]),
      commit("c2", "Add exponential backoff", [
        { path: "src/payments/backoff.py", currentPath: "src/payments/retry_policy.py", added: 38, removed: 0 },
      ]),
    ],
  });
  await searchDone(page, seq, { total: 2, ms: 2 });
  return page;
}

test("a rename shows its old and new path, and an older change says the file's name now", async (t) => {
  // @covers screen:history-renames op:f
  const page = await searched(t);
  const rows = page.locator(".row.commit .commit-files");
  assert.equal(await rows.nth(0).innerText(), "src/payments/policy.py → src/payments/retry_policy.py +0 −0 renamed");
  assert.equal(await rows.nth(1).innerText(), "src/payments/backoff.py +38 −0 now src/payments/retry_policy.py");
  assert.equal(await page.locator('[data-testid="mode"]').textContent(), "Commit history · follows renames");
  assert.deepEqual(pageErrors(page), []);
});

test("previewing a rename that changed no line says so instead of an empty diff", async (t) => {
  // @covers screen:history-renames
  const page = await searched(t);
  await fromHost(page, "preview.result", {
    ref: "c1",
    preview: {
      kind: "commit",
      sha: SHA,
      subject: "Move policy into retry_policy",
      body: "",
      author: "Jane Doe <jane@example.com>",
      at: "2026-09-01T00:00:00Z",
      files: [{ path: "src/payments/retry_policy.py", oldPath: "src/payments/policy.py", added: 0, removed: 0 }],
      hunks: [],
      subjectHits: [],
      bodyHits: [],
    },
  });
  const preview = page.locator('[data-testid="preview"]');
  assert.equal(
    await preview.locator('[data-testid="renamed-only"]').innerText(),
    "Renamed from src/payments/policy.py; no lines changed.",
  );
  assert.equal(await preview.locator('[data-testid="renamed"]').count(), 1);
  assert.deepEqual(pageErrors(page), []);
});

test("history without f: keeps the plain mode chip", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0), { mode: "history" }));
  assert.equal(await page.locator('[data-testid="mode"]').textContent(), "Commit history");
});

test("with merged branches, a branch's commit says which merge brought it in, and a merge says so", async (t) => {
  // @covers screen:history-renames setting:index.mergedBranches
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0), { mode: "history" }));
  const merge = "cd1cc79a".repeat(5);
  const now = Date.now();
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [
      {
        ...commit("c1", "Merge branch 'jitter'", [
          { path: "src/payments/client.py", added: 1, removed: 0, resolution: true },
        ]),
        sha: merge,
        parents: 2,
      },
      {
        ...commit("c2", "Log the delay before each retry", [{ path: "src/payments/client.py", added: 1, removed: 1 }]),
        author: { name: "Bob Lee", email: "bob@example.com" },
        at: new Date(now - 21 * 86_400_000).toISOString(),
        mergedIn: merge,
        mergedAt: new Date(now - 2 * 86_400_000).toISOString(),
      },
    ],
  });
  await searchDone(page, seq, { total: 2, ms: 2 });
  const rows = page.locator(".row.commit");
  assert.equal(await rows.nth(0).locator('[data-testid="merge"]').textContent(), "merge");
  assert.equal(
    await rows.nth(0).locator(".commit-files").innerText(),
    "src/payments/client.py +1 −0 conflict resolved",
  );
  assert.equal(await rows.nth(1).locator('[data-testid="merged-in"]').textContent(), "merged in cd1cc79 · 2 days ago");
  assert.equal(await rows.nth(1).locator('[data-testid="merge"]').count(), 0);
  // A merge with no changes of its own says why its preview has none.
  await fromHost(page, "preview.result", {
    ref: "c1",
    preview: {
      kind: "commit",
      sha: merge,
      subject: "Merge branches 'docs', 'owners' and 'scripts'",
      body: "",
      author: "Ana Ruiz <ana@example.com>",
      at: new Date(now).toISOString(),
      files: [],
      hunks: [],
      subjectHits: [],
      bodyHits: [],
      parents: 4,
    },
  });
  assert.equal(
    await page.locator('[data-testid="merge-no-changes"]').innerText(),
    "No changes of its own: the commits it merged are listed on their own.",
  );
  assert.deepEqual(pageErrors(page), []);
});
