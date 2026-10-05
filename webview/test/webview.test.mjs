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
  openPanelWithSavedState,
  panelWithResults,
  parsedQuery,
  QUERY,
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

test("dragging the divider resizes the recent queries and the width survives a reload", async (t) => {
  const page = await openPanel(t);
  await page.setViewportSize({ width: 1200, height: 700 });
  await restore(page, ["sym:RetryPolicy"]);
  const recentWidth = async () => (await page.locator(".recent").boundingBox()).width;
  const divider = page.locator('[data-testid="recent-divider"]');
  const before = await recentWidth();
  const box = await divider.boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 150, box.y + box.height / 2, { steps: 5 });
  await page.mouse.up();
  assert.ok(Math.abs((await recentWidth()) - (before + 150)) <= 2, "the recent queries follow the pointer");
  const dragged = await recentWidth();
  assert.equal(await page.evaluate(() => globalThis.__savedState.recentWidth), dragged);

  // ← and → move it too, and neither side can be dragged shut.
  await divider.focus();
  await page.keyboard.press("ArrowLeft");
  assert.equal(Math.round(await recentWidth()), Math.round(dragged) - 24);
  assert.equal(await divider.getAttribute("aria-valuenow"), String(Math.round(dragged) - 24));
  assert.equal(await divider.getAttribute("aria-valuemin"), "200");
  const moved = await divider.boundingBox();
  await page.mouse.move(moved.x + moved.width / 2, moved.y + moved.height / 2);
  await page.mouse.down();
  await page.mouse.move(0, moved.y + moved.height / 2, { steps: 5 });
  await page.mouse.up();
  assert.equal(Math.round(await recentWidth()), 200);

  // A reloaded panel keeps the width; a double-click puts back the default split.
  const saved = await page.evaluate(() => globalThis.__savedState);
  const reloaded = await openPanelWithSavedState(t, saved);
  await reloaded.setViewportSize({ width: 1200, height: 700 });
  await restore(reloaded, ["sym:RetryPolicy"]);
  assert.equal(Math.round((await reloaded.locator(".recent").boundingBox()).width), 200);
  await reloaded.locator('[data-testid="recent-divider"]').dblclick();
  assert.equal(Math.round((await reloaded.locator(".recent").boundingBox()).width), Math.round(before));
  assert.equal(await reloaded.evaluate(() => globalThis.__savedState.recentWidth), undefined);
});

test("a recent width saved in a wide panel still leaves the cheat sheet its minimum when the panel narrows", async (t) => {
  const page = await openPanelWithSavedState(t, { recentWidth: 800 });
  await page.setViewportSize({ width: 1200, height: 700 });
  await restore(page, ["sym:RetryPolicy"]);
  await page.setViewportSize({ width: 900, height: 700 });
  assert.ok((await page.locator(".sheet").boundingBox()).width >= 320, "the cheat sheet keeps 320px");
});

test("the divider shows once recent and the cheat sheet fit side by side, and hides when they stack", async (t) => {
  const page = await openPanel(t);
  await page.setViewportSize({ width: 841, height: 700 });
  await restore(page, ["sym:RetryPolicy"]);
  const divider = page.locator('[data-testid="recent-divider"]');
  assert.equal(await divider.isVisible(), true);
  await page.setViewportSize({ width: 600, height: 700 });
  assert.equal(await divider.isVisible(), false);
});

test("? in an empty box scrolls the cheat sheet into view when it sits below the recent queries", async (t) => {
  const page = await openPanel(t);
  await page.setViewportSize({ width: 600, height: 320 });
  await restore(
    page,
    Array.from({ length: 12 }, (_, index) => `retry ${index}`),
  );
  const sheet = page.locator('[data-testid="sheet"]');
  const inView = () => sheet.evaluate((node) => node.getBoundingClientRect().top < globalThis.innerHeight);
  assert.equal(await inView(), false, "the sheet starts below the fold");
  await page.locator(QUERY).press("?");
  assert.equal(await inView(), true);
  assert.equal(await page.inputValue(QUERY), "", "the ? isn't typed into the box");
});

