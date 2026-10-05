// The search panel and the welcome page given hostile or broken host
// messages: markup in every string the daemon passes through (paths, code,
// commit messages, names), offsets that overlap, run backwards or past the
// text, malformed payloads, late messages of an old search, a lot of results,
// and a corrupt saved state. Nothing may run, throw or go missing.
// @covers msg:search.batch msg:search.done msg:parse.result msg:preview.result msg:index.status msg:state.restore
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  FILE_NAME_RESULT,
  fromHost,
  lastSent,
  openPanel,
  openPanelWithSavedState,
  openWelcome,
  pageErrors,
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

/** Markup that would run script, close the bundle's <script>, or decode as an entity if any of it reached innerHTML. */
const MARKUP = `<img src=x onerror="globalThis.__ran=1"></script>&amp;"'<b>`;

/** A code-line result with `text` and `hits`. */
const codeLine = (ref, text, hits, extra = {}) => ({
  kind: "line",
  ref,
  repoId: "r1",
  path: "src/a.py",
  line: 1,
  text,
  hits,
  ...extra,
});

/** A panel searching "retry" as seq `seq` (returned), with no results yet. */
async function searchingPanel(t) {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  return { page, seq };
}

/** Whether any element that markup would have created exists, or its script ran. */
const markupTookEffect = (page) =>
  page.evaluate(() => globalThis.__ran !== undefined || document.querySelector("#app img, #app b") !== null);

/** The refs of the result rows on screen. */
const shownReferences = (page) =>
  page.locator('[data-testid="result"]').evaluateAll((rows) => rows.map((row) => row.dataset.ref));

test("markup in results, previews, notes and banners shows as text and never runs", async (t) => {
  const { page, seq } = await searchingPanel(t);
  await fromHost(page, "index.status", { repos: [{ repoId: "r1", name: MARKUP, tree: "indexing", history: "ready" }] });
  await fromHost(page, "banner", { state: "stopped", message: MARKUP });
  const commit = {
    kind: "commit",
    ref: "c1",
    repoId: "r1",
    sha: MARKUP,
    subject: MARKUP,
    subjectHits: [{ start: 0, end: 4 }],
    author: { name: MARKUP, email: MARKUP },
    at: MARKUP,
    matchedTerms: [0],
    diffHits: 1,
    files: [{ path: MARKUP, added: 1, removed: 1 }],
  };
  const items = [
    { ...FILE_NAME_RESULT, path: MARKUP, nameHits: [{ start: 1, end: 4 }], lastCommit: { author: MARKUP, at: MARKUP } },
    codeLine("l1", MARKUP, [{ start: 0, end: 5, termIndex: 0 }], { path: MARKUP }),
    { kind: "symbol", ref: "s1", repoId: "r1", path: MARKUP, line: 1, name: MARKUP, symbolKind: "class", hits: [] },
    commit,
  ];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  const undo = { title: MARKUP, edits: [] };
  const hidden = [{ reason: "pathFilter", filter: MARKUP, count: 2, unit: "files", undo }];
  await fromHost(page, "search.done", { seq, searchId: "s1", total: 4, truncated: false, hidden, ms: 1 });
  const lines = [MARKUP, MARKUP];
  const filePreview = { kind: "file", path: MARKUP, firstLine: 1, lines, focusLine: 1, hits: [], dirtyLines: [] };
  await fromHost(page, "preview.result", { ref: "f1", preview: { ...filePreview, symbols: [{ name: MARKUP }] } });

  assert.equal(await markupTookEffect(page), false);
  assert.equal(await page.locator('[data-ref="l1"] code.text').textContent(), MARKUP);
  assert.equal(await page.locator('[data-ref="c1"] .subject').textContent(), MARKUP);
  assert.match(await page.locator('[data-testid="preview"]').textContent(), /<img src=x onerror=/);
  assert.match(await page.locator('[data-testid="banner"]').textContent(), /<\/script>&amp;/);
  assert.deepEqual(pageErrors(page), []);
});

