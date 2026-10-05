// Builds test/out/searchPanel.html (the search panel), test/out/help.html
// (the help page) and test/out/welcome.html (the welcome page): the real webview bundles, with a fake acquireVsCodeApi in
// place of VS Code. Also test/out/queryEdit.mjs, the pure query edits as an ES module for unit tests in Node.
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
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, "out");
mkdirSync(out, { recursive: true });

const FAKE_VSCODE_API = `
window.__sent = [];
if (!("__savedState" in window)) window.__savedState = undefined;
window.acquireVsCodeApi = () => ({
  postMessage: (message) => {
    const copy = JSON.parse(JSON.stringify(message));
    window.__sent.push(copy);
    window.__forwardToHost?.(copy);
  },
  getState: () => window.__savedState,
  setState: (state) => { window.__savedState = state; },
});
window.__fromHost = (type, payload) =>
  window.dispatchEvent(new MessageEvent("message", { data: { v: 1, type, payload } }));
`;

const css = readFileSync(join(here, "../src/main.css"), "utf8");

/** Bundles one webview entry into out/<name>.html with the fake VS Code API. */
async function buildPage(entry, name) {
  const result = await build({
    entryPoints: [join(here, "../src", entry)],
    bundle: true,
    format: "iife",
    target: "es2022",
    write: false,
  });
  const script = result.outputFiles[0].text;
  writeFileSync(
    join(out, `${name}.html`),
    `<!doctype html>
<html><head><meta charset="utf-8"><style>${css}</style></head>
<body class="vscode-dark"><div id="app"></div>
<script>${FAKE_VSCODE_API}</script>
<script>${script.replaceAll("</script>", String.raw`<\/script>`)}</script>
</body></html>`,
  );
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
