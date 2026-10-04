#!/usr/bin/env node
// Spec coverage: every spec item must be proven by at least one test.
//
// Spec IDs come straight from the sources of truth: protocol/protocol.schema.json
// (operators, RPC methods, webview messages), extension/package.json (settings,
// commands, shortcut presets), the daemon's diagnostic codes, the 18 mocks and
// the failure table. Tests declare what they prove with `@covers <id> ...`.
//
// Usage: node scripts/specCoverage.mjs [--strict]
//   --strict exits non-zero when anything is uncovered (the release gate).
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
/** Mocks 1-18 in docs/dev/mocks.md. */
const MOCK_COUNT = 18;
/** Rows of the failure table in docs/dev/implementation-plan.md. */
const FAILURE_ROWS = ["daemon-crash", "index-corrupt", "repo-indexing", "no-git", "ref-stale", "disk-full"];
const read = (path) => readFileSync(join(root, path), "utf8");
const schema = JSON.parse(read("protocol/protocol.schema.json"));
const manifest = JSON.parse(read("extension/package.json"));

/** @type {Map<string, string>} id -> where it comes from */
const required = new Map();
const add = (ids, source) => ids.forEach((id) => required.set(id, source));

add(
  schema.$defs.OpName.enum.map((op) => `op:${op}`),
  "OpName in protocol.schema.json",
);
add(
  ["and", "or", "not", "group", "phrase", "regex"].map((construct) => `syntax:${construct}`),
  "query grammar",
);
add(
  Object.keys(schema["x-rpc-methods"]).map((method) => `rpc:${method}`),
  "x-rpc-methods",
);
add(
  Object.keys(schema["x-webview-messages"]).map((type) => `msg:${type}`),
  "x-webview-messages",
);
add(
  Object.keys(manifest.contributes.configuration.properties).map(
    (key) => `setting:${key.replace(/^unifiedSearch\./, "")}`,
  ),
  "package.json settings",
);
add(
  manifest.contributes.commands.map((command) => `command:${command.command.replace(/^unifiedSearch\./, "")}`),
  "package.json commands",
);
add(
  Array.from({ length: MOCK_COUNT }, (_, i) => `mock:${i + 1}`),
  "docs/dev/mocks.md",
);
add(
  FAILURE_ROWS.map((row) => `failure:${row}`),
  "failure table",
);

// Diagnostic codes are declared as `Diag… = "code"` constants in the query package.
const diagDir = join(root, "daemon/internal/query");
for (const file of safeList(diagDir).filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))) {
  for (const match of readFileSync(join(diagDir, file), "utf8").matchAll(/^\s*Diag\w+\s*=\s*"([a-z_]+)"/gm)) {
    required.set(`diag:${match[1]}`, "daemon/internal/query diagnostic codes");
  }
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

const missing = [...required.keys()].filter((id) => !covered.has(id));
const unknown = [...covered.keys()].filter((id) => !required.has(id));
const groups = new Map();
for (const id of required.keys()) {
  const kind = id.split(":")[0];
  const group = groups.get(kind) ?? { total: 0, done: 0 };
  group.total++;
  if (covered.has(id)) group.done++;
  groups.set(kind, group);
}

console.log(`Spec coverage: ${required.size - missing.length}/${required.size}`);
for (const [kind, group] of groups) console.log(`  ${kind.padEnd(8)} ${String(group.done).padStart(3)}/${group.total}`);
if (missing.length) console.log(`Uncovered: ${missing.join(" ")}`);
if (unknown.length) {
  console.log(`Unknown @covers ids (typo or stale): ${unknown.join(" ")}`);
  process.exitCode = 1;
}
if (process.argv.includes("--strict") && missing.length) process.exitCode = 1;

function safeList(dir) {
  try {
    return readdirSync(dir);
  } catch {
    return [];
  }
}

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name.startsWith(".") || name === "dist" || name === "dist-test") continue;
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* walk(path);
    else yield path;
  }
}
