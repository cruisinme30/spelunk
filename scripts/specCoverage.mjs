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
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(fileURLToPath(import.meta.url), "..", "..");
const read = (p) => readFileSync(join(root, p), "utf8");
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
  ["and", "or", "not", "group", "phrase", "regex"].map((s) => `syntax:${s}`),
  "query grammar",
);
add(
  Object.keys(schema["x-rpc-methods"]).map((m) => `rpc:${m}`),
  "x-rpc-methods",
);
add(
  Object.keys(schema["x-webview-messages"]).map((m) => `msg:${m}`),
  "x-webview-messages",
);
add(
  Object.keys(manifest.contributes.configuration.properties).map((k) => `setting:${k.replace(/^unifiedSearch\./, "")}`),
  "package.json settings",
);
add(
  manifest.contributes.commands.map((c) => `command:${c.command.replace(/^unifiedSearch\./, "")}`),
  "package.json commands",
);
add(
  Array.from({ length: 18 }, (_, i) => `mock:${i + 1}`),
  "docs/dev/mocks.md",
);
add(
  ["daemon-crash", "index-corrupt", "repo-indexing", "no-git", "ref-stale", "disk-full"].map((f) => `failure:${f}`),
  "failure table",
);

// Diagnostic codes are declared as `Diag… = "code"` constants in the query package.
const diagDir = join(root, "daemon/internal/query");
for (const file of safeList(diagDir).filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))) {
  for (const m of readFileSync(join(diagDir, file), "utf8").matchAll(/^\s*Diag\w+\s*=\s*"([a-z_]+)"/gm)) {
    required.set(`diag:${m[1]}`, "daemon/internal/query diagnostic codes");
  }
}

// Collect @covers tags from every test file.
const testFile = /(_test\.go|\.test\.ts|\.test\.mjs)$/;
const covered = new Map(); // id -> [files]
for (const file of walk(root)) {
  if (!testFile.test(file)) continue;
  for (const m of readFileSync(file, "utf8").matchAll(/@covers\s+([^\n*]+)/g)) {
    for (const id of m[1].trim().split(/\s+/)) {
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
  const g = groups.get(kind) ?? { total: 0, done: 0 };
  g.total++;
  if (covered.has(id)) g.done++;
  groups.set(kind, g);
}

console.log(`Spec coverage: ${required.size - missing.length}/${required.size}`);
for (const [kind, g] of groups) console.log(`  ${kind.padEnd(8)} ${String(g.done).padStart(3)}/${g.total}`);
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
