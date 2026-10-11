// The query a selection or a name at the cursor becomes. The last test runs
// each one against the real daemon, to prove the box reads it as the text
// that was selected.
// @covers screen:seed-from-editor
import assert from "node:assert/strict";
import { readdirSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { makeRoot } from "../roots";
import { definitionsQuery, MAX_SEED_CHARS, MAX_SEED_LINES, seedFromSelection, textTerm, usesQuery } from "../seedQuery";
import { newTestDaemon, SKIP_WITHOUT_DAEMON as skip, untilIndexed } from "./realDaemon";

const WORKSPACE = join(__dirname, "../../testdata/workspace");

/** The query a selection seeds after `last`, or the reason it doesn't. */
function seed(selection: string, last = "timeout"): string {
  const result = seedFromSelection(selection, last);
  return "query" in result ? result.query : `(${result.skipped})`;
}

test("a line is searched as an exact phrase, even a single word", () => {
  assert.equal(seed("RetryPolicy"), '"RetryPolicy"');
  assert.equal(seed("retry_policy.next_delay"), '"retry_policy.next_delay"');
  assert.equal(seed("  RetryPolicy  "), '"RetryPolicy"', "spaces around the selection are dropped");
  assert.equal(seed("charge failed after retries"), '"charge failed after retries"');
});

test("text that looks like query syntax is quoted, with its quotes and backslashes escaped", () => {
  assert.equal(seed('"retrying charge in %.1fs"'), String.raw`"\"retrying charge in %.1fs\""`);
  assert.equal(seed('content:"retry"'), String.raw`"content:\"retry\""`);
  assert.equal(seed("TransientError:"), '"TransientError:"');
  assert.equal(seed("std::vector"), '"std::vector"');
  assert.equal(seed("-v"), '"-v"');
  assert.equal(seed("OR"), '"OR"');
  assert.equal(seed("NOT"), '"NOT"');
  assert.equal(seed("or"), '"or"');
  assert.equal(seed("f(x)"), '"f(x)"');
  assert.equal(seed("a|b"), '"a|b"');
  assert.equal(seed("/charges"), '"/charges"', "a leading slash would open a regex");
  assert.equal(seed(String.raw`C:\temp`), String.raw`"C:\\temp"`);
  assert.equal(seed(".hidden"), '".hidden"');
});

test("with .* pressed in the last query, the selection is an escaped regex", () => {
  assert.equal(
    seed("self.retry_policy.next_delay(attempt)", "/retry.*delay/"),
    String.raw`/self\.retry_policy\.next_delay\(attempt\)/`,
  );
  assert.equal(seed("a/b", "/x/"), String.raw`/a\/b/`, "a slash can't close the regex");
  assert.equal(seed("price $5 [x]", "/x/"), String.raw`/price \$5 \[x\]/`);
  assert.equal(seed("RetryPolicy", 'lang:python "/not a regex/"'), '"RetryPolicy"', "a phrase with slashes isn't .*");
});

test("Aa and ab carry over from the last query; filters and other words don't", () => {
  assert.equal(seed("RetryPolicy", "case:yes word:yes timeout"), 'case:yes word:yes "RetryPolicy"');
  assert.equal(seed("RetryPolicy", "case:smart word:no timeout"), 'case:smart word:no "RetryPolicy"');
  assert.equal(seed("RetryPolicy", "lang:python f:src/ timeout"), '"RetryPolicy"');
  assert.equal(seed("RetryPolicy", 'case:yes "case:no inside a phrase"'), 'case:yes "RetryPolicy"');
  assert.equal(seed("a.b", "case:yes /x/"), String.raw`case:yes /a\.b/`);
  assert.equal(seed("RetryPolicy", ""), '"RetryPolicy"');
});

test("several lines become one regex across them, indentation and blank lines ignored", () => {
  const selection = "except TransientError:\n                delay = self.retry_policy.next_delay(attempt)";
  assert.equal(
    seed(selection),
    String.raw`/except TransientError:\n\s*delay = self\.retry_policy\.next_delay\(attempt\)/`,
  );
  assert.equal(seed("a\r\n\r\n  b"), String.raw`/a\n\s*b/`, "Windows line endings, and a blank line");
  assert.equal(seed("a\nb", "case:yes timeout"), String.raw`case:yes /a\n\s*b/`);
  assert.equal(seed("RetryPolicy\n"), '"RetryPolicy"', "a trailing newline leaves one line");
});

test("an empty or too long selection keeps the last query", () => {
  assert.equal(seed("   \n\t"), "(empty)");
  assert.equal(
    seed(Array.from({ length: MAX_SEED_LINES }, () => "x").join("\n")),
    String.raw`/x\n\s*x\n\s*x\n\s*x\n\s*x\n\s*x\n\s*x\n\s*x\n\s*x\n\s*x/`,
  );
  assert.equal(seed(Array.from({ length: MAX_SEED_LINES + 1 }, () => "x").join("\n")), "(tooLong)");
  assert.equal(seed("x".repeat(MAX_SEED_CHARS)), `"${"x".repeat(MAX_SEED_CHARS)}"`);
  assert.equal(seed("x".repeat(MAX_SEED_CHARS + 1)), "(tooLong)");
});

test("Find Definitions asks for the exact name; Find Uses for the name as a whole word", () => {
  assert.equal(definitionsQuery("RetryPolicy"), "sym:/^RetryPolicy$/");
  assert.equal(definitionsQuery("$scope"), String.raw`sym:/^\$scope$/`);
  assert.equal(usesQuery("RetryPolicy"), "ref:RetryPolicy");
  assert.equal(usesQuery("foo-bar"), 'ref:"foo-bar"');
  assert.equal(textTerm('say "hi"'), String.raw`"say \"hi\""`);
});

test("the daemon reads every seeded query as the selected text, and finds it", { skip }, async () => {
  const roots = readdirSync(WORKSPACE, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => makeRoot(join(WORKSPACE, entry.name)));
  const daemon = newTestDaemon(roots);
  await daemon.start();
  try {
    await untilIndexed(daemon);
    const cases: [selection: string, last: string][] = [
      ["RetryPolicy", "timeout"],
      ["charge failed after retries", "timeout"],
      ['"retrying charge in %.1fs"', "timeout"],
      ["except TransientError:", "timeout"],
      ["self.retry_policy.next_delay(attempt)", "/retry.*delay/"],
      ["except TransientError:\n                delay = self.retry_policy.next_delay(attempt)", "timeout"],
      ["RetryPolicy", "case:yes word:yes timeout"],
    ];
    for (const [index, [selection, last]] of cases.entries()) {
      const text = seed(selection, last);
      const { query } = await daemon.request("query/parse", { text, cursor: text.length });
      assert.deepEqual(query.diagnostics, [], `${text}: diagnostics`);
      const result = await daemon.request("search/start", { searchId: `seed${index}`, text });
      assert.ok(result.total > 0, `${text} finds the selection`);
    }
    const exact = await daemon.request("search/start", { searchId: "exact", text: seed("RetryPolicy") });
    assert.equal(exact.total, 5, "only the 5 lines that say RetryPolicy: no file names, not retry_policy.py");
    const definitions = await daemon.request("search/start", {
      searchId: "defs",
      text: definitionsQuery("RetryPolicy"),
    });
    assert.equal(definitions.total, 1, "only class RetryPolicy, not names that contain it");
    const uses = await daemon.request("search/start", { searchId: "uses", text: usesQuery("RetryPolicy") });
    assert.ok(uses.total > 0, "the lines that use RetryPolicy");
  } finally {
    await daemon.stop();
  }
});
