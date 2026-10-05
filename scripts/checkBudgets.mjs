#!/usr/bin/env node
// Checks benchmark output against the performance budgets of the
// implementation plan, and fails when one is exceeded. It reads the output
// of `go test -bench 'LargeRepo|LargeHistory' -v` (see the Budgets workflow).
//
// Usage: node scripts/checkBudgets.mjs bench.txt
import { readFileSync } from "node:fs";

/** Budgets in milliseconds, by benchmark family. */
const FIRST_RESULT_MS = { BenchmarkLargeRepo: 100, BenchmarkLargeHistory: 250 };
/** How long the working-tree index may take to build. */
const TREE_BUILD_MS = 2 * 60_000;
/** How long the history index may take to read. */
const HISTORY_READ_MS = 15 * 60_000;
/** How long until the first commits of a history read are searchable. */
const HISTORY_FIRST_SEARCHABLE_MS = 60_000;

const file = process.argv[2];
if (!file) throw new Error("usage: node scripts/checkBudgets.mjs <go test -bench output>");
const output = readFileSync(file, "utf8");

/** Go's duration strings ("1m2.5s", "608ms", "21.95s") in milliseconds. */
function milliseconds(duration) {
  let total = 0;
  for (const [, value, unit] of duration.matchAll(/([\d.]+)(ms|µs|h|m|s)/g)) {
    total += Number(value) * { h: 3_600_000, m: 60_000, s: 1000, ms: 1, µs: 0.001 }[unit];
  }
  return total;
}

const failures = [];
let checked = 0;
for (const [, family, name, firstMs] of output.matchAll(/^(Benchmark\w+)\/(\S+?)-\d+\s.*?([\d.]+) ms-to-first/gm)) {
  checked++;
  const budget = FIRST_RESULT_MS[family];
  const verdict = Number(firstMs) <= budget ? "ok" : "OVER";
  console.log(`${verdict.padEnd(4)} ${family}/${name}: first result in ${firstMs} ms (budget ${budget} ms)`);
  if (verdict === "OVER") failures.push(`${family}/${name}`);
}
const built = /indexed \d+ files in (\S+);/.exec(output);
if (built) {
  console.log(`working tree indexed in ${built[1]}`);
  if (milliseconds(built[1]) > TREE_BUILD_MS) failures.push("working-tree build time");
}
const read = /read \d+ commits in (\S+) \(newest searchable after (\S+)\)/.exec(output);
if (read) {
  const [, total, first] = read;
  console.log(`history read in ${total}, newest searchable after ${first}`);
  if (milliseconds(total) > HISTORY_READ_MS) failures.push("history read time");
  if (milliseconds(first) > HISTORY_FIRST_SEARCHABLE_MS) failures.push("time to the first searchable commits");
}
if (checked === 0) failures.push("no benchmark results found");
if (failures.length > 0) {
  console.error(`Over budget: ${failures.join(", ")}`);
  process.exitCode = 1;
}
