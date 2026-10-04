// Bundles the extension host (CommonJS) and the webview with esbuild.
// --tests also compiles test/*.test.ts for `node --test`.
import { build } from "esbuild";
import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const webview = join(here, "..", "webview");
const tests = process.argv.includes("--tests");
const watch = false;

await build({
  entryPoints: [join(here, "src/extension.ts")],
  bundle: true, platform: "node", format: "cjs", target: "node18",
  external: ["vscode"], outfile: join(here, "dist/extension.js"), sourcemap: true, logLevel: "warning",
});

if (existsSync(join(webview, "src/main.ts"))) {
  mkdirSync(join(here, "dist/webview"), { recursive: true });
  await build({
    entryPoints: [join(webview, "src/main.ts")],
    bundle: true, platform: "browser", format: "iife", target: "es2020",
    outfile: join(here, "dist/webview/main.js"), sourcemap: false, logLevel: "warning",
  });
  if (existsSync(join(webview, "src/main.css"))) cpSync(join(webview, "src/main.css"), join(here, "dist/webview/main.css"));
}

if (tests) {
  rmSync(join(here, "dist-test"), { recursive: true, force: true });
  const entries = readdirSync(join(here, "test")).filter((f) => f.endsWith(".test.ts")).map((f) => join(here, "test", f));
  await build({
    entryPoints: entries, bundle: true, platform: "node", format: "cjs", target: "node18",
    external: ["vscode"], outdir: join(here, "dist-test"), logLevel: "warning",
  });
}
void watch;
