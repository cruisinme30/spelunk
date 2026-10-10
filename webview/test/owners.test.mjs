// owner: (board 42): each result names who owns its file by its repo's
// CODEOWNERS file, the facet row counts the results by owner, the preview
// says which CODEOWNERS line decided, and the hidden note says what owner:
// left out.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  FILE_NAME_RESULT,
  fromHost,
  lastSent,
  openPanel,
  pageErrors,
  parsedQuery,
  QUERY,
  restore,
  searchDone,
  textNode,
  typeAndParse,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

const TEAM = "@acme/payments-core";
const RAW = `retry owner:${TEAM}`;
const OWNER_NODE = { kind: "op", op: "owner", value: TEAM, match: "literal", span: { start: 6, end: RAW.length } };
const PARSED = parsedQuery(RAW, {
  kind: "and",
  children: [textNode("retry", 0), OWNER_NODE],
  span: { start: 0, end: RAW.length },
});

/** A panel searching retry owner:@acme/payments-core, with one file owned by the team and Jane, one by the team. */
async function ownedResults(t, done = {}) {
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  const seq = await typeAndParse(page, RAW, PARSED);
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [
      { ...FILE_NAME_RESULT, path: "src/payments/http/retry.py", owners: [TEAM, "@jane"] },
      { ...CODE_LINE_RESULT, owners: [TEAM] },
    ],
  });
  await searchDone(page, seq, { total: 2, ms: 3, ...done });
  return { page, seq };
}

test("each result names its file's owners, the first with a count of the rest", async (t) => {
  // @covers screen:owner-filter op:owner
  const { page } = await ownedResults(t);
  const badges = page.locator('[data-testid="owner"]');
  assert.deepEqual(await badges.allTextContents(), [`${TEAM} +1`, TEAM]);
  assert.equal(await badges.first().getAttribute("title"), `Owned by ${TEAM}, @jane`);
  // A result whose file nobody owns has no badge.
  const seq = (await lastSent(page, "query.changed")).seq;
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [{ ...FILE_NAME_RESULT, ref: "f2" }] });
  assert.equal(await badges.count(), 2);
  assert.deepEqual(pageErrors(page), []);
});

test("the facet row counts results by owner, and a bucket keeps or leaves out that owner", async (t) => {
  // @covers screen:owner-filter screen:result-facets
  const { page } = await ownedResults(t, {
    facets: [
      {
        field: "owner",
        more: 0,
        buckets: [
          { label: TEAM, count: 5, filter: `owner:${TEAM}` },
          { label: "@jane", count: 3, filter: "owner:@jane" },
          { label: "No owner", count: 1, filter: "owner:none" },
        ],
      },
    ],
  });
  const owner = page.locator('[data-testid="facets"] [data-field="owner"]');
  assert.equal(await owner.locator(".label").textContent(), "Owner");
  assert.deepEqual(await owner.locator('[data-testid="facet-bucket"] .facet-label').allTextContents(), [
    TEAM,
    "@jane",
    "No owner",
  ]);
  await page.click('[data-filter="owner:@jane"]');
  assert.equal(await page.inputValue(QUERY), `${RAW} owner:@jane`);
});

/** The preview of the first result, with `ownership` when its repo has a CODEOWNERS file. */
function filePreview(ownership) {
  return {
    ref: "f1",
    preview: {
      kind: "file",
      path: "src/payments/http/retry.py",
      firstLine: 1,
      lines: ["def retry(fn):"],
      focusLine: 1,
      hits: [],
      dirtyLines: [],
      ...(ownership ? { ownership } : {}),
    },
  };
}

test("the preview says who owns the file and which CODEOWNERS line says so", async (t) => {
  // @covers screen:owner-filter msg:preview.result
  const { page } = await ownedResults(t);
  await waitForSent(page, "result.select", { ref: "f1" });
  const line = page.locator('[data-testid="preview"] [data-testid="ownership"]');
  await fromHost(page, "preview.result", filePreview({ owners: [TEAM, "@jane"], file: ".github/CODEOWNERS", line: 4 }));
  assert.equal((await line.innerText()).replaceAll(/\s+/g, " "), `Owned by ${TEAM} @jane · .github/CODEOWNERS line 4`);
  await fromHost(page, "preview.result", filePreview({ owners: [], file: ".github/CODEOWNERS", line: 6 }));
  assert.equal((await line.innerText()).replaceAll(/\s+/g, " ").trim(), "No owner in .github/CODEOWNERS line 6");
  await fromHost(page, "preview.result", filePreview({ owners: [], file: "CODEOWNERS", line: 0 }));
  assert.equal((await line.innerText()).replaceAll(/\s+/g, " ").trim(), "No owner in CODEOWNERS");
  // A repo without CODEOWNERS has no line at all.
  await fromHost(page, "preview.result", filePreview());
  assert.equal(await line.count(), 0);
});

test("the hidden note says how many matches owner: left out and offers them back", async (t) => {
  // @covers screen:owner-filter
  const { page } = await ownedResults(t, {
    hidden: [
      {
        reason: "owner",
        filter: `owner:${TEAM}`,
        count: 9,
        unit: "matches",
        undo: { title: `Remove owner:${TEAM}`, edits: [{ span: { start: 5, end: RAW.length }, newText: "" }] },
      },
    ],
  });
  const note = page.locator('[data-testid="hidden-notes"] .note').first();
  assert.match(await note.innerText(), new RegExp(`9 matches hidden by owner:${TEAM}`));
  await note.locator('[data-testid="show-hidden"]').click();
  assert.equal(await page.inputValue(QUERY), "retry");
});
