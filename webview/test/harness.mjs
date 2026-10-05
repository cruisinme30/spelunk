// Shared helpers for the webview tests: a Chromium per test file, pages of
// the real bundles (built by buildHarness.mjs), host messages to play into a
// page, and the messages it sent back.
import { dirname, join } from "node:path";
import { after, before } from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const out = join(dirname(fileURLToPath(import.meta.url)), "out");

/** Settings for a test panel: no typing delay, so every keystroke is sent at once. */
export const UI_SETTINGS = {
  typingDelayMs: 0,
  openTrigger: "doubleClick",
  preview: true,
  showParsedQuery: true,
  caseSensitive: false,
  wholeWord: false,
  order: "best",
};

/** The query box. */
export const QUERY = '[data-testid="query"]';

let browser;
/** Starts Chromium before the calling file's tests and closes it after them. */
export function useBrowser() {
  before(async () => {
    browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
  });
  after(async () => browser?.close());
}

/** Opens a fresh search panel, closed again when the test ends (even if it fails). */
export const openPanel = (testContext) => openPage(testContext, "searchPanel.html");

/** Opens a search panel that VS Code hands `savedState` on load, as after a webview reload. */
export const openPanelWithSavedState = (testContext, savedState) =>
  openPage(testContext, "searchPanel.html", savedState);

/** Opens the help page. */
export const openHelp = (testContext) => openPage(testContext, "help.html");

/** Opens the welcome page. */
export const openWelcome = (testContext) => openPage(testContext, "welcome.html");

/** Uncaught exceptions and console errors of each open page, in order. */
const errorsByPage = new WeakMap();

async function openPage(testContext, file, savedState) {
  const page = await browser.newPage();
  testContext.after(() => page.close());
  const errors = [];
  errorsByPage.set(page, errors);
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  if (savedState !== undefined) {
    await page.addInitScript((state) => {
      globalThis.__savedState = state;
    }, savedState);
  }
  await page.goto(pathToFileURL(join(out, file)).href);
  return page;
}

/** Every uncaught exception and console error the page has had so far. */
export const pageErrors = (page) => errorsByPage.get(page) ?? [];

/** Every message of `type` the page has sent so far, oldest first. */
export const sentMessages = (page, type) =>
  page.evaluate((wantedType) => globalThis.__sent.filter((message) => message.type === wantedType), type);

/** The payload of the newest message of `type` the page sent, or undefined. */
export const lastSent = async (page, type) => (await sentMessages(page, type)).at(-1)?.payload;

/**
 * Waits until the newest `type` message the page sent has all of `fields`
 * (compared as JSON), and returns its payload. For messages the page sends
 * after a delay, such as result.select.
 */
export async function waitForSent(page, type, fields = {}) {
  await page.waitForFunction(
    ([wantedType, wantedFields]) => {
      const payload = globalThis.__sent.findLast((message) => message.type === wantedType)?.payload;
      return (
        payload !== undefined &&
        Object.entries(wantedFields).every(([key, value]) => JSON.stringify(payload[key]) === JSON.stringify(value))
      );
    },
    [type, fields],
  );
  return lastSent(page, type);
}

/** Plays a message from the host into the page. */
export const fromHost = (page, type, payload) =>
  page.evaluate(([messageType, messagePayload]) => globalThis.__fromHost(messageType, messagePayload), [type, payload]);

/** The host's first message to a new panel: an empty box, `recent` queries and UI_SETTINGS with `settings` over them. */
export const restore = (page, recent = [], settings = {}) =>
  fromHost(page, "state.restore", { text: "", recent, settings: { ...UI_SETTINGS, ...settings } });

/** The end of search "s1" for `seq`: nothing found, hidden or truncated, unless `fields` says otherwise. */
export const searchDone = (page, seq, fields = {}) =>
  fromHost(page, "search.done", { seq, searchId: "s1", total: 0, truncated: false, hidden: [], ms: 1, ...fields });

/** A ParsedQuery as the daemon would send it; `overrides` replaces any field. */
export const parsedQuery = (raw, root, overrides = {}) => ({
  version: 1,
  raw,
  root,
  mode: "workingTree",
  diagnostics: [],
  globals: { case: null, count: null, type: null },
  ...overrides,
});

/** "sinse:6m" with an unknown-operator error whose fix rewrites it to since:. */
export const MISSPELT = parsedQuery("sinse:6m", null, {
  diagnostics: [
    {
      severity: "error",
      code: "unknown_operator",
      message: "Unknown operator sinse:",
      span: { start: 0, end: 6 },
      fixes: [{ title: "Change to since:", edits: [{ span: { start: 0, end: 6 }, newText: "since:" }] }],
    },
  ],
});

/** A literal text term starting at `start`. */
export const textNode = (value, start, termIndex = 0) => ({
  kind: "text",
  value,
  match: "literal",
  termIndex,
  span: { start, end: start + value.length },
});

/** Types a query and answers its parse with `query`; returns the seq. */
export async function typeAndParse(page, text, query) {
  await page.fill(QUERY, text);
  const { seq } = await lastSent(page, "query.changed");
  await fromHost(page, "parse.result", { seq, query, completions: [] });
  return seq;
}

/** A file-name result for "retry_policy". */
export const FILE_NAME_RESULT = {
  kind: "file",
  ref: "f1",
  repoId: "r1",
  path: "src/payments/retry_policy.py",
  nameHits: [{ start: 13, end: 25 }],
  dirty: false,
};

/** A code-line result for "retry_policy". */
export const CODE_LINE_RESULT = {
  kind: "line",
  ref: "l1",
  repoId: "r1",
  path: "src/payments/client.py",
  line: 42,
  text: "self.retry_policy = RetryPolicy()",
  hits: [{ start: 5, end: 17, termIndex: 0 }],
};

/** A panel showing FILE_NAME_RESULT and CODE_LINE_RESULT for "retry_policy", as search "s1". */
export async function panelWithResults(testContext) {
  const page = await openPanel(testContext);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  const seq = await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [FILE_NAME_RESULT, CODE_LINE_RESULT] });
  await searchDone(page, seq, { total: 2, ms: 3 });
  return page;
}
