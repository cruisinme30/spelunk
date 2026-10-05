#!/usr/bin/env node
// Renders the search panel for a query, end to end, and saves a PNG: the
// real daemon indexes a workspace, the real host controller relays every
// message, and the real webview bundle draws the result in Chromium. Only
// VS Code itself is missing. Use it to see how the panel looks for a query
// without starting VS Code.
//
// Usage:
//   node scripts/screenshotPanel.mjs --query 'retry_policy' --out panel.png
//   node scripts/screenshotPanel.mjs --recent 'since:2w timeout' --key '?' --out sheet.png
//
// Options:
//   --query <text>      what to type in the search box (default: leave it empty)
//   --first <text>      a query to run before --query, e.g. to show that a broken query keeps the last results
//   --recent <text>     a recent query (repeatable, newest first)
//   --open <path>       a file open in the editor, relative to --workspace, for is:open (repeatable)
//   --key <key>         a key to press after typing, as Playwright names it, e.g. Tab or ? (repeatable)
//   --click <selector>  an element to click after the keys, e.g. '[data-testid="repos"]' (repeatable)
//   --help-page         render the help page instead of the search panel
//   --out <file>        where to save the PNG (default: panel.png)
//   --select <n>        press ↓ n times after the results arrive, to preview a result
//   --setting <k=v>     a spelunk.* setting, e.g. caseSensitive=smart; v is read as JSON when it parses (repeatable),
//                       so --setting 'ui.pinnedQueries=[{"query":"sym:RetryPolicy"}]' pins a query
//   --workspace <dir>   a folder of repos, one root per subfolder (default: testdata/workspace)
//   --width, --height   the panel size in pixels (default: 1200 × 720)
//
// Needs the daemon binary (npm test builds it) and Playwright's Chromium
// (or CHROMIUM_PATH).
import { mkdtempSync, readdirSync, rmSync, statSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parseArgs } from "node:util";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const { values: args } = parseArgs({
  options: {
    query: { type: "string", default: "" },
    recent: { type: "string", multiple: true, default: [] },
    open: { type: "string", multiple: true, default: [] },
    setting: { type: "string", multiple: true, default: [] },
    key: { type: "string", multiple: true, default: [] },
    click: { type: "string", multiple: true, default: [] },
    "help-page": { type: "boolean", default: false },
    first: { type: "string" },
    out: { type: "string", default: "panel.png" },
    select: { type: "string", default: "0" },
    workspace: { type: "string", default: join(repoRoot, "testdata/workspace") },
    width: { type: "string", default: "1200" },
    height: { type: "string", default: "720" },
  },
});

// Tools resolve from the workspace that depends on them.
const { build } = createRequire(join(repoRoot, "extension/package.json"))("esbuild");
const { chromium } = createRequire(join(repoRoot, "webview/package.json"))("playwright");

const scratch = mkdtempSync(join(tmpdir(), "us-screenshot-"));
/** A settings reader that has only the --setting values set; every other setting is its default. */
const configured = { get: (key, defaultValue) => (settingValues.has(key) ? settingValues.get(key) : defaultValue) };
const settingValues = new Map(args.setting.map((pair) => parseSetting(pair)));

/** "caseSensitive=smart" → ["caseSensitive", "smart"]; "defaultCount=5" → ["defaultCount", 5]. */
function parseSetting(pair) {
  const [key, value] = [pair.slice(0, pair.indexOf("=")), pair.slice(pair.indexOf("=") + 1)];
  try {
    return [key, JSON.parse(value)];
  } catch {
    return [key, value];
  }
}
try {
  await main();
} finally {
  rmSync(scratch, { recursive: true, force: true });
}

async function main() {
  await import(pathToFileURL(join(repoRoot, "webview/test/buildHarness.mjs")).href); // writes webview/test/out/*.html
  if (args["help-page"]) return screenshotHelpPage();
  const host = await loadHost();
  const daemon = new host.Daemon({
    binary: join(repoRoot, "daemon/bin/spelunk-daemon"),
    roots: () => workspaceRoots(host),
    settings: () => ({ ...host.daemonSettings(configured, scratch), location: join(scratch, "index") }),
  });
  await daemon.start();
  const browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
  try {
    await untilIndexed(daemon);
    const page = await browser.newPage({ viewport: { width: Number(args.width), height: Number(args.height) } });
    const posted = await connectPanel(page, host, daemon);
    await page.goto(pathToFileURL(join(repoRoot, "webview/test/out/searchPanel.html")).href);
    await postToPanel(page, "index.status", await daemon.request("index/status", {}));
    await drivePanel(page, posted);
    await page.screenshot({ path: args.out });
    console.log(`saved ${args.out}`);
  } finally {
    await browser.close();
    await daemon.stop();
  }
}

