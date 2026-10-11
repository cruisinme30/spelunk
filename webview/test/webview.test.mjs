// The search panel webview: drives the real panel bundle in Chromium with
// host messages written as the extension host would send them, and checks
// what it renders and sends.
// @covers msg:ready msg:state.restore msg:query.changed msg:parse.result msg:search.batch msg:search.done msg:result.select msg:preview.result msg:index.status msg:banner msg:daemon.restart msg:result.open
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  dblclickInPreview,
  FILE_NAME_RESULT,
  fromHost,
  lastSent,
  MISSPELT,
  openPanel,
  openPanelWithSavedState,
  pageErrors,
  panelWithResults,
  parsedQuery,
  QUERY,
  restore,
  searchDone,
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

test("an empty box shows recent queries and all 29 operators", async (t) => {
  // @covers screen:empty-box
  const page = await openPanel(t);
  assert.equal((await sentMessages(page, "ready")).length, 1);
  await restore(page, ["sym:RetryPolicy", "since:2w timeout"]);
  assert.equal(await page.locator('[data-testid="recent"]').count(), 2);
  assert.equal(await page.locator('[data-testid="sheet-op"]').count(), 29);
  // An operator's full and short names share one line.
  const [full, short] = await page.locator('[data-testid="sheet-op"]', { hasText: "file:" }).locator("code").all();
  assert.equal(await short.textContent(), "f:");
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

test("an empty box lists pinned queries, named or not, above the recent ones", async (t) => {
  // @covers screen:pinned-queries
  const page = await openPanel(t);
  await restore(page, ["since:2w timeout", "sym:RetryPolicy", "retry_policy"], {}, [
    { query: "author:jane timeout", name: "Flaky payment tests" },
    { query: "sym:RetryPolicy" },
  ]);
  const titles = await page.locator(".recent > .section-title").allTextContents();
  assert.deepEqual(titles, ["Pinned", "Recent"]);
  assert.deepEqual(await page.locator('[data-testid="pinned"] .pinned-name').allTextContents(), [
    "Flaky payment tests",
  ]);
  assert.deepEqual(await page.locator('[data-testid="pinned"] code').allTextContents(), [
    "author:jane timeout",
    "sym:RetryPolicy",
  ]);
  assert.deepEqual(
    await page.locator('[data-testid="recent"] code').allTextContents(),
    ["since:2w timeout", "retry_policy"],
    "a pinned query isn't listed again under Recent",
  );

  // ↑↓ and ↵ go through the pinned queries first.
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  assert.equal(await page.locator(".recent-row.selected").getAttribute("data-recent"), "since:2w timeout");
  await page.keyboard.press("ArrowUp");
  await page.keyboard.press("ArrowUp");
  await page.keyboard.press("Enter");
  assert.equal(await page.inputValue(QUERY), "author:jane timeout");
});

test("a recent query Ask wrote shows the question, runs the query, and pins named after the question", async (t) => {
  const page = await openPanel(t);
  const question = "where is the retry policy defined";
  await restore(page, [{ query: "sym:RetryPolicy", question }, "timeout"]);
  assert.deepEqual(await page.locator('[data-testid="recent-question"]').allTextContents(), [`${question}?`]);
  assert.deepEqual(await page.locator('[data-testid="recent"] code').allTextContents(), ["sym:RetryPolicy", "timeout"]);
  await page.locator('[data-testid="recent"]').first().click();
  assert.equal(await page.inputValue(QUERY), "sym:RetryPolicy", "the row runs the query, not the question");

  await page.fill(QUERY, "");
  await page.locator(".recent-row").first().hover();
  await page.locator('[data-testid="recent-pin"]').first().click();
  assert.equal(await page.inputValue('[data-testid="pinned-name"]'), `${question}?`, "the name starts as the question");
  assert.deepEqual(
    (await sentMessages(page, "pinned.save")).map((message) => message.payload),
    [{ query: "sym:RetryPolicy", name: `${question}?` }],
  );
});

test("the star pins a recent query and asks for a name, which ↵ saves and Esc skips", async (t) => {
  // @covers msg:pinned.save
  const page = await openPanel(t);
  await restore(page, ["first", "second"]);
  await page.locator(".recent-row").nth(1).hover();
  await page.locator('[data-testid="recent-pin"]').nth(1).click();
  assert.deepEqual(await page.locator('[data-testid="recent"] code').allTextContents(), ["first"]);
  const field = page.locator('[data-testid="pinned-name"]');
  assert.equal(await page.evaluate(() => document.activeElement.dataset.testid), "pinned-name");
  await field.fill("  Second one ");
  await field.press("Enter");
  assert.deepEqual(await page.locator('[data-testid="pinned"] .pinned-name').allTextContents(), ["Second one"]);
  assert.deepEqual(
    (await sentMessages(page, "pinned.save")).map((message) => message.payload),
    [{ query: "second" }, { query: "second", name: "Second one" }],
  );
  assert.equal(
    await page.evaluate(() => document.activeElement.dataset.testid),
    "query",
    "the box gets the focus back",
  );
  assert.equal((await sentMessages(page, "query.changed")).length, 0, "pinning doesn't run the query");

  // A settings refresh while a name is typed leaves the field alone; Esc then keeps the old name.
  await page.locator(".recent-row.pinned").hover();
  await page.locator('[data-testid="pinned-rename"]').click();
  await field.fill("Renamed");
  await restore(page, ["first", "second"], {}, [{ query: "second", name: "Second one" }]);
  assert.equal(await field.inputValue(), "Renamed");
  await field.press("Escape");
  assert.deepEqual(await page.locator('[data-testid="pinned"] .pinned-name').allTextContents(), ["Second one"]);
  assert.equal((await sentMessages(page, "pinned.save")).length, 2, "Esc saves nothing");
  assert.equal((await sentMessages(page, "panel.close")).length, 0, "Esc in the field doesn't close the panel");

  // A blank name clears it.
  await page.locator('[data-testid="pinned-rename"]').click();
  await field.fill(" ");
  await field.press("Enter");
  assert.equal(await page.locator('[data-testid="pinned"] .pinned-name').count(), 0);
  assert.deepEqual(await lastSent(page, "pinned.save"), { query: "second" });
});

test("the filled star and ⇧⌫ unpin a query, which goes back among the recent ones", async (t) => {
  // @covers msg:pinned.remove
  const page = await openPanel(t);
  await restore(page, ["first", "second"], {}, [{ query: "second", name: "Two" }, { query: "other" }]);
  await page.locator(".recent-row.pinned").first().hover();
  await page.locator('[data-testid="pinned-unpin"]').first().click();
  assert.deepEqual(await page.locator('[data-testid="pinned"] code').allTextContents(), ["other"]);
  assert.deepEqual(await page.locator('[data-testid="recent"] code').allTextContents(), ["first", "second"]);
  await page.keyboard.press("Shift+Backspace"); // the first row: "other"
  assert.equal(await page.locator('[data-testid="pinned"]').count(), 0);
  assert.deepEqual(await page.locator(".recent > .section-title").allTextContents(), ["Recent"]);
  assert.deepEqual(
    (await sentMessages(page, "pinned.remove")).map((message) => message.payload.query),
    ["second", "other"],
  );
  assert.equal((await sentMessages(page, "recent.remove")).length, 0);
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

/** CODE_LINE_RESULT moved to `path`, with a rank reason if given. */
const lineIn = (path, rankReason) => ({ ...CODE_LINE_RESULT, ref: path, path, ...(rankReason && { rankReason }) });

test("section titles say results are best match first, and each file says why it ranks where it does", async (t) => {
  // @covers screen:ranked-results
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [
      { ...FILE_NAME_RESULT, path: "http/retry.py", rankReason: "definition" },
      lineIn("http/retry.py", "definition"),
      lineIn("src/client.py", "open"),
      lineIn("tests/retry_test.py", "test"),
    ],
  });
  assert.match(await page.locator('[data-testid="section-files"] .section-title').innerText(), /best match first/i);
  assert.match(await page.locator('[data-testid="section-code"] .section-title').innerText(), /best match first/i);
  const groups = page.locator('[data-testid="code-group"]');
  assert.equal(await groups.nth(0).locator('[data-testid="rank-reason"]').innerText(), "definition");
  assert.equal(await groups.nth(1).locator('[data-testid="rank-reason"]').innerText(), "open");
  assert.doesNotMatch((await groups.nth(1).getAttribute("class")) ?? "", /demoted/);
  assert.equal(await groups.nth(2).locator('[data-testid="rank-reason"]').innerText(), "test");
  assert.match((await groups.nth(2).getAttribute("class")) ?? "", /demoted/);
});

/** A file-name result for "cfg" in http/config.*, with c, f and g each highlighted. */
const fuzzyName = (path, ref) => ({
  ...FILE_NAME_RESULT,
  ref,
  path,
  nameHits: [5, 8, 10].map((start) => ({ start, end: start + 1 })),
});

test("a file name matched by its letters in order highlights each run of them", async (t) => {
  // @covers screen:fuzzy-file-names
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "cfg", parsedQuery("cfg", textNode("cfg", 0)));
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [fuzzyName("http/config.py", "f1"), fuzzyName("http/config.yaml", "f2")],
  });
  const marks = page.locator('[data-testid="section-files"] .row.file').first().locator("mark");
  assert.deepEqual(await marks.allInnerTexts(), ["c", "f", "g"]);
});

