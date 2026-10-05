#!/usr/bin/env node
// Spec coverage: lists every spec item that no test proves yet.
//
// Tests declare what they prove with `@covers <id> ...`. The spec IDs come
// from the sources of truth where possible: protocol/protocol.schema.json
// (op: operators, rpc: methods, msg: webview messages), extension/package.json
// (setting: and command:), and the daemon's diagnostic codes (diag:). The
// panel's screens (screen:) come from the mock table in docs/dev/mocks.md. The
// query syntax (syntax:) and the failure modes (failure:) are listed below.
//
// It always fails on an @covers id that names no spec item (a typo or a
// removed item); gaps are only reported.
//
// Usage: node scripts/specCoverage.mjs [--strict]
//   --strict also fails when anything is uncovered (the release gate).
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

import { screenIds } from "./mockTable.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
/** What can go wrong, and must be handled visibly: each needs a test tagged failure:<name>. */
const FAILURE_MODES = ["daemon-crash", "index-corrupt", "repo-indexing", "no-git", "ref-stale", "disk-full"];
/** The query syntax that isn't an operator. */
const SYNTAX = ["and", "or", "not", "group", "phrase", "regex"];
const read = (path) => readFileSync(join(root, path), "utf8");
const schema = JSON.parse(read("protocol/protocol.schema.json"));
const manifest = JSON.parse(read("extension/package.json"));

/** Every spec id, in report order; the part before the colon is its kind. */
const required = new Set();
/** Adds `kind:name` for each name. */
const add = (kind, names) => {
  for (const name of names) required.add(`${kind}:${name}`);
};
const withoutPrefix = (key) => key.replace(/^spelunk\./, "");

add("op", schema.$defs.OpName.enum);
add("syntax", SYNTAX);
add("rpc", Object.keys(schema["x-rpc-methods"]));
add("msg", Object.keys(schema["x-webview-messages"]));
add(
  "setting",
  Object.keys(manifest.contributes.configuration.properties).map((key) => withoutPrefix(key)),
);
add(
  "command",
  manifest.contributes.commands.map((command) => withoutPrefix(command.command)),
);
add("screen", screenIds(root));
add("failure", FAILURE_MODES);

// Diagnostic codes are declared as `Diag… = "code"` constants in the query package.
const queryPackage = join(root, "daemon/internal/query");
const goSources = filesInOrNone(queryPackage).filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"));
for (const file of goSources) {
  const source = readFileSync(join(queryPackage, file), "utf8");
  add(
    "diag",
    [...source.matchAll(/^\s*Diag\w+\s*=\s*"([a-z_]+)"/gm)].map((match) => match[1]),
  );
}

// Collect @covers tags from every test file.
const testFile = /(_test\.go|\.test\.ts|\.test\.mjs)$/;
const covered = new Map(); // id -> [files]
for (const file of walk(root)) {
  if (!testFile.test(file)) continue;
  for (const match of readFileSync(file, "utf8").matchAll(/@covers\s+([^\n*]+)/g)) {
    for (const id of match[1].trim().split(/\s+/)) {
      if (!covered.has(id)) covered.set(id, []);
      covered.get(id).push(relative(root, file));
    }
  }
}

const missing = [...required].filter((id) => !covered.has(id));
const unknown = [...covered.keys()].filter((id) => !required.has(id));
const groups = new Map();
for (const id of required) {
  const kind = id.split(":")[0];
  const group = groups.get(kind) ?? { total: 0, done: 0 };
  group.total++;
  if (covered.has(id)) group.done++;
  groups.set(kind, group);
}

console.log(`Spec coverage: ${required.size - missing.length}/${required.size}`);
for (const [kind, group] of groups) console.log(`  ${kind.padEnd(8)} ${String(group.done).padStart(3)}/${group.total}`);
if (missing.length > 0) console.log(`Uncovered: ${missing.join(" ")}`);
if (unknown.length > 0) {
  console.log(`Unknown @covers ids (typo or stale): ${unknown.join(" ")}`);
  process.exitCode = 1;
}
if (process.argv.includes("--strict") && missing.length > 0) process.exitCode = 1;

/** The names in `directory`, or none if it doesn't exist (a checkout without the daemon). */
function filesInOrNone(directory) {
  try {
    return readdirSync(directory);
  } catch {
    return [];
  }
}

/** Every file under `directory`, skipping dependencies, build output and dot-folders. */
function* walk(directory) {
  for (const name of readdirSync(directory)) {
    if (name === "node_modules" || name.startsWith(".") || name === "dist" || name === "dist-test") continue;
    const path = join(directory, name);
    if (statSync(path).isDirectory()) yield* walk(path);
    else yield path;
  }
}
