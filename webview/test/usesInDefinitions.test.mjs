// Uses inside definitions (board 45): kind:function ref:RetryPolicy lists
// each use of RetryPolicy inside a function, each row naming the function
// it is in, and the note counts the uses outside one.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  fromHost,
  openPanel,
  pageErrors,
  parsedQuery,
  QUERY,
  restore,
  searchDone,
  typeAndParse,
  useBrowser,
} from "./harness.mjs";

useBrowser();

const RAW = "kind:function ref:RetryPolicy";
const PARSED = parsedQuery(RAW, {
  kind: "and",
  children: [
    { kind: "op", op: "kind", value: "function", match: "literal", span: { start: 0, end: 13 } },
    { kind: "op", op: "ref", value: "RetryPolicy", match: "literal", span: { start: 14, end: RAW.length } },
  ],
  span: { start: 0, end: RAW.length },
});

/** A panel showing two uses inside functions, with `done` added to the search's end. */
async function uses(t, done = {}) {
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
      {
        ...CODE_LINE_RESULT,
        ref: "l1",
        line: 58,
        text: "    policy = RetryPolicy.from_config(cfg)",
        hits: [{ start: 13, end: 24, termIndex: 0 }],
        within: { name: "build_client", kind: "function", line: 55 },
      },
      {
        ...CODE_LINE_RESULT,
        ref: "l2",
        line: 71,
        text: "    if isinstance(policy, RetryPolicy):",
        hits: [{ start: 26, end: 37, termIndex: 0 }],
        within: { name: "describe_policy", kind: "function", line: 69 },
      },
    ],
  });
  await searchDone(page, seq, { total: 2, ms: 3, ...done });
  return page;
}

test("each use names the function it is in", async (t) => {
  // @covers screen:uses-in-definitions op:kind op:ref
  const page = await uses(t);
  const labels = page.locator(".row.line .within");
  assert.deepEqual(await labels.allTextContents(), ["in function build_client", "in function describe_policy"]);
  assert.match(await labels.first().getAttribute("title"), /starts on line 55/);
  assert.deepEqual(pageErrors(page), []);
});

test("the note counts the uses outside a function and shows every use", async (t) => {
  // @covers screen:uses-in-definitions
  const page = await uses(t, {
    hidden: [
      {
        reason: "kind",
        filter: "kind:function",
        count: 2,
        unit: "matches",
        undo: { title: "Remove kind:function", edits: [{ span: { start: 0, end: 14 }, newText: "" }] },
      },
    ],
  });
  const note = page.locator('[data-testid="hidden-notes"] .note').first();
  assert.match(await note.innerText(), /2 matches outside a function hidden by kind:function/);
  const show = note.locator('[data-testid="show-hidden"]');
  assert.equal(await show.innerText(), "Show every match");
  await show.click();
  assert.equal(await page.inputValue(QUERY), "ref:RetryPolicy");
});