test("order:file, or the order setting, makes the titles say path order", async (t) => {
  // @covers screen:path-order
  const page = await openPanel(t);
  await restore(page, [], { order: "file" });
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [CODE_LINE_RESULT] });
  assert.match(await page.locator('[data-testid="section-code"] .section-title').innerText(), /path order/i);
  const best = parsedQuery("order:best retry", textNode("retry", 11), {
    globals: { case: null, count: null, type: null, order: "best" },
  });
  const next = await typeAndParse(page, "order:best retry", best);
  await fromHost(page, "search.batch", { seq: next, searchId: "s2", items: [CODE_LINE_RESULT] });
  assert.match(await page.locator('[data-testid="section-code"] .section-title').innerText(), /best match first/i);
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

test("a double-click in the preview opens its file to the side, at the word double-clicked", async (t) => {
  // @covers msg:result.open
  const page = await panelWithResults(t);
  await page.keyboard.press("ArrowDown");
  await waitForSent(page, "result.select", { ref: "l1" });
  await fromHost(page, "preview.result", {
    ref: "l1",
    preview: {
      kind: "file",
      path: "src/payments/client.py",
      firstLine: 41,
      lines: ["    import retry", "self.retry_policy = RetryPolicy()"],
      focusLine: 42,
      hits: [{ line: 42, ranges: [{ start: 5, end: 17, termIndex: 0 }] }],
      dirtyLines: [],
    },
  });
  // One click only selects, as it always has.
  await page.click('[data-testid="preview"] .code-line[data-line="41"] code');
  assert.equal(await lastSent(page, "result.open"), undefined);

  await dblclickInPreview(page, 42, "RetryPolicy");
  assert.deepEqual(await lastSent(page, "result.open"), {
    ref: "l1",
    where: "side",
    position: { line: 42, column: 21, length: 11 },
  });
  // A line above the match, indented, counts its indentation.
  await dblclickInPreview(page, 41, "retry");
  assert.deepEqual((await lastSent(page, "result.open")).position, { line: 41, column: 12, length: 5 });
  // A word before the match's mark, at the line's start.
  await dblclickInPreview(page, 42, "self");
  assert.deepEqual((await lastSent(page, "result.open")).position, { line: 42, column: 1, length: 4 });
  // The line number opens at the line's start.
  await page.dblclick('[data-testid="preview"] .code-line[data-line="41"] .line-number');
  assert.deepEqual((await lastSent(page, "result.open")).position, { line: 41, column: 1, length: 0 });
  assert.deepEqual(pageErrors(page), []);
});