test("markup in diagnostics, chips, suggestions and recent queries shows as text", async (t) => {
  const page = await openPanel(t);
  await restore(page, [MARKUP]);
  assert.equal(await page.locator('[data-testid="recent"] code').textContent(), MARKUP);
  await page.fill(QUERY, MARKUP);
  const { seq } = await lastSent(page, "query.changed");
  const repo = { kind: "op", op: "repo", value: MARKUP, span: { start: 0, end: 4 }, resolved: { label: MARKUP } };
  const fixes = [{ title: MARKUP, edits: [] }];
  const diagnostic = { severity: "warning", code: "x", message: MARKUP, span: { start: 1, end: 4 }, fixes };
  const query = parsedQuery(
    MARKUP,
    { kind: "and", children: [textNode(MARKUP, 0), repo] },
    { diagnostics: [diagnostic] },
  );
  const completion = { label: MARKUP, detail: MARKUP, context: MARKUP, note: MARKUP, section: MARKUP, group: "value" };
  const insert = { title: MARKUP, edits: [{ span: { start: 0, end: 4 }, newText: "x" }] };
  await fromHost(page, "parse.result", { seq, query, completions: [{ ...completion, insert }], searchText: MARKUP });
  assert.equal(await page.locator('[data-testid="completion"] .title').textContent(), MARKUP);
  await page.keyboard.press("Escape"); // shows the chips and diagnostics again
  assert.equal(await page.locator(".diagnostic-message").textContent(), MARKUP);
  assert.match(await page.locator('[data-testid="chips"]').textContent(), /<img src=x/);
  assert.equal(await markupTookEffect(page), false);
  assert.deepEqual(pageErrors(page), []);
});

test("a diagnostic span before or past the text marks its end instead of throwing", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  for (const span of [
    { start: -3, end: 1e9 },
    { start: 2e9, end: 2e9 },
  ]) {
    const diagnostics = [{ severity: "error", code: "odd_span", message: "Odd", span, fixes: [] }];
    await typeAndParse(page, "abc", parsedQuery("abc", null, { diagnostics }));
    assert.equal(await page.locator('[data-code="odd_span"]').count(), 1, JSON.stringify(span));
  }
  assert.deepEqual(pageErrors(page), []);
});

/** The highlighted runs of row `ref`'s code: [text, class] per <mark>, and the whole text. */
const marksOf = (page, ref) =>
  page.locator(`[data-ref="${ref}"] code.text`).evaluate((code) => ({
    text: code.textContent,
    marks: [...code.querySelectorAll("mark")].map((mark) => [mark.textContent, mark.className]),
  }));

test("hits that overlap, run backwards, fall outside the line or aren't numbers keep every character", async (t) => {
  const { page, seq } = await searchingPanel(t);
  const hits = [
    { start: 8, end: 2, termIndex: 0 }, // backwards
    { start: -4, end: 3, termIndex: -1 }, // starts before the line; a negative term
    { start: 2, end: 6, termIndex: 1 }, // overlaps the one before
    { start: 5, end: 5, termIndex: 2 }, // empty
    { start: 100, end: 200, termIndex: 3 }, // past the end
    { start: Number.NaN, end: 9, termIndex: 0 },
    { start: 7, end: 9, termIndex: 1.5 },
  ];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [codeLine("l1", "hello world", hits)] });
  assert.deepEqual(await marksOf(page, "l1"), {
    text: "hello world",
    marks: [
      ["hel", "term-color-3"],
      ["lo ", "term-color-1"],
      ["or", "term-color-0"],
    ],
  });
});

test("a hit never splits an emoji in half", async (t) => {
  const { page, seq } = await searchingPanel(t);
  // "😀" is two UTF-16 units: hits end and start in its middle.
  const items = [codeLine("l1", "a😀b😀c", [{ start: 2, end: 4, termIndex: 0 }])];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  assert.deepEqual(await marksOf(page, "l1"), { text: "a😀b😀c", marks: [["😀b", "term-color-0"]] });
});

test("tabs, control and right-to-left override characters in a line render as text", async (t) => {
  const { page, seq } = await searchingPanel(t);
  const text = "x\t\u0000‮evil‬ retry";
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [codeLine("l1", text, [{ start: 10, end: 15 }])],
  });
  assert.deepEqual(await marksOf(page, "l1"), { text, marks: [["retry", "term-color-0"]] });
});

test("a 1 MB line and 10,000 results in 100 batches render, and ↓ stops at the last row", async (t) => {
  const { page, seq } = await searchingPanel(t);
  const longHits = Array.from({ length: 5000 }, (_, index) => ({ start: index * 200, end: index * 200 + 5 }));
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [codeLine("long", "x".repeat(2 ** 20), longHits)],
  });
  assert.equal(await page.locator('[data-ref="long"] mark').count(), 5000);
  for (let batch = 0; batch < 100; batch++) {
    const items = Array.from({ length: 100 }, (_, index) =>
      codeLine(`b${batch}-${index}`, "retry", [{ start: 0, end: 5 }], { path: `f${batch}.py` }),
    );
    await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  }
  await fromHost(page, "search.done", { seq, searchId: "s1", total: 10_001, truncated: false, hidden: [], ms: 1 });
  assert.equal(await page.locator('[data-testid="result"]').count(), 10_001);
  await page.locator('[data-ref="b99-98"]').click();
  await page.focus(QUERY);
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await waitForSent(page, "result.select", { ref: "b99-99" });
  assert.deepEqual(pageErrors(page), []);
});

