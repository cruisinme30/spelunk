// Bundles the extension host (CommonJS) and the webview with esbuild.
// --tests also bundles src/test/*.test.ts into dist-test/ for `node --test`,
// and --e2e bundles src/e2e/commands.test.ts into dist-e2e/ for scripts/e2e.mjs.
import { build } from "esbuild";
import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const webview = join(here, "..", "webview");
const tests = process.argv.includes("--tests");
const endToEnd = process.argv.includes("--e2e");
/** How the extension host and its tests are bundled: CommonJS for Node, with vscode left to the host. */
const nodeBundle = {
  bundle: true,
  platform: "node",
  format: "cjs",
  target: "node18",
  external: ["vscode"],
  logLevel: "warning",
};

await build({
  ...nodeBundle,
  entryPoints: [join(here, "src/extension.ts")],
  outfile: join(here, "dist/extension.js"),
  sourcemap: true,
});

if (existsSync(join(webview, "src/main.ts"))) {
  mkdirSync(join(here, "dist/webview"), { recursive: true });
  // The search panel, the help page and the welcome page share main.css.
  for (const [entry, out] of [
    ["main.ts", "main.js"],
    ["helpPage.ts", "help.js"],
    ["welcomePage.ts", "welcome.js"],
  ]) {
    await build({
      entryPoints: [join(webview, "src", entry)],
      bundle: true,
      platform: "browser",
      format: "iife",
      target: "es2022",
      outfile: join(here, "dist/webview", out),
      sourcemap: false,
      logLevel: "warning",
    });
  }
  if (existsSync(join(webview, "src/main.css")))
    cpSync(join(webview, "src/main.css"), join(here, "dist/webview/main.css"));
}

if (tests) {
  rmSync(join(here, "dist-test"), { recursive: true, force: true });
  const testDirectory = join(here, "src", "test");
  const entries = readdirSync(testDirectory)
    .filter((file) => file.endsWith(".test.ts"))
    .map((file) => join(testDirectory, file));
  await build({
    ...nodeBundle,
    entryPoints: entries,
    outdir: join(here, "dist-test"),
  });
}

if (endToEnd) {
  await build({
    ...nodeBundle,
    entryPoints: [join(here, "src/e2e/commands.test.ts")],
    outfile: join(here, "dist-e2e/index.js"),
  });
}
