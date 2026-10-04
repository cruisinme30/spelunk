// Builds test/out/harness.html: the real webview bundle with a fake
// acquireVsCodeApi that records outbound messages and lets tests play
// recorded host messages (Contract 2 contract tests, test plan L2).
import { build } from "esbuild";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, "out");
mkdirSync(out, { recursive: true });
const res = await build({ entryPoints: [join(here, "../src/main.ts")], bundle: true, format: "iife", target: "es2020", write: false });
const css = readFileSync(join(here, "../src/main.css"), "utf8");
const js = res.outputFiles[0].text;
writeFileSync(join(out, "harness.html"), `<!doctype html>
<html><head><meta charset="utf-8"><style>${css}</style></head>
<body class="vscode-dark"><div id="app"></div>
<script>
window.__sent = [];
window.__state = undefined;
window.acquireVsCodeApi = () => ({
  postMessage: (m) => window.__sent.push(JSON.parse(JSON.stringify(m))),
  getState: () => window.__state,
  setState: (s) => { window.__state = s; },
});
window.__host = (type, payload) => window.dispatchEvent(new MessageEvent("message", { data: { v: 1, type, payload } }));
</script>
<script>${js.replace(/<\/script>/g, "<\\/script>")}</script>
</body></html>`);
