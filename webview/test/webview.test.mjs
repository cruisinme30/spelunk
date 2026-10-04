// The search panel webview: drives the real panel bundle in Chromium with
// host messages written as the extension host would send them, and checks
// what it renders and sends.
// @covers msg:ready msg:state.restore msg:query.changed msg:parse.result msg:search.batch msg:search.done msg:result.select msg:preview.result msg:index.status msg:banner msg:daemon.restart msg:result.open
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  FILE_NAME_RESULT,
  fromHost,
  lastSent,
  openPanel,
  panelWithResults,
  parsedQuery,
  restore,
  sentMessages,
  textNode,
  typeAndParse,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

/** The query's error diagnostics as rendered, one element each. */
const DIAGNOSTIC_ROWS = '[data-testid="diagnostics"] > *';

test("an empty box shows recent queries and all 16 operators", async (t) => {
  // @covers screen:empty-box
  const page = await openPanel(t);
  assert.equal((await sentMessages(page, "ready")).length, 1);
  await restore(page, ["sym:RetryPolicy", "since:2w timeout"]);
  assert.equal(await page.locator('[data-testid="recent"]').count(), 2);
  assert.equal(await page.locator('[data-testid="sheet-op"]').count(), 16);
});

test("typing sends query.changed with the text and a new seq", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  await page.fill('[data-testid="query"]', "retry_policy");
  const message = await lastSent(page, "query.changed");
  assert.equal(message.text, "retry_policy");
  assert.equal(message.cursor, 12);
});

test("focus selects the box, and Esc with nothing else open asks to close the panel", async (t) => {
  // @covers msg:focus msg:panel.close
  const page = await openPanel(t);
  await restore(page);
  await page.fill('[data-testid="query"]', "retry");
  await page.evaluate(() => document.activeElement.blur());
  await fromHost(page, "focus", {});
  const box = await page.evaluate(() => ({
    testId: document.activeElement.dataset.testid,
    selected: document.activeElement.selectionEnd - document.activeElement.selectionStart,
  }));
  assert.deepEqual(box, { testId: "query", selected: 5 });
  await page.keyboard.press("Escape");
  assert.equal((await sentMessages(page, "panel.close")).length, 1);
});

test("streamed results render in sections with a summary", async (t) => {
  const page = await panelWithResults(t);
  assert.equal(await page.locator('[data-testid="result"]').count(), 2);
  assert.match(await page.locator('[data-testid="summary"]').innerText(), /1 file name · 1 code match/);
});

test("the first result is selected and previewed; ↓ moves the selection", async (t) => {
  const page = await panelWithResults(t);
  assert.equal((await waitForSent(page, "result.select")).ref, "f1");
  await page.keyboard.press("ArrowDown");
  await waitForSent(page, "result.select", { ref: "l1" });
  await fromHost(page, "preview.result", {
    ref: "l1",
    preview: {
      kind: "file",
      path: "src/payments/client.py",
      firstLine: 41,
      lines: ["x", "self.retry_policy = RetryPolicy()"],
      focusLine: 42,
      hits: [{ line: 42, ranges: [{ start: 5, end: 17, termIndex: 0 }] }],
      dirtyLines: [],
    },
  });
  assert.equal(await page.locator('[data-testid="preview"] mark').count(), 1);
});

test("Enter opens the selected result; ⌘Enter opens it to the side", async (t) => {
  const page = await panelWithResults(t);
  await waitForSent(page, "result.select");
  await page.keyboard.press("Enter");
  assert.deepEqual(await lastSent(page, "result.open"), { ref: "f1", where: "current" });
  await page.keyboard.press("Control+Enter");
  assert.deepEqual(await lastSent(page, "result.open"), { ref: "f1", where: "side" });
});