/** A file preview of client.py, at line 42 inside RetryClient.charge, with the file's Outline. */
const PREVIEW_WITH_OUTLINE = {
  kind: "file",
  path: "src/payments/client.py",
  firstLine: 41,
  lines: ["    def charge(self):", "        self.retry_policy = RetryPolicy()"],
  focusLine: 42,
  hits: [{ line: 42, ranges: [{ start: 13, end: 25, termIndex: 0 }] }],
  dirtyLines: [],
  symbols: [
    { name: "RetryClient", line: 30, kind: "class", depth: 0 },
    { name: "__init__", line: 33, kind: "method", depth: 1 },
    { name: "charge", line: 41, kind: "method", depth: 1 },
    { name: "connect", line: 60, kind: "function", depth: 0 },
  ],
};

test("the Outline lists the file's definitions, nested, closed until opened; a click opens one to the side", async (t) => {
  // @covers screen:file-outline
  const page = await panelWithResults(t);
  await page.keyboard.press("ArrowDown");
  await waitForSent(page, "result.select", { ref: "l1" });
  await fromHost(page, "preview.result", { ref: "l1", preview: PREVIEW_WITH_OUTLINE });
  const toggle = page.locator('[data-testid="outline-toggle"]');
  const outline = page.locator('[data-testid="preview"] .outline-pane');
  assert.equal(await toggle.textContent(), "Outline4");
  assert.equal(await toggle.getAttribute("aria-expanded"), "false");
  assert.equal(await outline.isVisible(), false);

  await toggle.click();
  assert.equal(await toggle.getAttribute("aria-expanded"), "true");
  assert.equal(await outline.isVisible(), true);
  const rows = await outline
    .locator(".outline-row")
    .evaluateAll((buttons) =>
      buttons.map((button) => `${button.style.getPropertyValue("--depth")} ${button.getAttribute("aria-label")}`),
    );
  assert.deepEqual(rows, [
    "0 RetryClient, class, line 30",
    "1 __init__, method, line 33",
    "1 charge, method, line 41",
    "0 connect, function, line 60",
  ]);
  // The definition the match is in is marked.
  assert.equal(await outline.locator('[aria-current="true"] .outline-name').textContent(), "charge");
  // Members sit further right than the class they're in.
  const left = async (name) =>
    (await outline.locator(".outline-row", { hasText: name }).locator(".outline-name").boundingBox()).x;
  assert.ok((await left("__init__")) > (await left("RetryClient")));

  await outline.locator(".outline-row", { hasText: "connect" }).click();
  assert.deepEqual(await lastSent(page, "result.open"), {
    ref: "l1",
    where: "side",
    position: { line: 60, column: 1, length: 0 },
  });

  // It stays open for the next file, and across a reload; closing it is kept too.
  assert.equal(await page.evaluate(() => globalThis.__savedState.outlineOpen), true);
  await fromHost(page, "preview.result", { ref: "l1", preview: PREVIEW_WITH_OUTLINE });
  assert.equal(await outline.isVisible(), true);
  await toggle.click();
  assert.equal(await outline.isVisible(), false);
  assert.equal(await page.evaluate(() => globalThis.__savedState.outlineOpen), false);
  assert.deepEqual(pageErrors(page), []);
});

