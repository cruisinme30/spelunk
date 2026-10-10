// Builds test/out/searchPanel.html (the search panel), test/out/help.html
// (the help page) and test/out/welcome.html (the welcome page): the real webview bundles, with a fake acquireVsCodeApi in
// place of VS Code, in the page extension/src/webviewPage.ts writes for VS Code, under its content security policy, so a
// test fails on anything the policy would block in the editor (harness.mjs fails a test on any violation). Also
// test/out/queryEdit.mjs, the pure query edits as an ES module for unit tests in Node.
// Tests talk to a page through these globals:
//
//   window.__sent                every message the webview sent, in order
//   window.__fromHost(type, p)   delivers a host message to the webview
//   window.__savedState          what the webview saved with setState; a test can
//                                set it before the page loads (addInitScript) to
//                                play the state VS Code hands back on a reload
//   window.__forwardToHost       optional; when a page defines it (e.g. as a
//                                Playwright binding), every sent message is
//                                also passed to it, as to a real host
import { build } from "esbuild";
import { copyFileSync, mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, "out");
mkdirSync(out, { recursive: true });

copyFileSync(join(here, "../src/main.css"), join(out, "main.css"));

// The real page shell, bundled for Node; it imports only vscode's types.
await build({
  entryPoints: [join(here, "../../extension/src/webviewPage.ts")],
  bundle: true,
  platform: "node",
  format: "esm",
  outfile: join(out, "webviewPage.mjs"),
});
const { webviewPage } = await import(pathToFileURL(join(out, "webviewPage.mjs")).href);

/** A stand-in for VS Code's webview: resources are the files in out/, and the policy's source is file:. */
const fakeWebview = { cspSource: "file:", asWebviewUri: (uri) => uri };
/** out/ as the webview root that webviewPage joins resource names onto. */
const webviewRoot = {
  path: out,
  with: ({ path }) => ({ path, toString: () => pathToFileURL(path).href }),
};

/**
 * Bundles one webview entry into out/<name>.js, after fakeVscodeApi.mjs (the policy allows the page only its one
 * script), and its page into out/<name>.html.
 */
async function buildPage(entry, name) {
  await build({
    stdin: {
      contents: `import "./fakeVscodeApi.mjs";\nimport ${JSON.stringify(join(here, "../src", entry))};`,
      resolveDir: here,
      loader: "js",
    },
    bundle: true,
    format: "iife",
    target: "es2022",
    outfile: join(out, `${name}.js`),
  });
  writeFileSync(join(out, `${name}.html`), webviewPage(fakeWebview, webviewRoot, `${name}.js`, name));
}

await buildPage("main.ts", "searchPanel");
await buildPage("helpPage.ts", "help");
await buildPage("welcomePage.ts", "welcome");
await build({
  entryPoints: [join(here, "../src/queryEdit.ts")],
  bundle: true,
  format: "esm",
  target: "es2022",
  outfile: join(out, "queryEdit.mjs"),
});
