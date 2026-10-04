#!/usr/bin/env node
// Renders the search panel for a query, end to end, and saves a PNG: the
// real daemon indexes a workspace, the real host controller relays every
// message, and the real webview bundle draws the result in Chromium. Only
// VS Code itself is missing. Use it to compare the panel with the mocks.
//
// Usage:
//   node scripts/screenshotPanel.mjs --query 'retry_policy' --out panel.png
//   node scripts/screenshotPanel.mjs --recent 'since:2w timeout' --key '?' --out sheet.png
//
// Options:
//   --query <text>      what to type in the search box (default: leave it empty)
//   --first <text>      a query to run before --query, e.g. to show that a broken query keeps the last results
//   --recent <text>     a recent query (repeatable, newest first)
//   --key <key>         a key to press after typing, as Playwright names it, e.g. Tab or ? (repeatable)
//   --click <selector>  an element to click after the keys, e.g. '[data-testid="repos"]' (repeatable)
//   --help-page         render the help page instead of the search panel
//   --out <file>        where to save the PNG (default: panel.png)
//   --select <n>        press ↓ n times after the results arrive, to preview a result
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
try {
  await main();
} finally {
  rmSync(scratch, { recursive: true, force: true });
}

async function main() {
  await import(pathToFileURL(join(repoRoot, "webview/test/buildHarness.mjs")).href); // writes test/out/*.html
  if (args["help-page"]) return screenshotHelpPage();
  const host = await loadHost();

  const roots = readdirSync(args.workspace)
    .map((name) => join(args.workspace, name))
    .filter((path) => statSync(path).isDirectory())
    .map((path) => host.makeRoot(path));
  const daemon = new host.Daemon({
    binary: join(repoRoot, "daemon/bin/unified-search-daemon"),
    roots: () => roots,
    settings: () => ({ ...host.DEFAULTS, exclude: [...host.DEFAULTS.exclude], location: join(scratch, "index") }),
  });
  await daemon.start();
  const browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
  try {
    await untilIndexed(daemon);
    const page = await browser.newPage({ viewport: { width: Number(args.width), height: Number(args.height) } });
    const posted = [];
    const ui = {
      post: (type, payload) => {
        posted.push(type);
        void page.evaluate(([t, p]) => window.__host(t, p), [type, payload]);
      },
      openTarget: async () => undefined,
      hidePanel: () => undefined,
      openHelp: () => undefined,
      openSettings: () => undefined,
      restartDaemon: () => undefined,
      setContext: () => undefined,
      saveState: () => undefined,
    };
    const uiSettings = {
      typingDelayMs: 0,
      openTrigger: "doubleClick",
      preview: true,
      showParsedQuery: true,
      caseSensitive: false,
    };
    const controller = new host.SearchController(
      daemon,
      ui,
      { recentLimit: () => 20, closeOnOpen: () => false, uiSettings: () => uiSettings },
      { text: "", recent: args.recent },
    );
    daemon.on("progress", (progress) => ui.post("index.status", progress));

    // Every message the webview sends goes to the controller, as in extension.ts.
    await page.exposeBinding("__toHost", (_source, message) => controller.handle(message));
    await page.addInitScript(() => {
      let sent;
      Object.defineProperty(window, "__sent", {
        get: () => sent,
        set: (list) => {
          list.push = (message) => {
            Array.prototype.push.call(list, message);
            window.__toHost(message);
            return list.length;
          };
          sent = list;
        },
      });
    });
    await page.goto(pathToFileURL(join(repoRoot, "webview/test/out/harness.html")).href);
    ui.post("index.status", { repos: await daemon.request("index/status", {}).then((status) => status.repos) });

    if (args.first) {
      await page.fill('[data-testid="query"]', args.first);
      await until("the first query's results", () => posted.includes("search.done"));
      posted.length = 0;
    }
    if (args.query) {
      await page.fill('[data-testid="query"]', args.query);
      await until(
        "search.done or parse errors",
        () => posted.includes("search.done") || posted.includes("parse.result"),
      );
      await page.waitForTimeout(150); // a query with errors gets no search.done
    }
    for (const key of args.key) await page.keyboard.press(key);
    for (const selector of args.click) await page.click(selector);
    for (let i = 0; i < Number(args.select); i++) await page.keyboard.press("ArrowDown");
    if (Number(args.select) > 0) await until("preview.result", () => posted.includes("preview.result"));
    await page.waitForTimeout(100);
    await page.screenshot({ path: args.out });
    console.log(`saved ${args.out}`);
  } finally {
    await browser.close();
    await daemon.stop();
  }
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
        'export { DEFAULTS } from "./settings";',
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

async function untilIndexed(daemon) {
  await until("every root indexed", async () => {
    const { repos } = await daemon.request("index/status", {});
    return repos.every((repo) => repo.tree === "ready");
  });
}

async function until(description, condition, timeoutMs = 10_000) {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${description}`);
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
}