test("an empty box shows recent queries and all 16 operators", async (t) => {
  // @covers screen:empty-box
  const page = await openPanel(t);
  assert.equal((await sentMessages(page, "ready")).length, 1);
  await restore(page, ["sym:RetryPolicy", "since:2w timeout"]);
  assert.equal(await page.locator('[data-testid="recent"]').count(), 2);
  assert.equal(await page.locator('[data-testid="sheet-op"]').count(), 16);
  // An operator's full and short names share one line.
  const [full, short] = await page.locator('[data-testid="sheet-op"]').first().locator("code").all();
  assert.equal(await short.textContent(), "c:");
  assert.equal((await full.boundingBox()).y, (await short.boundingBox()).y);
});

test("the × on a recent query removes it without running it", async (t) => {
  // @covers msg:recent.remove
  const page = await openPanel(t);
  await restore(page, ["first", "second", "third"]);
  const recent = page.locator('[data-testid="recent"] code');
  await page.locator(".recent-row").nth(1).hover();
  await page.locator('[data-testid="recent-remove"]').nth(1).click();
  assert.deepEqual(await recent.allTextContents(), ["first", "third"]);
  assert.deepEqual(
    (await sentMessages(page, "recent.remove")).map((message) => message.payload),
    [{ query: "second" }],
  );
  assert.equal((await sentMessages(page, "query.changed")).length, 0, "removing doesn't run the query");
  assert.equal(await page.evaluate(() => document.activeElement.dataset.testid), "query", "the box keeps focus");
});

test("⇧⌫ on an empty box removes the selected recent query and keeps the selection in place", async (t) => {
  const page = await openPanel(t);
  await restore(page, ["first", "second", "third"]);
  await page.keyboard.press("ArrowDown"); // "second"
  await page.keyboard.press("Shift+Backspace");
  assert.deepEqual(await page.locator('[data-testid="recent"] code').allTextContents(), ["first", "third"]);
  assert.equal(await page.locator(".recent-row.selected").getAttribute("data-recent"), "third");
  await page.keyboard.press("Shift+Backspace");
  await page.keyboard.press("Shift+Backspace");
  assert.equal(await page.locator('[data-testid="recent"]').count(), 0);
  assert.deepEqual(
    (await sentMessages(page, "recent.remove")).map((message) => message.payload.query),
    ["second", "third", "first"],
  );
  await page.fill(QUERY, "timeouts");
  await page.keyboard.press("Shift+Backspace");
  assert.equal(await page.inputValue(QUERY), "timeout", "with text in the box it just deletes");
});

test("typing sends query.changed with the text and a new seq", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  await page.fill(QUERY, "retry_policy");
  const message = await lastSent(page, "query.changed");
  assert.equal(message.text, "retry_policy");
  assert.equal(message.cursor, 12);
});