test("a file with no definitions has no Outline, and a reload keeps an open Outline open", async (t) => {
  const page = await openPanelWithSavedState(t, { outlineOpen: true });
  await restore(page);
  const seq = await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [FILE_NAME_RESULT, CODE_LINE_RESULT] });
  await searchDone(page, seq, { total: 2, ms: 3 });
  await fromHost(page, "preview.result", { ref: "f1", preview: { ...PREVIEW_WITH_OUTLINE, symbols: [] } });
  assert.equal(await page.locator('[data-testid="outline-toggle"]').count(), 0);
  assert.equal(await page.locator(".outline-pane").count(), 0);
  await fromHost(page, "preview.result", { ref: "f1", preview: PREVIEW_WITH_OUTLINE });
  assert.equal(await page.locator('[data-testid="preview"] .outline-pane').isVisible(), true);
  assert.deepEqual(pageErrors(page), []);
});

/** Drags the sash `testId` names `dx` pixels to the right (left when negative), as a person would. */
async function dragSash(page, testId, dx) {
  const box = await page.locator(`[data-testid="${testId}"]`).boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + dx, box.y + box.height / 2, { steps: 5 });
  await page.mouse.up();
}

test("dragging the line between the results and the preview resizes them, and the width survives a reload", async (t) => {
  const page = await panelWithResults(t);
  await page.setViewportSize({ width: 1200, height: 700 });
  const resultsWidth = async (on = page) => (await on.locator("#results").boundingBox()).width;
  const before = await resultsWidth();
  await dragSash(page, "results-divider", 120);
  assert.ok(Math.abs((await resultsWidth()) - (before + 120)) <= 2, "the results follow the pointer");
  const dragged = await resultsWidth();
  assert.equal(await page.evaluate(() => globalThis.__savedState.resultsWidth), dragged);

  // ← moves it too, and neither side can be dragged shut.
  await page.locator('[data-testid="results-divider"]').focus();
  await page.keyboard.press("ArrowLeft");
  assert.equal(Math.round(await resultsWidth()), Math.round(dragged) - 24);
  await dragSash(page, "results-divider", -2000);
  assert.equal(Math.round(await resultsWidth()), 240);
  await dragSash(page, "results-divider", 2000);
  assert.ok((await page.locator("#preview").boundingBox()).width >= 320, "the preview keeps 320px");

  // A reloaded panel keeps the width; a double-click puts back the default split.
  const kept = await resultsWidth();
  const reloaded = await openPanelWithSavedState(t, await page.evaluate(() => globalThis.__savedState));
  await reloaded.setViewportSize({ width: 1200, height: 700 });
  await restore(reloaded);
  const seq = await typeAndParse(reloaded, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  await fromHost(reloaded, "search.batch", { seq, searchId: "s1", items: [FILE_NAME_RESULT, CODE_LINE_RESULT] });
  await searchDone(reloaded, seq, { total: 2, ms: 3 });
  assert.equal(Math.round(await resultsWidth(reloaded)), Math.round(kept));
  await reloaded.locator('[data-testid="results-divider"]').dblclick();
  assert.equal(Math.round(await resultsWidth(reloaded)), Math.round(before));
  assert.equal(await reloaded.evaluate(() => globalThis.__savedState.resultsWidth), undefined);
  assert.deepEqual(pageErrors(page), []);
});

test("dragging the Outline's left edge resizes it, and the width is kept for the next file", async (t) => {
  const page = await panelWithResults(t);
  await page.setViewportSize({ width: 1200, height: 700 });
  await page.keyboard.press("ArrowDown");
  await waitForSent(page, "result.select", { ref: "l1" });
  await fromHost(page, "preview.result", { ref: "l1", preview: PREVIEW_WITH_OUTLINE });
  await page.locator('[data-testid="outline-toggle"]').click();
  const outlineWidth = async () => (await page.locator(".outline-pane").boundingBox()).width;
  assert.equal(Math.round(await outlineWidth()), 210);
  // Dragging left widens it, as the Outline sits on the right.
  await dragSash(page, "outline-divider", -60);
  assert.ok(Math.abs((await outlineWidth()) - 270) <= 2, "the Outline follows the pointer");
  assert.equal(Math.round(await page.evaluate(() => globalThis.__savedState.outlineWidth)), 270);
  await dragSash(page, "outline-divider", 2000);
  assert.equal(Math.round(await outlineWidth()), 140);

  await fromHost(page, "preview.result", { ref: "l1", preview: PREVIEW_WITH_OUTLINE });
  assert.equal(Math.round(await outlineWidth()), 140);
  await page.locator('[data-testid="outline-divider"]').dblclick();
  assert.equal(Math.round(await outlineWidth()), 210);
  assert.equal(await page.evaluate(() => globalThis.__savedState.outlineWidth), undefined);
  assert.deepEqual(pageErrors(page), []);
});

test("the line between the results and the preview hides with the preview in a narrow panel", async (t) => {
  const page = await panelWithResults(t);
  await page.setViewportSize({ width: 641, height: 700 });
  assert.equal(await page.locator('[data-testid="results-divider"]').isVisible(), true);
  await page.setViewportSize({ width: 640, height: 700 });
  assert.equal(await page.locator('[data-testid="results-divider"]').isVisible(), false);
});

test("↑↓ go through the sections top to bottom, in the order their batches came, and stop at either end", async (t) => {
  const page = await panelWithResults(t);
  const { seq } = await lastSent(page, "query.changed");
  // A later batch adds to every section: the file name goes above the code lines that came before it.
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [
      { ...CODE_LINE_RESULT, ref: "l2", line: 43 },
      { ...FILE_NAME_RESULT, ref: "f2", path: "src/payments/retry.py" },
      {
        kind: "symbol",
        ref: "s1",
        repoId: "r1",
        path: "a.py",
        line: 3,
        name: "retry",
        symbolKind: "function",
        hits: [],
      },
    ],
  });
  const selected = () => page.getAttribute('[aria-selected="true"]', "data-ref");
  const walk = [];
  for (let step = 0; step < 6; step++) {
    walk.push(await selected());
    await page.keyboard.press("ArrowDown");
  }
  assert.deepEqual(walk, ["f1", "f2", "s1", "l1", "l2", "l2"]);
  for (let step = 0; step < 6; step++) await page.keyboard.press("ArrowUp");
  assert.equal(await selected(), "f1");
});

