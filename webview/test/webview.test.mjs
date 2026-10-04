// Contract 2, webview side: drives the real panel bundle in Chromium with
// recorded host messages and checks what it renders and sends.
// @covers msg:ready msg:state.restore msg:query.changed msg:parse.result msg:search.batch msg:search.done msg:result.select msg:preview.result msg:index.status msg:banner msg:daemon.restart msg:panel.close msg:focus msg:result.open
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { after, before, test } from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const harnessUrl = pathToFileURL(join(dirname(fileURLToPath(import.meta.url)), "out/harness.html")).href;
const UI_SETTINGS = {
  typingDelayMs: 0,
  openTrigger: "doubleClick",
  preview: true,
  showParsedQuery: true,
  caseSensitive: false,
};

let browser;
before(async () => {
  browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
});
after(async () => browser?.close());

/** Opens a fresh panel, closed again when the test ends (even if it fails). */
async function openPanel(t) {
  const page = await browser.newPage();
  t.after(() => page.close());
  await page.goto(harnessUrl);
  return page;
}

const sentMessages = (page, type) => page.evaluate((t) => window.__sent.filter((m) => m.type === t), type);
const lastSent = async (page, type) => (await sentMessages(page, type)).at(-1)?.payload;
const fromHost = (page, type, payload) => page.evaluate(([t, p]) => window.__host(t, p), [type, payload]);
const restore = (page, recent = []) => fromHost(page, "state.restore", { text: "", recent, settings: UI_SETTINGS });

const parsedQuery = (raw, root, overrides = {}) => ({
  version: 1,
  raw,
  root,
  mode: "workingTree",
  diagnostics: [],
  globals: { case: null, count: null, type: null },
  ...overrides,
});
const textNode = (value, start, termIndex = 0) => ({
  kind: "text",
  value,
  match: "literal",
  termIndex,
  span: { start, end: start + value.length },
});

/** Types a query and answers its parse with `query`; returns the seq. */
async function typeAndParse(page, text, query) {
  await page.fill('[data-testid="query"]', text);
  const { seq } = await lastSent(page, "query.changed");
  await fromHost(page, "parse.result", { seq, query, completions: [] });
  return seq;
}

const RESULT_ITEMS = [
  {
    kind: "file",
    ref: "f1",
    repoId: "r1",
    path: "src/payments/retry_policy.py",
    nameHits: [{ start: 13, end: 25 }],
    dirty: false,
  },
  {
    kind: "line",
    ref: "l1",
    repoId: "r1",
    path: "src/payments/client.py",
    line: 42,
    text: "self.retry_policy = RetryPolicy()",
    hits: [{ start: 5, end: 17, termIndex: 0 }],
  },
];

/** A panel showing RESULT_ITEMS for "retry_policy". */
async function panelWithResults(t) {
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  const seq = await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: RESULT_ITEMS });
  await fromHost(page, "search.done", { seq, searchId: "s1", total: 2, truncated: false, hidden: [], ms: 3 });
  return page;
}

test("an empty box shows recent queries and all 16 operators", async (t) => {
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

test("streamed results render in sections with a summary", async (t) => {
  const page = await panelWithResults(t);
  assert.equal(await page.locator('[data-testid="result"]').count(), 2);
  assert.match(await page.locator('[data-testid="summary"]').innerText(), /1 file name · 1 code match/);
});

test("the first result is selected and previewed; ↓ moves the selection", async (t) => {
  const page = await panelWithResults(t);
  await page.waitForFunction(() => window.__sent.some((m) => m.type === "result.select"));
  assert.equal((await lastSent(page, "result.select")).ref, "f1");
  await page.keyboard.press("ArrowDown");
  await page.waitForFunction(() => window.__sent.filter((m) => m.type === "result.select").at(-1).payload.ref === "l1");
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
  assert.equal(await page.locator("#preview mark").count(), 1);
});

test("Enter opens the selected result; ⌘Enter opens it to the side", async (t) => {
  const page = await panelWithResults(t);
  await page.waitForFunction(() => window.__sent.some((m) => m.type === "result.select"));
  await page.keyboard.press("Enter");
  assert.deepEqual(await lastSent(page, "result.open"), { ref: "f1", where: "current" });
  await page.keyboard.press("Control+Enter");
  assert.deepEqual(await lastSent(page, "result.open"), { ref: "f1", where: "side" });
});

test("a parse result for an older seq is dropped and the newest one renders", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  await page.fill('[data-testid="query"]', "a");
  await page.fill('[data-testid="query"]', "ab");
  const [olderSeq, newestSeq] = (await sentMessages(page, "query.changed")).map((m) => m.payload.seq);
  const withError = (raw) =>
    parsedQuery(raw, null, {
      diagnostics: [{ severity: "error", code: "x", message: raw, span: { start: 0, end: 1 }, fixes: [] }],
    });
  await fromHost(page, "parse.result", { seq: olderSeq, query: withError("a"), completions: [] });
  assert.equal(await page.locator('[data-testid="diagnostics"] .diag').count(), 0, "older seq ignored");
  await fromHost(page, "parse.result", { seq: newestSeq, query: withError("ab"), completions: [] });
  assert.equal(await page.locator('[data-testid="diagnostics"] .diag').count(), 1, "newest seq rendered");
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
  // @covers mock:13
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
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [RESULT_ITEMS[1]] });
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
  const item = {
    ...RESULT_ITEMS[1],
    text: "        self.retry_policy = x",
    hits: [{ start: 13, end: 25, termIndex: 0 }],
  };
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [item] });
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