test("malformed host messages are dropped, and the panel keeps working", async (t) => {
  const page = await openPanel(t);
  await restore(page, ["recent"]);
  const broken = [
    ["index.status", {}],
    ["index.status", { repos: null }],
    ["index.status", { repos: [null] }],
    ["state.restore", {}],
    ["state.restore", { text: "", recent: "x" }],
    ["state.restore", { text: "", recent: [], settings: null }],
    ["banner", null],
    ["parse.result", { seq: 1 }],
    ["search.batch", { seq: 1, searchId: "s1" }],
    ["search.batch", { seq: 1, searchId: "s1", items: [null] }],
    ["search.done", { seq: 1, searchId: "s1" }],
    ["preview.result", null],
    ["no.such.message", {}],
  ];
  for (const [type, payload] of broken) await fromHost(page, type, payload);
  await page.evaluate(() => {
    for (const data of [null, "text", { v: 1 }, { v: 1, type: "focus" }]) {
      globalThis.dispatchEvent(new MessageEvent("message", { data }));
    }
  });
  assert.deepEqual(pageErrors(page), []);
  assert.equal(await page.locator('[data-testid="recent"]').count(), 1, "the recent queries survived");

  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  await fromHost(page, "index.status", { repos: [{ repoId: "r1", name: "payments-api", tree: "ready" }] });
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [FILE_NAME_RESULT] });
  assert.equal(await page.locator('[data-testid="result"]').count(), 1);
  assert.deepEqual(pageErrors(page), []);
});

test("a result of a kind the panel doesn't know is skipped, and the rest of its batch shows", async (t) => {
  const { page, seq } = await searchingPanel(t);
  const items = [{ kind: "notebookCell", ref: "n1", repoId: "r1" }, FILE_NAME_RESULT];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  assert.deepEqual(await shownReferences(page), ["f1"]);
  assert.deepEqual(pageErrors(page), []);
});

test("a late batch of the old search is dropped once a newer query replaces it", async (t) => {
  const { page, seq } = await searchingPanel(t);
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [codeLine("old-1", "retry", [])] });
  await typeAndParse(page, "timeout", parsedQuery("timeout", textNode("timeout", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [codeLine("old-2", "retry", [])] });
  await fromHost(page, "search.done", { seq, searchId: "s1", total: 2, truncated: true, hidden: [], ms: 1 });
  assert.deepEqual(await shownReferences(page), ["old-1"]);
  assert.equal(await page.locator('[data-testid="summary"]').textContent(), "1 code match");
});

test("while the box has errors, the last good search keeps streaming its results", async (t) => {
  const { page, seq } = await searchingPanel(t);
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [codeLine("old-1", "retry", [])] });
  const diagnostics = [
    { severity: "error", code: "unclosed_paren", message: "(", span: { start: 6, end: 7 }, fixes: [] },
  ];
  await typeAndParse(page, "retry (", parsedQuery("retry (", null, { diagnostics }));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [codeLine("old-2", "retry", [])] });
  assert.deepEqual(await shownReferences(page), ["old-1", "old-2"]);
});

test("a corrupt or old saved state leaves the box empty", async (t) => {
  for (const saved of ["text", 42, [1, 2], { text: 5 }, { text: { nested: true } }, { version: 0, query: "x" }]) {
    const page = await openPanelWithSavedState(t, saved);
    assert.equal(await page.inputValue(QUERY), "", JSON.stringify(saved));
    assert.equal((await sentMessages(page, "ready")).length, 1);
    assert.deepEqual(pageErrors(page), []);
  }
  const page = await openPanelWithSavedState(t, { text: "since:2w" });
  assert.equal(await page.inputValue(QUERY), "since:2w", "a good draft is restored");
});

test("the welcome page keeps progress within 0-100% and ignores malformed repo lists", async (t) => {
  const page = await openWelcome(t);
  await fromHost(page, "welcome.state", { preset: "quickOpen", historyDepth: "2y", symbols: true, mac: true });
  await fromHost(page, "index.status", {
    repos: [{ repoId: "a", name: MARKUP, tree: "indexing", history: "ready", progress: 5 }],
  });
  await fromHost(page, "index.status", {});
  assert.equal(await page.getAttribute('[role="progressbar"]', "aria-valuenow"), "100");
  assert.equal(await page.locator(".repo-name").textContent(), MARKUP);
  assert.equal(await markupTookEffect(page), false);
  assert.deepEqual(pageErrors(page), []);
});