/** The selected result row. */
const SELECTED_RESULT = '[data-testid="result"][aria-selected="true"]';

test("keys pressed while a search runs again keep their place when its results replace the old ones", async (t) => {
  const page = await panelWithResults(t);
  const lines = [42, 43, 44].map((line) => ({ ...CODE_LINE_RESULT, ref: `old${line}`, line }));
  const { seq } = await lastSent(page, "query.changed");
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: lines });
  // Suggestions show; Esc closes them and searches the box as typed.
  const suggestion = { label: "x", detail: "x", group: "value", insert: { title: "x", edits: [] } };
  const query = parsedQuery("retry_policy", textNode("retry_policy", 0));
  await fromHost(page, "parse.result", { seq, query, completions: [suggestion] });
  assert.equal(await page.locator('[data-testid="completions"]').isVisible(), true);
  await page.keyboard.press("Escape");
  const { seq: again, asTyped } = await lastSent(page, "query.changed");
  assert.equal(asTyped, true);
  // Before its results come, ↓ moves on through the results on screen.
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  assert.equal(await page.getAttribute(SELECTED_RESULT, "data-ref"), "old43");
  // The same results under the new search's refs: the selection stays on the same line, even though the batch that
  // has it isn't the first.
  const fresh = [FILE_NAME_RESULT, CODE_LINE_RESULT, ...lines].map((item) => ({
    ...item,
    ref: item.ref.replace("old", "new"),
  }));
  await fromHost(page, "search.batch", { seq: again, searchId: "s2", items: fresh.slice(0, 2) });
  assert.equal(await page.locator(SELECTED_RESULT).count(), 0, "no jump to the first row meanwhile");
  await fromHost(page, "search.batch", { seq: again, searchId: "s2", items: fresh.slice(2) });
  assert.equal(await page.getAttribute(SELECTED_RESULT, "data-ref"), "new43");
  await waitForSent(page, "result.select", { ref: "new43" });

  // A search that doesn't find the selected result selects its first row when it is done.
  await page.fill(QUERY, "retry");
  const { seq: other } = await lastSent(page, "query.changed");
  await fromHost(page, "search.batch", { seq: other, searchId: "s3", items: [{ ...FILE_NAME_RESULT, ref: "f9" }] });
  assert.equal(await page.locator(SELECTED_RESULT).count(), 0);
  await fromHost(page, "search.done", { seq: other, searchId: "s3", total: 1, truncated: false, hidden: [], ms: 1 });
  assert.equal(await page.getAttribute(SELECTED_RESULT, "data-ref"), "f9");
});

