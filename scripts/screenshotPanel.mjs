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
//   --ask <question>    a question to ask in plain words after --query: typed, ↵, answered by the fake provider
//                       SPELUNK_ASK_FAKE names (a JSON object from each question to its query or answer), with
//                       --setting ask.provider=anthropic so Ask is on
//   --first <text>      a query to run before --query, e.g. to show that a broken query keeps the last results
//   --replace <text>    open the Replace row and preview replacing the query's matches with <text>
//   --recent <text>     a recent query (repeatable, newest first)
//   --open <path>       a file open in the editor, relative to --workspace, for is:open (repeatable)
//   --key <key>         a key to press after typing, as Playwright names it, e.g. Tab or ? (repeatable)
//   --click <selector>  an element to click after the keys, e.g. '[data-testid="repos"]' (repeatable)
//   --click-after <selector>  an element to click once --select has previewed a result, e.g.
//                       '[data-testid="outline-toggle"]' (repeatable)
//   --type <text>       text to type after the clicks, into whatever has the focus
//   --hover <selector>  an element to rest the mouse on last, until its hover label shows, e.g.
//                       '[data-testid="toggle-replace"]'
//   --help-page         render the help page instead of the search panel
//   --out <file>        where to save the PNG (default: panel.png)
//   --select <n>        press ↓ n times after the results arrive, to preview a result
//   --setting <k=v>     a spelunk.* setting, e.g. caseSensitive=smart; v is read as JSON when it parses (repeatable),
//                       so --setting 'ui.pinnedQueries=[{"query":"sym:RetryPolicy"}]' pins a query
//   --new-commit <r|a|s|p>  once indexed, commits a line added to file p in repo r of --workspace, by author a
//                       ("Name <email>") with subject s, then waits for the watches to see it (repeatable). It
//                       commits in --workspace, so point that at a copy whose repos have user.email set
//   --workspace <dir>   a folder of repos, one root per subfolder (default: testdata/workspace); each root's
//                       .vscode/settings.json files.exclude and search.exclude hide files, as in VS Code
//   --theme <name>      a color theme of the VS Code scripts/e2e.mjs downloads, e.g. "Dark Modern" or "Light Modern":
//                       the panel takes its colors, and code its syntax colors, by VS Code's own grammars
//   --width, --height   the panel size in pixels (default: 1200 × 720)
//
// Needs the daemon binary (npm test builds it) and Playwright's Chromium
// (or CHROMIUM_PATH).
import { execFileSync } from "node:child_process";
import { appendFileSync, existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parseArgs } from "node:util";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const { values: args } = parseArgs({
  options: {
    query: { type: "string", default: "" },
    ask: { type: "string" },
    recent: { type: "string", multiple: true, default: [] },
    replace: { type: "string" },
    open: { type: "string", multiple: true, default: [] },
    setting: { type: "string", multiple: true, default: [] },
    "new-commit": { type: "string", multiple: true, default: [] },
    key: { type: "string", multiple: true, default: [] },
    click: { type: "string", multiple: true, default: [] },
    "click-after": { type: "string", multiple: true, default: [] },
    type: { type: "string" },
    hover: { type: "string" },
    "help-page": { type: "boolean", default: false },
    first: { type: "string" },
    out: { type: "string", default: "panel.png" },
    select: { type: "string", default: "0" },
    workspace: { type: "string", default: join(repoRoot, "testdata/workspace") },
    theme: { type: "string" },
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
    settings: () => ({
      ...host.daemonSettings(configured, scratch),
      location: join(scratch, "index"),
      editorExcludes: host.editorExcludes(workspaceRoots(host), folderExcludes),
    }),
    watches: () => host.watchedQueries(configured),
  });
  await daemon.start();
  const browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
  try {
    await untilIndexed(daemon);
    const page = await browser.newPage({ viewport: { width: Number(args.width), height: Number(args.height) } });
    const theme = args.theme === undefined ? undefined : await loadTheme(host, args.theme);
    const posted = await connectPanel(page, host, daemon, theme);
    await page.goto(pathToFileURL(join(repoRoot, "webview/test/out/searchPanel.html")).href);
    if (theme) await applyThemeColors(page, theme);
    await postToPanel(page, "index.status", await daemon.request("index/status", {}));
    await commitForWatches(posted);
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

/** A folder's files.exclude or search.exclude, from its .vscode/settings.json, as VS Code reads it for the folder. */
function folderExcludes(folder, section) {
  const file = join(folder, ".vscode", "settings.json");
  return existsSync(file) ? JSON.parse(readFileSync(file, "utf8"))[`${section}.exclude`] : undefined;
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
async function connectPanel(page, host, daemon, theme) {
  const posted = [];
  // With --theme, code is colored as the extension colors it: after the results and previews that carry it.
  const syntax =
    theme &&
    new host.SyntaxPosts(theme.syntax, (message) => {
      posted.push("syntax.lines");
      void postToPanel(page, "syntax.lines", message);
    });
  const ui = {
    post: (type, payload) => {
      posted.push(type);
      void postToPanel(page, type, payload);
      syntax?.posted(type, payload);
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
    saveContextLines: () => {},
    saveOrder: () => {},
    // A screenshot previews a replace but never applies one.
    readDocument: async () => {},
    applyEdits: async () => false,
    saveFiles: () => {},
    // A watch's pop-up is VS Code's own; the panel shows its count.
    showWatchMatch: async () => {},
    showQuery: () => {},
    showError: (message) => console.error(message),
  };
  const openFiles = args.open.map((path) => join(args.workspace, path));
  // The extension's settings, minus the typing delay, so each keystroke searches at once.
  const uiSettings = { ...host.uiSettings(configured), typingDelayMs: 0 };
  // --ask's provider: the fake SPELUNK_ASK_FAKE describes, so a screenshot never calls a real one.
  const fakeProvider = host.fakeProviderFromEnvironment();
  const ask = fakeProvider && {
    setup: async () => ({ kind: "ready", provider: fakeProvider }),
    shareNames: () => true,
    setKey: async () => {},
  };
  const controller = new host.SearchController(
    daemon,
    ui,
    {
      ...(ask ? { ask } : {}),
      recentLimit: () => 20,
      pinnedQueries: () => host.pinnedQueries(configured),
      closeOnOpen: () => false,
      previewContextLines: () => host.previewContextLines(configured),
      uiSettings: () => uiSettings,
      openFiles: () => openFiles,
      watchNotify: () => true,
    },
    { text: "", recent: args.recent },
  );
  daemon.on("progress", (progress) => ui.post("index.status", progress));
  // The test page passes every message it sends to window.__forwardToHost when that exists.
  await page.exposeBinding("__forwardToHost", (_source, message) => controller.handle(message));
  return posted;
}

/**
 * Makes each --new-commit, then waits until the watches have counted them
 * (the controller posts watch.state once per announcement it records).
 */
async function commitForWatches(posted) {
  if (args["new-commit"].length === 0) return;
  await until("the panel's first watch.state", () => posted.includes("watch.state"));
  const counted = posted.filter((type) => type === "watch.state").length;
  for (const spec of args["new-commit"]) {
    const [repo, author, subject, path] = spec.split("|");
    const root = join(args.workspace, repo);
    appendFileSync(join(root, path), `# ${subject}\n`);
    execFileSync("git", ["-C", root, "add", "--all"]);
    execFileSync("git", ["-C", root, "commit", "--quiet", `--author=${author}`, "-m", subject]);
  }
  // The daemon looks for new commits every 5 seconds.
  await until(
    "the watches to count the new commits",
    () => posted.filter((type) => type === "watch.state").length > counted,
    20_000,
  );
}

/** Types --first and --query, previews --replace, presses --key, clicks --click, types --type and moves down --select rows. */
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
  if (args.ask !== undefined) {
    posted.length = 0;
    await page.fill('[data-testid="query"]', args.ask);
    await page.keyboard.press("Enter");
    await until("the question's answer", () => posted.includes("ask.result"));
    posted.length = 0;
    await until("the answer's results", () => posted.includes("search.done") || posted.includes("parse.result"));
    await page.waitForTimeout(150);
  }
  if (args.replace !== undefined) {
    await page.click('[data-testid="toggle-replace"]');
    await page.fill('[data-testid="replacement"]', args.replace);
    await until("the replace preview", () => posted.at(-1) === "replace.plan");
  }
  for (const key of args.key) await page.keyboard.press(key);
  for (const selector of args.click) await page.click(selector);
  if (args.type) await page.keyboard.type(args.type);
  const rowsDown = Number(args.select);
  for (let row = 0; row < rowsDown; row++) await page.keyboard.press("ArrowDown");
  if (rowsDown > 0) await until("preview.result", () => posted.includes("preview.result"));
  for (const selector of args["click-after"]) await page.click(selector);
  if (args.theme) await page.waitForTimeout(500); // the syntax colors follow the results and preview
  await page.waitForTimeout(100); // let the last render settle
  if (args.hover) {
    await page.hover(args.hover);
    // A second at most: what someone resting the mouse there would see by then.
    await page.waitForSelector('[data-testid="hover-label"]:not([hidden])', { timeout: 1000 }).catch(() => {});
  }
}

/**
 * --theme, from the VS Code scripts/e2e.mjs downloads into .vscode-test/:
 * its uiTheme, its workbench colors, and syntax colors by VS Code's own grammars.
 */
async function loadTheme(host, name) {
  const extensions = builtInExtensions();
  const found = host.themeFile(extensions, name);
  if (!found)
    throw new Error(`No color theme ${name} in .vscode-test/; run node scripts/e2e.mjs once to download VS Code`);
  const { rules, colors } = await host.readColorTheme(found.file, name, {}); // no customizations
  const engine = join(
    dirname(createRequire(join(repoRoot, "extension/package.json")).resolve("vscode-oniguruma")),
    "onig.wasm",
  );
  const syntax = new host.SyntaxColors(new host.GrammarCatalog(extensions), rules, () => readFile(engine));
  return { uiTheme: found.uiTheme, colors, syntax };
}

/** The extensions built into the downloaded VS Code: its themes and language grammars. */
function builtInExtensions() {
  const folder = extensionsFolder(join(repoRoot, ".vscode-test"), 0);
  if (!folder) throw new Error("No VS Code in .vscode-test/; run node scripts/e2e.mjs once to download it");
  return readdirSync(folder)
    .map((name) => join(folder, name))
    .filter((path) => existsSync(join(path, "package.json")))
    .map((path) => ({
      extensionPath: path,
      packageJSON: JSON.parse(readFileSync(join(path, "package.json"), "utf8")),
    }));
}

/** The folder of built-in extensions under `folder`: the one holding theme-defaults. */
function extensionsFolder(folder, depth) {
  if (existsSync(join(folder, "theme-defaults", "package.json"))) return folder;
  if (depth > 6 || !existsSync(folder) || !statSync(folder).isDirectory()) return;
  for (const name of readdirSync(folder)) {
    const found = extensionsFolder(join(folder, name), depth + 1);
    if (found) return found;
  }
}

/** Gives the page the theme's colors, as VS Code gives a webview: a body class for its kind, and --vscode-* properties. */
function applyThemeColors(page, { uiTheme, colors }) {
  const bodyClass = {
    vs: "vscode-light",
    "vs-dark": "vscode-dark",
    "hc-black": "vscode-high-contrast",
    "hc-light": "vscode-high-contrast vscode-high-contrast-light",
  }[uiTheme];
  return page.evaluate(
    ([className, themeColors]) => {
      const { body, documentElement } = globalThis.document;
      body.className = className;
      for (const [key, value] of Object.entries(themeColors)) {
        documentElement.style.setProperty(`--vscode-${key.replaceAll(".", "-")}`, value);
      }
    },
    [bodyClass ?? "vscode-dark", colors],
  );
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
        'export { fakeProviderFromEnvironment } from "./ask";',
        'export { Daemon } from "./daemon";',
        'export { makeRoot } from "./roots";',
        'export { editorExcludes } from "./editorExcludes";',
        'export { daemonSettings, pinnedQueries, previewContextLines, uiSettings, watchedQueries } from "./settings";',
        'export { readColorTheme, themeFile } from "./colorTheme";',
        'export { GrammarCatalog, SyntaxColors } from "./syntaxColors";',
        'export { SyntaxPosts } from "./syntaxLines";',
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
