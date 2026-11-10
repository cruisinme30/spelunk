// Regression tests for the release gate: incomplete or misleading evidence fails.
import assert from "node:assert/strict";
import { test } from "node:test";
import { checkBudgets } from "./checkBudgets.mjs";

const workloads = {
  BenchmarkLargeRepo: ["literal", "conjunction", "regex", "case-filter", "exclude", "file", "no-hit"],
  BenchmarkLargeHistory: ["literal", "regex", "message", "author", "boolean", "case", "no-hit"],
};
/** A complete passing release record, without depending on a real machine's speed. */
function passingOutput() {
  return [
    "indexed 100 files in 1s; heap 100000000 bytes",
    "read 100 commits in 2s (newest searchable after 100ms); heap 200000000 bytes",
    ...Object.entries(workloads).flatMap(([family, names]) =>
      names.map(
        (name) =>
          `${family}/${name}-4 20 1000000 ns/op ${name === "no-hit" ? 0 : 4} matches 1 ms-response-p95 1 ms-total-p95 20 samples 0 truncated`,
      ),
    ),
    "PASS",
  ].join("\n");
}

test("complete, measured evidence passes", () => {
  assert.deepEqual(checkBudgets(passingOutput()), []);
});

test("every missing workload fails even when another workload ran", () => {
  for (const [family, names] of Object.entries(workloads)) {
    for (const name of names) {
      const output = passingOutput()
        .split("\n")
        .filter((line) => !line.startsWith(`${family}/${name}-`))
        .join("\n");
      assert.ok(checkBudgets(output).includes(`missing workload ${family}/${name}`));
    }
  }
});

test("old synthetic nine-second, nine-gigabyte record cannot pass", () => {
  const output =
    "indexed 1 files in 1s; heap 9000 MB\nread 1 commits in 1s (newest searchable after 1s); heap 9000 MB\nBenchmarkLargeRepo/no_match-4 10 9000000000 ns/op 0 ms-to-first 9000 ms-total";
  assert.ok(checkBudgets(output).length > 0);
});

test("latency, memory, truncation and sample regressions fail individually", () => {
  for (const [before, after, error] of [
    ["1 ms-response-p95", "101 ms-response-p95", "response p95"],
    ["1 ms-total-p95", "9000 ms-total-p95", "total p95"],
    ["heap 100000000 bytes", "heap 9000000000 bytes", "retained heap"],
    ["20 samples", "10 samples", "measured samples"],
    ["0 truncated", "1 truncated", "truncated"],
    ["1 ms-total-p95", "NaN ms-total-p95", "total p95"],
    ["4 matches", "NaN matches", "match count"],
    ["indexed 100 files in 1s", "indexed 100 files in nonsense", "build time"],
  ]) {
    assert.ok(
      checkBudgets(passingOutput().replace(before, after)).some((failure) => failure.includes(error)),
      `${after} should fail ${error}`,
    );
  }
});

test("no-hit response is completion, never a fake instantaneous first result", () => {
  const output = passingOutput().replace("0 matches 1 ms-response-p95", "0 matches 0 ms-response-p95");
  assert.ok(checkBudgets(output).some((failure) => failure.includes("measure completion")));
});

test("failed benchmark process cannot pass despite complete metrics", () => {
  assert.ok(checkBudgets(passingOutput() + "\nFAIL").includes("benchmark process reported failure"));
});

test("duplicate metrics cannot hide an earlier over-budget measurement", () => {
  const output = passingOutput().replace("1 ms-total-p95", "9000 ms-total-p95 1 ms-total-p95");
  assert.ok(checkBudgets(output).some((failure) => failure.includes("repeats a metric")));
});

test("an overflowing sample count cannot satisfy the minimum", () => {
  const output = passingOutput()
    .replace("-4 20 ", `-4 ${"9".repeat(400)} `)
    .replace("20 samples", "Infinity samples");
  assert.ok(checkBudgets(output).some((failure) => failure.includes("measured samples")));
});