test("a single click selects a result and a double-click opens it", async (t) => {
  // @covers setting:open.trigger
  const page = await panelWithResults(t);
  const row = page.locator('[data-ref="l1"]');
  await row.click();
  await waitForSent(page, "result.select", { ref: "l1" });
  assert.equal(await lastSent(page, "result.open"), undefined, "one click only selects");
  await row.dblclick();
  assert.deepEqual(await lastSent(page, "result.open"), { ref: "l1", where: "current" });
});

test("a parse result for an older seq is dropped and the newest one renders", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  await page.fill('[data-testid="query"]', "a");
  await page.fill('[data-testid="query"]', "ab");
  const [olderSeq, newestSeq] = (await sentMessages(page, "query.changed")).map((message) => message.payload.seq);
  const withError = (raw) =>
    parsedQuery(raw, null, {
      diagnostics: [{ severity: "error", code: "x", message: raw, span: { start: 0, end: 1 }, fixes: [] }],
    });
  await fromHost(page, "parse.result", { seq: olderSeq, query: withError("a"), completions: [] });
  assert.equal(await page.locator(DIAGNOSTIC_ROWS).count(), 0, "older seq ignored");
  await fromHost(page, "parse.result", { seq: newestSeq, query: withError("ab"), completions: [] });
  assert.equal(await page.locator(DIAGNOSTIC_ROWS).count(), 1, "newest seq rendered");
});

test("⌘. applies the first fix as a text edit followed by query.changed", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const fix = { title: "Change to since:", edits: [{ span: { start: 0, end: 6 }, newText: "since:" }] };
  await typeAndParse(
    page,
    "sinse:6m",
    parsedQuery("sinse:6m", null, {
      diagnostics: [
        {
          severity: "error",
          code: "unknown_operator",
          message: "Unknown operator sinse:",
          span: { start: 0, end: 6 },
          fixes: [fix],
        },
      ],
    }),
  );
  assert.equal(await page.locator('[data-code="unknown_operator"]').count(), 1);
  await page.keyboard.press("Control+.");
  assert.equal(await page.inputValue('[data-testid="query"]'), "since:6m");
  assert.equal((await lastSent(page, "query.changed")).text, "since:6m");
});

test("Aa and .* reflect the query, and Aa removes case:yes from the text", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const root = {
    kind: "and",
    span: { start: 0, end: 16 },
    children: [
      { kind: "op", op: "case", value: "yes", match: "literal", span: { start: 0, end: 8 } },
      { kind: "text", value: "Retry", match: "regex", termIndex: 0, span: { start: 9, end: 16 } },
    ],
  };
  await typeAndParse(
    page,
    "case:yes /Retry/",
    parsedQuery("case:yes /Retry/", root, { globals: { case: "yes", count: null, type: null } }),
  );
  assert.equal(await page.getAttribute('[data-testid="toggle-case"]', "aria-pressed"), "true");
  assert.equal(await page.getAttribute('[data-testid="toggle-regex"]', "aria-pressed"), "true");
  await page.click('[data-testid="toggle-case"]');
  assert.equal(await page.inputValue('[data-testid="query"]'), "/Retry/");
});

test("hidden-result notes use singular units and name the filter", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "x -f:vendor/", parsedQuery("x -f:vendor/", textNode("x", 0)));
  const undo = { title: "Remove -f:vendor/", edits: [{ span: { start: 1, end: 12 }, newText: "" }] };
  await fromHost(page, "search.done", {
    seq,
    searchId: "s1",
    total: 0,
    truncated: false,
    ms: 1,
    hidden: [{ reason: "pathFilter", filter: "-f:vendor/", count: 1, unit: "files", undo }],
  });
  assert.equal(
    await page.locator('[data-testid="hidden-notes"] .note span').first().innerText(),
    "1 file hidden by -f:vendor/",
  );
});

