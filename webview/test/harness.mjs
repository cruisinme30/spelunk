// Shared helpers for the webview tests: a Chromium per test file, pages of
// the real bundles (built by buildHarness.mjs), and recorded host messages.
import { dirname, join } from "node:path";
import { after, before } from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const out = join(dirname(fileURLToPath(import.meta.url)), "out");
export const UI_SETTINGS = {
  typingDelayMs: 0,
  openTrigger: "doubleClick",
  preview: true,
  showParsedQuery: true,
  caseSensitive: false,
};

let browser;
/** Starts Chromium before the calling file's tests and closes it after them. */
export function useBrowser() {
  before(async () => {
    browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
  });
  after(async () => browser?.close());
}

/** Opens a fresh search panel, closed again when the test ends (even if it fails). */
export const openPanel = (t) => openPage(t, "harness.html");

/** Opens the help page. */
export const openHelp = (t) => openPage(t, "help.html");

async function openPage(t, file) {
  const page = await browser.newPage();
  t.after(() => page.close());
  await page.goto(pathToFileURL(join(out, file)).href);
  return page;
}

export const sentMessages = (page, type) => page.evaluate((t) => window.__sent.filter((m) => m.type === t), type);
export const lastSent = async (page, type) => (await sentMessages(page, type)).at(-1)?.payload;
export const fromHost = (page, type, payload) => page.evaluate(([t, p]) => window.__host(t, p), [type, payload]);
export const restore = (page, recent = []) =>
  fromHost(page, "state.restore", { text: "", recent, settings: UI_SETTINGS });

export const parsedQuery = (raw, root, overrides = {}) => ({
  version: 1,
  raw,
  root,
  mode: "workingTree",
  diagnostics: [],
  globals: { case: null, count: null, type: null },
  ...overrides,
});
export const textNode = (value, start, termIndex = 0) => ({
  kind: "text",
  value,
  match: "literal",
  termIndex,
  span: { start, end: start + value.length },
});

/** Types a query and answers its parse with `query`; returns the seq. */
export async function typeAndParse(page, text, query) {
  await page.fill('[data-testid="query"]', text);
  const { seq } = await lastSent(page, "query.changed");
  await fromHost(page, "parse.result", { seq, query, completions: [] });
  return seq;
}

export const RESULT_ITEMS = [
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
export async function panelWithResults(t) {
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
