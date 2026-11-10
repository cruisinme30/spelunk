#!/usr/bin/env node
// Release benchmark gate. Require every workload, complete results, p95
// response/completion latency, and retained heap measurements from both engines.
// The heap sum is a conservative proxy, not a measurement of daemon RSS.
// Usage: node scripts/checkBudgets.mjs bench.txt
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const FAMILIES = {
  BenchmarkLargeRepo: {
    response: 100,
    names: ["literal", "conjunction", "regex", "case-filter", "exclude", "file", "no-hit"],
  },
  BenchmarkLargeHistory: {
    response: 250,
    names: ["literal", "regex", "message", "author", "boolean", "case", "no-hit"],
  },
};
// Existing plan: 2-second search hard limit, 1 GB steady-state memory,
// 2-minute tree build, 15-minute history read, first commits within a minute.
const TOTAL_MS = 2000;
const HEAP_BYTES = 1_000_000_000;
const MIN_SAMPLES = 20;
const TREE_BUILD_MS = 120_000;
const HISTORY_READ_MS = 900_000;
const HISTORY_FIRST_SEARCHABLE_MS = 60_000;

/** Read an entire Go duration, rejecting malformed or missing measurements. */
function milliseconds(duration) {
  const parts = [...duration.matchAll(/(\d+(?:\.\d+)?)(ns|µs|ms|s|m|h)/g)];
  if (parts.map(([part]) => part).join("") !== duration) return Number.NaN;
  return parts.reduce(
    (sum, [, value, unit]) =>
      sum + Number(value) * { h: 3_600_000, m: 60_000, s: 1000, ms: 1, µs: 0.001, ns: 0.000_001 }[unit],
    0,
  );
}

/** Require a finite nonnegative metric, then enforce its maximum. */
function withinBudget(value, maximum, label, failures) {
  if (!Number.isFinite(value) || value < 0 || value > maximum) failures.push(`${label}: ${value} (maximum ${maximum})`);
}

/** Check one completed benchmark line, including the no-hit negative control. */
function checkSearch(match, failures, seen) {
  const [, family, name, iterations, tail] = match;
  const suite = FAMILIES[family];
  const label = `${family}/${name}`;
  if (!suite.names.includes(name)) failures.push(`unexpected workload ${label}`);
  seen.add(label);
  const entries = [...tail.matchAll(/(\S+)\s+(\S+)(?:\s+|$)/g)].map(([, value, unit]) => [unit, Number(value)]);
  if (new Set(entries.map(([unit]) => unit)).size !== entries.length) failures.push(`${label} repeats a metric`);
  const metrics = Object.fromEntries(entries);
  withinBudget(metrics["ms-response-p95"], suite.response, `${label} response p95`, failures);
  withinBudget(metrics["ms-total-p95"], TOTAL_MS, `${label} total p95`, failures);
  withinBudget(metrics.matches, Number.MAX_SAFE_INTEGER, `${label} match count`, failures);
  if (!Number.isInteger(metrics.matches)) failures.push(`${label} has no integer match count`);
  if (metrics.truncated !== 0) failures.push(`${label} is truncated or lacks truncation evidence`);
  if (!Number.isSafeInteger(metrics.samples) || metrics.samples !== Number(iterations) || metrics.samples < MIN_SAMPLES)
    failures.push(`${label} needs at least ${MIN_SAMPLES} measured samples`);
  if (metrics["ms-response-p95"] > metrics["ms-total-p95"]) failures.push(`${label} response exceeds completion`);
  if (name === "no-hit" && (metrics.matches !== 0 || metrics["ms-response-p95"] !== metrics["ms-total-p95"])) {
    failures.push(`${label} must have zero matches and measure completion as its response`);
  }
}

/** Validate build timing and retained heap; return the largest heap observed. */
function checkBuilds(output, family, failures) {
  const tree = family === "tree";
  const pattern = tree
    ? /indexed (\d+) files in (\S+); heap (\d+) bytes/g
    : /read (\d+) commits in (\S+) \(newest searchable after (\S+)\); heap (\d+) bytes/g;
  const records = [...output.matchAll(pattern)];
  if (records.length === 0) failures.push(`no ${family} build and heap measurements found`);
  let heap = 0;
  for (const record of records) {
    if (Number(record[1]) < 1) failures.push(`${family} corpus is empty`);
    withinBudget(milliseconds(record[2]), tree ? TREE_BUILD_MS : HISTORY_READ_MS, `${family} build time`, failures);
    if (!tree) withinBudget(milliseconds(record[3]), HISTORY_FIRST_SEARCHABLE_MS, "first searchable history", failures);
    heap = Math.max(heap, Number(record[tree ? 3 : 4]));
  }
  return heap;
}

/** Return every gate failure; missing evidence is a failure, never a skipped budget. */
export function checkBudgets(output) {
  const failures = [];
  const seen = new Set();
  for (const match of output.matchAll(/^(BenchmarkLarge(?:Repo|History))\/(\S+?)-\d+\s+(\d+)\s+(.+)$/gm)) {
    checkSearch(match, failures, seen);
  }
  for (const [family, suite] of Object.entries(FAMILIES)) {
    for (const name of suite.names) {
      if (!seen.has(`${family}/${name}`)) failures.push(`missing workload ${family}/${name}`);
    }
  }
  if (/^(?:FAIL\b|--- FAIL:|panic:)/m.test(output)) failures.push("benchmark process reported failure");
  const heap = checkBuilds(output, "tree", failures) + checkBuilds(output, "history", failures);
  withinBudget(heap, HEAP_BYTES, "combined retained heap bytes (RSS not measured)", failures);
  return failures;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const file = process.argv[2];
  if (!file) throw new Error("usage: node scripts/checkBudgets.mjs <go test -bench output>");
  const failures = checkBudgets(readFileSync(file, "utf8"));
  if (failures.length > 0) {
    console.error(`Benchmark gate failed:\n${failures.map((failure) => `- ${failure}`).join("\n")}`);
    process.exitCode = 1;
  } else {
    console.log(
      "Benchmark gate passed: complete workloads, p95 response/total latency, and retained heap proxy. RSS not measured.",
    );
  }
}