test("a case:yes note offers to ignore case", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "case:yes Retry", parsedQuery("case:yes Retry", textNode("Retry", 9)));
  const undo = { title: "Ignore case", edits: [{ span: { start: 0, end: 9 }, newText: "" }] };
  await fromHost(page, "search.done", {
    seq,
    searchId: "s1",
    total: 0,
    truncated: false,
    ms: 1,
    hidden: [{ reason: "case", filter: "case:yes", count: 5, unit: "matches", undo }],
  });
  const note = page.locator('[data-testid="hidden-notes"] .note');
  assert.equal(await note.locator("span").first().innerText(), "5 matches hidden by case:yes");
  assert.equal(await note.locator('[data-testid="show-hidden"]').innerText(), "Ignore case");
});

test("a query with errors keeps the last results and says which query they are for", async (t) => {
  // @covers screen:query-errors
  const page = await panelWithResults(t);
  const lastGood = page.locator('[data-testid="last-good"]');
  assert.equal(await lastGood.isVisible(), false);
  const broken = parsedQuery("retry_policy (", textNode("retry_policy", 0), {
    diagnostics: [
      {
        severity: "error",
        code: "unclosed_paren",
        span: { start: 13, end: 14 },
        message: "Missing closing parenthesis",
        fixes: [],
      },
    ],
  });
  await typeAndParse(page, "retry_policy (", broken);
  assert.equal(await page.locator('[data-testid="result"]').count(), 2, "the last results stay");
  assert.equal(await lastGood.textContent(), "Showing results for the last query that worked retry_policy");
  await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  assert.equal(await lastGood.isVisible(), false);
});

test("a path-scoped search says which paths its code results come from", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const text = "f:.*test\\.py$ timeout";
  const path = { kind: "op", op: "f", value: ".*test\\.py$", match: "regex", span: { start: 0, end: 14 } };
  const root = { kind: "and", children: [path, textNode("timeout", 15)], span: { start: 0, end: 22 } };
  const seq = await typeAndParse(page, text, parsedQuery(text, root));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [CODE_LINE_RESULT] });
  assert.match(
    await page.locator('[data-testid="section-code"] .section-title').innerText(),
    /Code in matching paths/i,
  );
  assert.equal(
    await page.locator('[data-testid="path-scope"]').innerText(),
    "Only files whose full path matches .*test\\.py$ are searched.",
  );
});

test("code rows drop the line's indentation and keep the highlight on the match", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  const indented = {
    ...CODE_LINE_RESULT,
    text: "        self.retry_policy = x",
    hits: [{ start: 13, end: 25, termIndex: 0 }],
  };
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [indented] });
  const code = page.locator('[data-kind="line"] code');
  assert.equal(await code.innerText(), "self.retry_policy = x");
  assert.equal(await code.locator("mark").innerText(), "retry_policy");
});

test("the stopped banner offers Restart and clears when the daemon is back", async (t) => {
  const page = await openPanel(t);
  await fromHost(page, "banner", { state: "stopped", message: "Search stopped" });
  await page.click('[data-testid="restart"]');
  assert.equal((await sentMessages(page, "daemon.restart")).length, 1);
  await fromHost(page, "banner", { state: "ok" });
  assert.equal(await page.locator('[data-testid="restart"]').count(), 0);
});

test("Load more asks for the next page of the same search and appends it", async (t) => {
  // @covers msg:results.more
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [FILE_NAME_RESULT] });
  const firstPage = { seq, searchId: "s1", total: 1, truncated: false, hidden: [], ms: 1 };
  await fromHost(page, "search.done", { ...firstPage, nextCursor: "page-2" });

  await page.click('[data-testid="load-more"]');
  assert.deepEqual(await lastSent(page, "results.more"), { searchId: "s1", cursor: "page-2" });

  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [CODE_LINE_RESULT] });
  await fromHost(page, "search.done", firstPage);
  assert.equal(await page.locator('[data-testid="result"]').count(), 2, "the next page is appended");
  assert.equal(await page.locator('[data-testid="load-more"]').count(), 0, "the last page has no Load more");
});
