// Reference benchmark checks are cheap and never launch large-corpus searches.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import {
  compatibleQuery,
  identities,
  identityDigest,
  comparableRecord,
  nativeCommand,
  runNative,
  referencesAgree,
  combineSearchRecords,
} from "./referenceSearchBench.mjs";

test("simple queries retain explicit case and word flags", () => {
  assert.deepEqual(compatibleQuery("case:yes content:SPDX"), {
    pattern: "SPDX",
    fixed: true,
    ignoreCase: false,
    word: false,
  });
  assert.equal(compatibleQuery("word:yes content:flat").word, true);
  assert.equal(compatibleQuery("/read.*value/").fixed, false);
});

test("unsupported semantics are not approximated silently", () => {
  for (const text of ["content:flat in:code", "sym:flat", "content:a OR content:b", "type:code /a/", "content:é"]) {
    assert.equal(compatibleQuery(text), undefined);
  }
});

test("native formats agree for colons and newlines in paths", () => {
  const ripgrep = Buffer.from("./dir/a:b\0" + "12:text:still text\n./line\nbreak\0" + "2:last\n");
  const git = Buffer.from("./dir/a:b\0" + "12\0text:still text\n./line\nbreak\0" + "2\0last\n");
  const wanted = [
    { path: Buffer.from("dir/a:b"), line: 12 },
    { path: Buffer.from("line\nbreak"), line: 2 },
  ];
  assert.deepEqual(identities(ripgrep, "ripgrep"), wanted);
  assert.deepEqual(identities(git, "git-grep"), wanted);
});

test("invalid native output fails instead of producing a partial digest", () => {
  for (const raw of ["missing delimiter", "path\0zero:text\n", "path\0" + "0:text\n"]) {
    assert.throws(() => identities(Buffer.from(raw), "ripgrep"));
  }
});

test("digest preserves duplicate lines", () => {
  const rows = [2, 2, 10].map((line) => ({ path: Buffer.from("a"), line }));
  const raw = "a\0" + "2\na\0" + "2\na\0" + "10\n";
  assert.equal(identityDigest(rows), createHash("sha256").update(raw).digest("hex"));
});

test("count-only, incomplete and filename pages are rejected", () => {
  assert.ok(comparableRecord());
  assert.ok(comparableRecord({ all_complete: false }));
  const record = {
    all_complete: true,
    all_results_and_stats_stable: true,
    first_session_pass: { count: 1, emitted: 1 },
  };
  assert.ok(comparableRecord(record));
  Object.assign(record.first_session_pass, { line_emitted: 1, line_identities_sha256: "example" });
  assert.equal(comparableRecord(record), undefined);
  record.first_session_pass.line_emitted = 0;
  assert.ok(comparableRecord(record));
});

test("reference comparison requires counts and both complete native answers", () => {
  const sample = { count: 1000, line_identities_sha256: "page" };
  const references = Object.fromEntries(
    ["ripgrep", "git-grep"].map((tool) => [
      tool,
      {
        count: 1000,
        full_line_identities_sha256: "full",
        page_line_identities_sha256: "page",
      },
    ]),
  );
  assert.equal(referencesAgree(references, sample), true);
  references["git-grep"].full_line_identities_sha256 = "different";
  assert.equal(referencesAgree(references, sample), false);
});

test("session aggregation cannot conceal earlier incompleteness or changed answers", () => {
  const complete = {
    all_complete: true,
    all_results_and_stats_stable: true,
    first_session_pass: { count: 1, emitted: 1, line_emitted: 1, line_identities_sha256: "same" },
  };
  assert.equal(combineSearchRecords(complete, complete).all_complete, true);
  assert.equal(combineSearchRecords({ ...complete, all_complete: false }, complete).all_complete, false);
  const changed = { ...complete, first_session_pass: { ...complete.first_session_pass, count: 2 } };
  assert.equal(combineSearchRecords(complete, changed).all_results_and_stats_stable, false);
});

test("commands disable configuration and use explicit workers and filesystem mode", () => {
  const query = compatibleQuery("word:yes content:flat");
  const ripgrep = nativeCommand("ripgrep", query, 4);
  const git = nativeCommand("git-grep", query, 4);
  assert.ok(ripgrep.includes("--no-config"));
  assert.ok(ripgrep.includes("--word-regexp"));
  assert.ok(git.includes("--no-index"));
  assert.ok(git.includes("--threads=4"));
});

/** Launch a tiny fixture process without invoking a real corpus search. */
function runFixture(source) {
  return runNative({ argv: [process.execPath, "-e", source], capture: true });
}

test("commands accept empty search results but reject warnings and failures", () => {
  assert.doesNotThrow(() => runFixture("process.exitCode = 1"));
  assert.throws(() => runFixture("process.exitCode = 2"), /exited 2/);
  assert.throws(() => runFixture("process.stderr.write('warning')"), /warned/);
});

test("a stuck command is killed by the configured timeout", () => {
  assert.throws(
    () =>
      runNative({
        argv: [process.execPath, "-e", "setInterval(() => {}, 1000)"],
        capture: true,
        timeoutMs: 50,
      }),
    /ETIMEDOUT/,
  );
});