test("under type:added, chips, commit rows and the hidden note speak of added lines", async (t) => {
  // @covers screen:added-removed-lines
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  const query = "type:added retry";
  const type = { kind: "op", op: "type", value: "added", match: "literal", span: { start: 0, end: 10 } };
  const root = { kind: "and", children: [type, textNode("retry", 11)], span: { start: 0, end: 16 } };
  const parsed = parsedQuery(query, root, { mode: "history", globals: { case: null, count: null, type: "added" } });
  const seq = await typeAndParse(page, query, parsed);
  const chips = await page.locator('[data-testid="chips"]').innerText();
  assert.match(chips, /added lines only/);
  assert.match(chips, /added line contains\s*retry/);
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [commitResult("c1", "Add retry to charge", { diffHits: 3 })],
  });
  const undo = { title: "Search every changed line", edits: [{ span: { start: 0, end: 10 }, newText: "type:commit" }] };
  await searchDone(page, seq, {
    total: 1,
    ms: 3,
    hidden: [{ reason: "type", filter: "type:added", count: 1, unit: "commits", undo }],
  });
  const meta = await page.locator(".row.commit .commit-meta").innerText();
  assert.match(meta, /3 hits in added lines/);
  assert.doesNotMatch(meta, /in diff/);
  const note = page.locator('[data-testid="hidden-notes"] .note');
  assert.equal(await note.locator("span").first().innerText(), "1 commit for retry hidden by type:added");
  await note.locator('[data-testid="show-hidden"]').click();
  assert.equal(await page.inputValue(QUERY), "type:commit retry");
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

test("a commit's body line longer than the column ends in an ellipsis inside its row", async (t) => {
  const page = await openPanel(t);
  await page.setViewportSize({ width: 900, height: 500 });
  await restore(page);
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0), { mode: "history" }));
  const text = `Retry the charge once more, ${"then back off for longer each time, ".repeat(6)}until the gateway answers`;
  const bodyLine = { text, hits: [{ start: 0, end: 5, termIndex: 0 }] };
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [commitResult("c1", "Cap retries", { bodyLine })],
  });
  const sizes = await page.$eval('[data-ref="c1"]', (row) => {
    const body = row.querySelector(".commit-body-line");
    return {
      cut: body.scrollWidth > body.clientWidth,
      inside: body.getBoundingClientRect().right <= row.getBoundingClientRect().right,
      ellipsis: getComputedStyle(body).textOverflow,
    };
  });
  assert.deepEqual(sizes, { cut: true, inside: true, ellipsis: "ellipsis" });
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
  await searchDone(page, seq, { total: 3, ms: 3 });
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
      files: [
        { path: "payments/charge.py", added: 5, removed: 1, hiddenByFilter: false },
        { path: "vendor/backoff.py", added: 2, removed: 0, hiddenByFilter: true },
      ],
      hunks: [
        {
          path: "payments/charge.py",
          header: "@@ -41 +41 @@",
          lines: [{ kind: "add", text: "sleep(delay)", hits: [] }],
        },
        {
          path: "vendor/backoff.py",
          header: "@@ -0,0 +1,2 @@",
          lines: [{ kind: "add", text: "def backoff(): ...", hits: [] }],
        },
      ],
      subjectHits: [],
      bodyHits: [{ start: 5, end: 10 }],
    },
  });
  assert.equal(await page.locator('[data-testid="preview"] pre.body mark').innerText(), "retry");
  assert.equal(await page.locator('[data-testid="matched-in-message"]').count(), 1);
  // A file the query's filters left out (f:, lang: or is:test) stays hidden until Show all.
  assert.match(
    await page.locator('[data-testid="preview"] .files').innerText(),
    /1 other changed file hidden by filters/,
  );
  assert.equal(await page.locator('[data-testid="preview"] .hunk').count(), 1);
  await page.click('[data-testid="show-all-files"]');
  assert.equal(await page.locator('[data-testid="preview"] .hunk').count(), 2);
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

test("after a click on a result, the keys still move through the results and open them", async (t) => {
  const page = await panelWithResults(t);
  await page.click('[data-ref="f1"]');
  assert.equal(await page.evaluate(() => document.activeElement?.id), "query");
  await page.keyboard.press("ArrowDown");
  assert.equal(await page.getAttribute('[data-ref="l1"]', "aria-selected"), "true");
  await page.keyboard.press("Enter");
  assert.deepEqual(await lastSent(page, "result.open"), { ref: "l1", where: "current" });

  // Text dragged over in a row stays selected, to be copied.
  const text = await page.locator('[data-ref="l1"] .text').boundingBox();
  const y = text.y + text.height / 2;
  await page.mouse.move(text.x + 2, y);
  await page.mouse.down();
  await page.mouse.move(text.x + text.width - 2, y, { steps: 5 });
  await page.mouse.up();
  assert.notEqual(await page.evaluate(() => document.getSelection()?.toString()), "");
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
  await typeAndParse(page, "sinse:6m", MISSPELT);
  assert.equal(await page.locator('[data-code="unknown_operator"]').count(), 1);
  await page.keyboard.press("Control+.");
  assert.equal(await page.inputValue(QUERY), "since:6m");
  assert.equal((await lastSent(page, "query.changed")).text, "since:6m");
});

