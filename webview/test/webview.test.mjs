// Contract 2, webview side: drive the real panel with recorded host messages.
// @covers msg:ready msg:state.restore msg:query.changed msg:parse.result msg:search.batch msg:search.done msg:result.select msg:preview.result msg:index.status msg:banner msg:daemon.restart msg:panel.close msg:focus
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const url = pathToFileURL(join(here, "out/harness.html")).href;
let browser;
before(async () => {
  browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
});
after(async () => browser?.close());

async function open() {
  const page = await browser.newPage();
  await page.goto(url);
  return page;
}
const sent = (page, type) => page.evaluate((t) => window.__sent.filter((m) => m.type === t), type);
const host = (page, type, payload) => page.evaluate(([t, p]) => window.__host(t, p), [type, payload]);
const ui = { typingDelayMs: 0, openTrigger: "doubleClick", preview: true, showParsedQuery: true, caseSensitive: false };

const q = (raw, root, extra = {}) => ({
  version: 1, raw, root, mode: "workingTree", diagnostics: [],
  globals: { case: null, count: null, type: null }, ...extra,
});
const text = (value, start, termIndex = 0) => ({ kind: "text", value, match: "literal", termIndex, span: { start, end: start + value.length } });

test("announces ready and renders recent queries on an empty box", async () => {
  const page = await open();
  assert.equal((await sent(page, "ready")).length, 1);
  await host(page, "state.restore", { text: "", recent: ["sym:RetryPolicy", "since:2w timeout"], settings: ui });
  assert.equal(await page.locator('[data-testid="recent"]').count(), 2);
  assert.equal(await page.locator('[data-testid="sheet-op"]').count(), 16);
  await page.close();
});

test("typing sends query.changed; parse and results render; selection previews", async () => {
  const page = await open();
  await host(page, "state.restore", { text: "", recent: [], settings: ui });
  await page.fill('[data-testid="query"]', "retry_policy");
  const qc = await sent(page, "query.changed");
  const last = qc.at(-1).payload;
  assert.equal(last.text, "retry_policy");
  await host(page, "parse.result", { seq: last.seq, query: q("retry_policy", text("retry_policy", 0)), completions: [] });
  await host(page, "index.status", { repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }] });
  await host(page, "search.batch", { seq: last.seq, searchId: "s1", items: [
    { kind: "file", ref: "f1", repoId: "r1", path: "src/payments/retry_policy.py", nameHits: [{ start: 13, end: 25 }], dirty: false },
    { kind: "line", ref: "l1", repoId: "r1", path: "src/payments/client.py", line: 42, text: "self.retry_policy = RetryPolicy()", hits: [{ start: 5, end: 17, termIndex: 0 }] },
  ] });
  await host(page, "search.done", { seq: last.seq, searchId: "s1", total: 2, truncated: false, hidden: [], ms: 3 });
  assert.equal(await page.locator('[data-testid="result"]').count(), 2);
  assert.match(await page.locator('[data-testid="summary"]').innerText(), /1 file name · 1 code match/);
  await page.waitForFunction(() => window.__sent.some((m) => m.type === "result.select"));
  const sel = (await sent(page, "result.select")).at(-1).payload;
  assert.equal(sel.ref, "f1");
  await page.keyboard.press("ArrowDown");
  await page.waitForFunction(() => window.__sent.filter((m) => m.type === "result.select").at(-1).payload.ref === "l1");
  await host(page, "preview.result", { ref: "l1", preview: { kind: "file", path: "src/payments/client.py", firstLine: 41, lines: ["x", "self.retry_policy = RetryPolicy()"], focusLine: 42, hits: [{ line: 42, ranges: [{ start: 5, end: 17, termIndex: 0 }] }], dirtyLines: [] } });
  assert.equal(await page.locator('#preview mark').count(), 1);
  await page.keyboard.press("Enter");
  assert.deepEqual((await sent(page, "result.open")).at(-1).payload, { ref: "l1", where: "current" });
  await page.close();
});

test("stale responses with an older seq are dropped", async () => {
  const page = await open();
  await host(page, "state.restore", { text: "", recent: [], settings: ui });
  await page.fill('[data-testid="query"]', "a");
  await page.fill('[data-testid="query"]', "ab");
  const seqs = (await sent(page, "query.changed")).map((m) => m.payload.seq);
  await host(page, "parse.result", { seq: seqs[0], query: q("a", text("a", 0), { diagnostics: [{ severity: "error", code: "x", message: "old", span: { start: 0, end: 1 }, fixes: [] }] }), completions: [] });
  assert.equal(await page.locator('[data-testid="diagnostics"] .diag').count(), 0);
  await page.close();
});

test("fix-its and ⌘. are text edits followed by query.changed", async () => {
  const page = await open();
  await host(page, "state.restore", { text: "", recent: [], settings: ui });
  await page.fill('[data-testid="query"]', "sinse:6m");
  const seq = (await sent(page, "query.changed")).at(-1).payload.seq;
  await host(page, "parse.result", { seq, completions: [], query: q("sinse:6m", null, { diagnostics: [
    { severity: "error", code: "unknown_operator", message: "Unknown operator sinse:", span: { start: 0, end: 6 }, fixes: [{ title: "Change to since:", edits: [{ span: { start: 0, end: 6 }, newText: "since:" }] }] },
  ] }) });
  assert.equal(await page.locator('[data-code="unknown_operator"]').count(), 1);
  await page.keyboard.press("Control+.");
  assert.equal(await page.inputValue('[data-testid="query"]'), "since:6m");
  assert.equal((await sent(page, "query.changed")).at(-1).payload.text, "since:6m");
  await page.close();
});

test("Aa toggle edits the query text", async () => {
  const page = await open();
  await host(page, "state.restore", { text: "", recent: [], settings: ui });
  await page.fill('[data-testid="query"]', "case:yes /Retry/");
  const seq = (await sent(page, "query.changed")).at(-1).payload.seq;
  const root = { kind: "and", span: { start: 0, end: 16 }, children: [
    { kind: "op", op: "case", value: "yes", match: "literal", span: { start: 0, end: 8 } },
    { kind: "text", value: "Retry", match: "regex", termIndex: 0, span: { start: 9, end: 16 } },
  ] };
  await host(page, "parse.result", { seq, completions: [], query: q("case:yes /Retry/", root, { globals: { case: "yes", count: null, type: null } }) });
  assert.equal(await page.getAttribute('[data-testid="toggle-case"]', "aria-pressed"), "true");
  assert.equal(await page.getAttribute('[data-testid="toggle-regex"]', "aria-pressed"), "true");
  await page.click('[data-testid="toggle-case"]');
  assert.equal(await page.inputValue('[data-testid="query"]'), "/Retry/");
  await page.close();
});

test("stopped banner offers Restart", async () => {
  const page = await open();
  await host(page, "banner", { state: "stopped", message: "Search stopped" });
  await page.click('[data-testid="restart"]');
  assert.equal((await sent(page, "daemon.restart")).length, 1);
  await host(page, "banner", { state: "ok" });
  assert.equal(await page.locator('[data-testid="restart"]').count(), 0);
  await page.close();
});
