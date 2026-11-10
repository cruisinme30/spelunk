// Select behavioral test layers for a develop PR. Static quality always runs
// through test-all.sh; changes to shared contracts or unknown paths run all layers.
import { execFileSync, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const ALL_LAYERS = ["daemon", "extension", "webview"];

/** Select the dependency closure of test layers affected by repository paths. */
export function testLayers(paths) {
  const selected = new Set();
  for (const path of paths) {
    // Backend changes can affect every consumer. Shared schemas, tooling,
    // fixtures and unrecognized paths also use the safe full-suite default.
    if (path.startsWith("daemon/")) return [...ALL_LAYERS];
    if (path.startsWith("extension/")) {
      selected.add("extension");
      selected.add("webview");
    } else if (path.startsWith("webview/")) {
      selected.add("webview");
    } else if (!(path.startsWith("docs/") || (path.endsWith(".md") && !path.includes("/")) || path === "LICENSE")) {
      return [...ALL_LAYERS];
    }
  }
  return ALL_LAYERS.filter((layer) => selected.has(layer));
}

/** Resolve refs before diffing so option-shaped or missing refs cannot be ignored. */
function commit(ref) {
  return execFileSync("git", ["rev-parse", "--verify", "--end-of-options", `${ref}^{commit}`], {
    encoding: "utf8",
  }).trim();
}

/** Read explicit ref arguments; --print only reports the selected layer list. */
function options(args) {
  const result = { base: "origin/develop", head: "HEAD", print: false };
  while (args.length > 0) {
    const option = args.shift();
    if (option === "--print") result.print = true;
    else if ((option === "--base" || option === "--head") && args.length > 0) result[option.slice(2)] = args.shift();
    else throw new Error(`unknown or incomplete argument: ${option}`);
  }
  return result;
}

/** Run one centrally maintained test runner with the computed layer selection. */
function main(args) {
  const { base, head, print } = options(args);
  const paths = execFileSync("git", ["diff", "--name-only", "--no-renames", "-z", commit(base), commit(head), "--"], {
    encoding: "utf8",
  })
    .split("\0")
    .filter(Boolean);
  const layers = testLayers(paths).join(",") || "none";
  if (print) {
    console.log(layers);
    return;
  }
  console.log(`Develop validation: all static checks; behavioral layers: ${layers}`);
  const runner = fileURLToPath(new URL("test-all.sh", import.meta.url));
  const result = spawnSync(runner, ["--layers", layers], { stdio: "inherit" });
  if (result.error) throw result.error;
  if (result.signal) throw new Error(`test runner terminated by ${result.signal}`);
  process.exitCode = result.status ?? 1;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main(process.argv.slice(2));