test("a diagnostic with an empty span marks the character at it once", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const warning = { severity: "warning", code: "history_full_scan", message: "Reads all history", fixes: [] };
  await typeAndParse(
    page,
    "retry",
    parsedQuery("retry", textNode("retry", 0), { diagnostics: [{ ...warning, span: { start: 2, end: 2 } }] }),
  );
  assert.equal(await page.textContent('[data-testid="diagnostics"] .snippet'), "retry");
  assert.equal(await page.textContent('[data-testid="diagnostics"] .bad'), "t");
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

test("under smart case, Aa has a dot, follows the query's capitals, and writes the opposite case:", async (t) => {
  // @covers screen:smart-case
  const page = await openPanel(t);
  await restore(page, [], { caseSensitive: "smart" });
  const button = page.locator('[data-testid="toggle-case"]');
  await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  assert.equal(await button.getAttribute("aria-pressed"), "false");
  assert.match((await button.getAttribute("class")) ?? "", /\bsmart\b/);
  assert.match((await button.getAttribute("title")) ?? "", /ignoring case until the query has a capital/);
  await typeAndParse(page, "Retry", parsedQuery("Retry", textNode("Retry", 0), { hasCapital: true }));
  assert.equal(await button.getAttribute("aria-pressed"), "true");
  assert.match((await button.getAttribute("title")) ?? "", /matching case, since the query has a capital/);
  await button.click();
  assert.equal(await page.inputValue(QUERY), "case:no Retry");
});

test("a case:smart note says why nothing was found and offers to ignore case", async (t) => {
  const page = await openPanel(t);
  await restore(page, [], { caseSensitive: "smart" });
  const seq = await typeAndParse(page, "Retry", parsedQuery("Retry", textNode("Retry", 0), { hasCapital: true }));
  const undo = { title: "Ignore case", edits: [{ span: { start: 0, end: 0 }, newText: "case:no " }] };
  await searchDone(page, seq, { hidden: [{ reason: "case", filter: "case:smart", count: 18, unit: "matches", undo }] });
  assert.match(await page.locator('[data-testid="no-results"]').innerText(), /Smart case matches case/);
  const note = page.locator('[data-testid="hidden-notes"] .note');
  assert.equal(await note.locator("span").first().innerText(), "18 matches hidden by case:smart");
  assert.equal(await note.locator('[data-testid="show-hidden"]').innerText(), "Ignore case");
});

test("ab reflects word:yes and removes it from the text", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const root = {
    kind: "and",
    span: { start: 0, end: 14 },
    children: [
      { kind: "op", op: "word", value: "yes", match: "literal", span: { start: 0, end: 8 } },
      { kind: "text", value: "retry", match: "literal", termIndex: 0, span: { start: 9, end: 14 } },
    ],
  };
  await typeAndParse(
    page,
    "word:yes retry",
    parsedQuery("word:yes retry", root, { globals: { case: null, count: null, type: null, word: "yes" } }),
  );
  assert.equal(await page.getAttribute('[data-testid="toggle-word"]', "aria-pressed"), "true");
  assert.equal(await page.getAttribute('[data-testid="toggle-case"]', "aria-pressed"), "false");
  await page.click('[data-testid="toggle-word"]');
  assert.equal(await page.inputValue(QUERY), "retry");
});

test("a word:yes note offers to match parts of words", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "word:yes retry", parsedQuery("word:yes retry", textNode("retry", 9)));
  const undo = { title: "Match parts of words", edits: [{ span: { start: 0, end: 9 }, newText: "" }] };
  await searchDone(page, seq, { hidden: [{ reason: "word", filter: "word:yes", count: 4, unit: "matches", undo }] });
  const note = page.locator('[data-testid="hidden-notes"] .note');
  assert.equal(await note.locator("span").first().innerText(), "4 matches hidden by word:yes");
  assert.equal(await note.locator('[data-testid="show-hidden"]').innerText(), "Match parts of words");
});

test("exact matching's note counts the file names match:fuzzy would add, and Show them adds it", async (t) => {
  // @covers screen:fuzzy-file-names
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "rtrypol", parsedQuery("rtrypol", textNode("rtrypol", 0)));
  const undo = { title: "Show them", edits: [{ span: { start: 0, end: 0 }, newText: "match:fuzzy " }] };
  await searchDone(page, seq, { hidden: [{ reason: "match", filter: "match:exact", count: 3, unit: "files", undo }] });
  const note = page.locator('[data-testid="hidden-notes"] .note');
  assert.equal(await note.locator("span").first().innerText(), "3 file names have these letters in order");
  await note.locator('[data-testid="show-hidden"]').click();
  assert.equal(await page.inputValue(QUERY), "match:fuzzy rtrypol");
});

test("hidden-result notes use singular units and name the filter", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "x -f:vendor/", parsedQuery("x -f:vendor/", textNode("x", 0)));
  const undo = { title: "Remove -f:vendor/", edits: [{ span: { start: 1, end: 12 }, newText: "" }] };
  await searchDone(page, seq, {
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
  await searchDone(page, seq, { hidden: [{ reason: "case", filter: "case:yes", count: 5, unit: "matches", undo }] });
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

  await page.dblclick('[data-testid="load-more"]', { force: true });
  assert.deepEqual(await sentMessages(page, "results.more").then((sent) => sent.map((message) => message.payload)), [
    { searchId: "s1", cursor: "page-2" },
  ]);

  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [CODE_LINE_RESULT] });
  await fromHost(page, "search.done", firstPage);
  assert.equal(await page.locator('[data-testid="result"]').count(), 2, "the next page is appended");
  assert.equal(await page.locator('[data-testid="load-more"]').count(), 0, "the last page has no Load more");
});