/** One root per subfolder of --workspace. */
function workspaceRoots(host) {
  return readdirSync(args.workspace)
    .map((name) => join(args.workspace, name))
    .filter((path) => statSync(path).isDirectory())
    .map((path) => host.makeRoot(path));
}

/** Plays a host message into the page, as the extension's panel would post it. */
function postToPanel(page, type, payload) {
  return page.evaluate(
    ([messageType, messagePayload]) => globalThis.__fromHost(messageType, messagePayload),
    [type, payload],
  );
}

/**
 * Wires the page to a real SearchController, as extension.ts does: what the
 * controller posts goes to the page, and what the page sends goes to the
 * controller. Returns the list of message types posted so far, which the
 * caller waits on.
 */
async function connectPanel(page, host, daemon) {
  const posted = [];
  const ui = {
    post: (type, payload) => {
      posted.push(type);
      void postToPanel(page, type, payload);
    },
    openTarget: async () => {},
    hidePanel: () => {},
    openHelp: () => {},
    openSettings: () => {},
    restartDaemon: () => {},
    showOpened: () => {},
    setContext: () => {},
    saveState: () => {},
    savePinned: () => {},
  };
  const openFiles = args.open.map((path) => join(args.workspace, path));
  // The extension's settings, minus the typing delay, so each keystroke searches at once.
  const uiSettings = { ...host.uiSettings(configured), typingDelayMs: 0 };
  const controller = new host.SearchController(
    daemon,
    ui,
    {
      recentLimit: () => 20,
      pinnedQueries: () => host.pinnedQueries(configured),
      closeOnOpen: () => false,
      uiSettings: () => uiSettings,
      openFiles: () => openFiles,
    },
    { text: "", recent: args.recent },
  );
  daemon.on("progress", (progress) => ui.post("index.status", progress));
  // The test page passes every message it sends to window.__forwardToHost when that exists.
  await page.exposeBinding("__forwardToHost", (_source, message) => controller.handle(message));
  return posted;
}

/** Types --first and --query, presses --key, clicks --click and moves down --select rows. */
async function drivePanel(page, posted) {
  if (args.first) {
    await page.fill('[data-testid="query"]', args.first);
    await until("the first query's results", () => posted.includes("search.done"));
    posted.length = 0;
  }
  if (args.query) {
    await page.fill('[data-testid="query"]', args.query);
    await until("search.done or parse errors", () => posted.includes("search.done") || posted.includes("parse.result"));
    await page.waitForTimeout(150); // a query with errors gets no search.done
  }
  for (const key of args.key) await page.keyboard.press(key);
  for (const selector of args.click) await page.click(selector);
  const rowsDown = Number(args.select);
  for (let row = 0; row < rowsDown; row++) await page.keyboard.press("ArrowDown");
  if (rowsDown > 0) await until("preview.result", () => posted.includes("preview.result"));
  await page.waitForTimeout(100); // let the last render settle
}

/** The help page needs no daemon: it is static until Try is clicked. */
async function screenshotHelpPage() {
  const browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
  try {
    const page = await browser.newPage({ viewport: { width: Number(args.width), height: Number(args.height) } });
    await page.goto(pathToFileURL(join(repoRoot, "webview/test/out/help.html")).href);
    for (const selector of args.click) await page.click(selector);
    await page.screenshot({ path: args.out });
    console.log(`saved ${args.out}`);
  } finally {
    await browser.close();
  }
}

/** Bundles the host's daemon client and controller, which have no VS Code imports. */
async function loadHost() {
  const outfile = join(scratch, "host.mjs");
  await build({
    stdin: {
      contents: [
        'export { SearchController } from "./controller";',
        'export { Daemon } from "./daemon";',
        'export { makeRoot } from "./roots";',
        'export { daemonSettings, pinnedQueries, uiSettings } from "./settings";',
      ].join("\n"),
      resolveDir: join(repoRoot, "extension/src"),
      loader: "ts",
    },
    bundle: true,
    platform: "node",
    format: "esm",
    outfile,
    logLevel: "warning",
  });
  return import(pathToFileURL(outfile).href);
}

/** Whether an index has nothing left to read: it's ready, or off (a folder outside Git has no history). */
function finished(state) {
  return state === "ready" || state === "off";
}

/** Waits until the daemon has indexed every root's files and history, so the results are complete. */
async function untilIndexed(daemon) {
  await until("every root indexed", async () => {
    const { repos } = await daemon.request("index/status", {});
    return repos.every((repo) => repo.tree === "ready" && finished(repo.history));
  });
}

/** Polls `condition` until it holds, failing after `timeoutMs`. */
async function until(description, condition, timeoutMs = 10_000) {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${description}`);
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
}