test("focus selects the box, and Esc with nothing else open asks to close the panel", async (t) => {
  // @covers msg:focus msg:panel.close
  const page = await openPanel(t);
  await restore(page);
  await page.fill(QUERY, "retry");
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

/** A commit result for "retry" from Jane; `overrides` replaces any field. */
const commitResult = (ref, subject, overrides) => ({
  kind: "commit",
  ref,
  repoId: "r1",
  sha: "4c18e9b0000000000000000000000000000000000",
  subject,
  author: { name: "Jane Doe", email: "jane@payments.example" },
  at: "2026-10-01T00:00:00Z",
  files: [{ path: "payments/charge.py", added: 5, removed: 1 }],
  diffHits: 0,
  matchedTerms: [0],
  subjectHits: [],
  inMessage: false,
  ...overrides,
});

test("commit rows say whether the message or the diff matched, and a preview says so too", async (t) => {
  // @covers screen:words-in-messages
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0), { mode: "history" }));
  const items = [
    commitResult("c1", "Cap total retry timeout", {
      subjectHits: [{ start: 10, end: 15 }],
      inMessage: true,
      diffHits: 3,
    }),
    commitResult("c2", "Back off between charge attempts", {
      inMessage: true,
      bodyLine: { text: "So a retry storm can't hammer the processor.", hits: [{ start: 5, end: 10 }] },
    }),
    commitResult("c3", "Rename attempt counter", { diffHits: 2 }),
  ];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  await fromHost(page, "search.done", { seq, searchId: "s1", total: 3, truncated: false, hidden: [], ms: 3 });
  const meta = await page.locator(".row.commit .commit-meta").allInnerTexts();
  assert.match(meta[0], /in message · 3 hits in diff/);
  assert.match(meta[1], /in message only/);
  assert.match(meta[2], /2 hits in diff/);
  assert.doesNotMatch(meta[2], /message/);
  const bodyLine = page.locator('[data-ref="c2"] .commit-body-line');
  assert.equal(await bodyLine.innerText(), "So a retry storm can't hammer the processor.");
  assert.equal(await bodyLine.locator("mark").innerText(), "retry");

  await page.click('[data-ref="c2"]');
  await waitForSent(page, "result.select", { ref: "c2" });
  await fromHost(page, "preview.result", {
    ref: "c2",
    preview: {
      kind: "commit",
      sha: "4c18e9b0000000000000000000000000000000000",
      subject: "Back off between charge attempts",
      body: "So a retry storm can't hammer the processor.",
      author: "Jane Doe <jane@payments.example>",
      at: "2026-10-01T00:00:00Z",
      files: [{ path: "payments/charge.py", added: 5, removed: 1, hiddenByFilter: false }],
      hunks: [
        {
          path: "payments/charge.py",
          header: "@@ -41 +41 @@",
          lines: [{ kind: "add", text: "sleep(delay)", hits: [] }],
        },
      ],
      subjectHits: [],
      bodyHits: [{ start: 5, end: 10 }],
    },
  });
  assert.equal(await page.locator('[data-testid="preview"] pre.body mark').innerText(), "retry");
  assert.equal(await page.locator('[data-testid="matched-in-message"]').count(), 1);
});

test("Enter opens the selected result; ⌘Enter opens it to the side", async (t) => {
  const page = await panelWithResults(t);
  await waitForSent(page, "result.select");
  await page.keyboard.press("Enter");
  assert.deepEqual(await lastSent(page, "result.open"), { ref: "f1", where: "current" });
  await page.keyboard.press("Control+Enter");
  assert.deepEqual(await lastSent(page, "result.open"), { ref: "f1", where: "side" });
});

test("the gear in the search bar asks the host to open settings", async (t) => {
  // @covers msg:settings.open
  const page = await openPanel(t);
  await restore(page);
  await page.click('[data-testid="open-settings"]');
  assert.equal((await sentMessages(page, "settings.open")).length, 1);
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

/** A parsed query for `raw` with one error. */
const withError = (raw) =>
  parsedQuery(raw, null, {
    diagnostics: [{ severity: "error", code: "x", message: raw, span: { start: 0, end: 1 }, fixes: [] }],
  });

test("a parse result for an older seq is dropped and the newest one renders", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  await page.fill(QUERY, "a");
  await page.fill(QUERY, "ab");
  const [olderSeq, newestSeq] = (await sentMessages(page, "query.changed")).map((message) => message.payload.seq);
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
  assert.equal(await page.inputValue(QUERY), "since:6m");
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
  assert.equal(await page.inputValue(QUERY), "/Retry/");
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
  const text = String.raw`f:.*test\.py$ timeout`;
  const path = { kind: "op", op: "f", value: String.raw`.*test\.py$`, match: "regex", span: { start: 0, end: 14 } };
  const root = { kind: "and", children: [path, textNode("timeout", 15)], span: { start: 0, end: 22 } };
  const seq = await typeAndParse(page, text, parsedQuery(text, root));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [CODE_LINE_RESULT] });
  assert.match(
    await page.locator('[data-testid="section-code"] .section-title').innerText(),
    /Code in matching paths/i,
  );
  assert.equal(
    await page.locator('[data-testid="path-scope"]').innerText(),
    String.raw`Only files whose full path matches .*test\.py$ are searched.`,
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