/** The facets the daemon sends with a search of the payments workspace for retry_policy. */
const FACETS = [
  {
    field: "repo",
    more: 0,
    buckets: [
      { label: "payments-api", count: 5, filter: "repo:payments-api" },
      { label: "shared-libs", count: 2, filter: "repo:shared-libs" },
    ],
  },
  { field: "lang", more: 0, buckets: [{ label: "Python", count: 7, filter: "lang:python" }] },
  {
    field: "folder",
    more: 2,
    buckets: ["src", "tests", "http", "docs", "scripts", "vendor"].map((folder, index) => ({
      label: `${folder}/`,
      count: 6 - index,
      filter: `f:^${folder}/`,
    })),
  },
];

/** The facet row's buckets as "label count", by field. */
const facetRow = (page) =>
  page.$$eval('[data-testid="facets"] .facet', (groups) =>
    Object.fromEntries(
      groups.map((group) => [
        group.dataset.field,
        [...group.querySelectorAll("button")].map((button) => button.textContent),
      ]),
    ),
  );

test("the facet row counts the results, and a click keeps a bucket or Alt-click leaves it out", async (t) => {
  // @covers screen:result-facets
  const page = await panelWithResults(t);
  const seq = (await lastSent(page, "query.changed")).seq;
  await searchDone(page, seq, { total: 7, facets: FACETS });
  assert.deepEqual(await facetRow(page), {
    repo: ["payments-api5", "shared-libs2"],
    lang: ["Python7"],
    // five buckets, then the sixth and the daemon's two more behind +3
    folder: ["src/6", "tests/5", "http/4", "docs/3", "scripts/2", "+3"],
  });
  const bucket = await page.locator('[data-filter="repo:payments-api"]').boundingBox();
  assert.ok(bucket.height < 30, `a bucket is a small pill, not ${String(bucket.height)}px tall`);
  await page.click('[data-testid="facets"] [data-field="folder"] [data-testid="facet-more"]');
  assert.ok((await facetRow(page)).folder.includes("vendor/1"), "+3 shows every bucket sent");
  await searchDone(page, seq, { total: 7, facets: FACETS }); // Load more's page of the same search
  assert.ok((await facetRow(page)).folder.includes("vendor/1"), "still every bucket");
  await page.click('[data-filter="repo:payments-api"]');
  assert.equal(await page.inputValue(QUERY), "retry_policy repo:payments-api");
  await page.fill(QUERY, "retry_policy");
  await page.click('[data-filter="repo:shared-libs"]', { modifiers: ["Alt"] });
  assert.equal(await page.inputValue(QUERY), "retry_policy -repo:shared-libs"); // The next search shows five buckets again.
  const next = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  await fromHost(page, "search.done", {
    seq: next,
    searchId: "s2",
    total: 7,
    truncated: false,
    hidden: [],
    ms: 1,
    facets: FACETS,
  });
  assert.equal((await facetRow(page)).folder.at(-1), "+3");
});

test("a left-out bucket stays in the facet row, struck through, and a click brings it back", async (t) => {
  // @covers screen:result-facets
  const page = await panelWithResults(t);
  await searchDone(page, (await lastSent(page, "query.changed")).seq, { total: 7, facets: FACETS });
  const raw = "retry_policy -repo:shared-libs";
  const repo = { kind: "op", op: "repo", value: "shared-libs", match: "regex", span: { start: 14, end: 30 } };
  const root = {
    kind: "and",
    children: [textNode("retry_policy", 0), { kind: "not", child: repo, span: { start: 13, end: 30 } }],
  };
  const seq = await typeAndParse(page, raw, parsedQuery(raw, root));
  await fromHost(page, "search.batch", { seq, searchId: "s2", items: [FILE_NAME_RESULT] });
  // shared-libs found nothing, so the daemon no longer counts it
  await searchDone(page, seq, {
    searchId: "s2",
    total: 5,
    facets: [{ ...FACETS[0], buckets: [FACETS[0].buckets[0]] }, FACETS[1]],
  });
  const leftOut = page.locator('[data-filter="repo:shared-libs"]');
  assert.equal(await leftOut.getAttribute("class"), "facet-bucket left-out");
  assert.equal(await leftOut.getAttribute("aria-pressed"), "true");
  await leftOut.click();
  assert.equal(await page.inputValue(QUERY), "retry_policy");
});

test("the facet row stays hidden when there is nothing to choose between", async (t) => {
  const page = await panelWithResults(t);
  await searchDone(page, (await lastSent(page, "query.changed")).seq, { total: 2, facets: [FACETS[1]] });
  assert.equal(await page.isVisible('[data-testid="facets"]'), false);
});
