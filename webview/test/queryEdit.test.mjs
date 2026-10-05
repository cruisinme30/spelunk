// The pure query edits behind fix-its, completions, the Aa / ab / .* toggles and
// the repo menu (src/queryEdit.ts), run in Node on edge-case inputs: empty
// text, spans past either end, overlapping or unsorted edits, and astral
// characters, which take two UTF-16 code units.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  applyEdits,
  facetState,
  isCasePressed,
  isCaseSmart,
  isRegexPressed,
  isWordPressed,
  scopeToRepo,
  toggleCase,
  toggleFacet,
  toggleRegex,
  toggleWord,
} from "./out/queryEdit.mjs";

/** A text edit replacing start..end. */
const edit = (start, end, newText) => ({ span: { start, end }, newText });

/** A ParsedQuery for `raw` with `root`; case: and word: as `caseValue` and `wordValue` say. */
const parsed = (raw, root, caseValue = null, wordValue = null) => ({
  version: 1,
  raw,
  root,
  mode: "workingTree",
  diagnostics: [],
  globals: { case: caseValue, count: null, type: null, ...(wordValue ? { word: wordValue } : {}) },
  ...(/\p{Lu}/u.test(raw.replace(/case:\w+/, "")) ? { hasCapital: true } : {}),
});

/** A text term spanning `start` to `end` (default: start + value's length). */
const term = (value, start, match = "literal", end = start + value.length) => ({
  kind: "text",
  value,
  match,
  termIndex: 0,
  span: { start, end },
});

/** An operator node `name:value` starting at `start`. */
const operator = (name, value, start) => ({
  kind: "op",
  op: name,
  value,
  span: { start, end: start + name.length + 1 + value.length },
});

test("applyEdits on empty text and with no edits", () => {
  assert.deepEqual(applyEdits("", []), { text: "", cursor: 0 });
  assert.deepEqual(applyEdits("abc", []), { text: "abc", cursor: 3 });
  assert.deepEqual(applyEdits("", [edit(0, 0, "since:")]), { text: "since:", cursor: 6 });
});

test("applyEdits clips spans past either end of the text instead of slicing from the end", () => {
  assert.deepEqual(applyEdits("abc", [edit(5, 9, "X")]), { text: "abcX", cursor: 4 });
  assert.deepEqual(applyEdits("abc", [edit(-5, 1, "X")]), { text: "Xbc", cursor: 1 });
  assert.deepEqual(applyEdits("one two", [edit(-2, 4, "1 ")]), { text: "1 two", cursor: 2 });
});

test("applyEdits sorts edits, and never copies text twice when spans overlap or run backwards", () => {
  assert.deepEqual(applyEdits("hello", [edit(3, 5, "B"), edit(0, 1, "A")]), { text: "AelB", cursor: 4 });
  // The second edit lies inside the first; it moves to where the first ended, so "ll" isn't copied again.
  assert.deepEqual(applyEdits("hello", [edit(0, 4, "Z"), edit(1, 2, "Y")]), { text: "ZYo", cursor: 2 });
  assert.deepEqual(applyEdits("abc", [edit(2, 1, "X")]), { text: "abXc", cursor: 3 });
  assert.deepEqual(applyEdits("abc", [edit(1, 1, "X"), edit(1, 1, "Y")]), { text: "aXYbc", cursor: 3 });
});

test("applyEdits skips an edit whose span isn't a number", () => {
  assert.deepEqual(applyEdits("abc", [edit(Number.NaN, 1, "X")]), { text: "abc", cursor: 3 });
  assert.deepEqual(applyEdits("abc", [edit(1, Number.POSITIVE_INFINITY, "X"), edit(0, 1, "Y")]), {
    text: "Ybc",
    cursor: 1,
  });
});

test("applyEdits counts astral characters as two UTF-16 code units", () => {
  // "😀" is 2 units, so "b" is at 3.
  assert.deepEqual(applyEdits("a😀b", [edit(1, 3, "E")]), { text: "aEb", cursor: 2 });
  assert.deepEqual(applyEdits("😀😀", [edit(2, 4, "x")]), { text: "😀x", cursor: 3 });
});

test("Aa on an empty box writes case:yes (or case:no) with the cursor inside the text", () => {
  assert.deepEqual(toggleCase("", undefined, "off"), { text: "case:yes", cursor: 8 });
  assert.deepEqual(toggleCase("", undefined, "on"), { text: "case:no", cursor: 7 });
  assert.deepEqual(toggleCase("😀", undefined, "off"), { text: "case:yes 😀", cursor: 9 });
});

test("Aa removes an explicit case:yes along with one neighbouring space", () => {
  const raw = "x case:yes";
  const query = parsed(raw, { kind: "and", children: [term("x", 0), operator("case", "yes", 2)] }, "yes");
  assert.deepEqual(toggleCase(raw, query, "off"), { text: "x", cursor: 1 });
});

test("under smart case, Aa shows whether the query's capitals made case match, and flips it", () => {
  const lower = parsed("retry", term("retry", 0));
  const upper = parsed("Retry", term("Retry", 0));
  assert.equal(isCaseSmart(lower, "smart"), true);
  assert.equal(isCasePressed(lower, "smart"), false);
  assert.equal(isCasePressed(upper, "smart"), true);
  assert.deepEqual(toggleCase("retry", lower, "smart"), { text: "case:yes retry", cursor: 9 });
  assert.deepEqual(toggleCase("Retry", upper, "smart"), { text: "case:no Retry", cursor: 8 });

  const raw = "case:smart Retry";
  const query = parsed(raw, { kind: "and", children: [operator("case", "smart", 0), term("Retry", 11)] }, "smart");
  assert.equal(isCaseSmart(query, "off"), true, "case:smart wins over the setting");
  assert.equal(isCasePressed(query, "off"), true);
  assert.deepEqual(toggleCase(raw, query, "off"), { text: "Retry", cursor: 0 }, "removing it ignores case again");
  assert.deepEqual(toggleCase(raw, query, "smart"), { text: "case:no Retry", cursor: 7 });
  const explicit = parsed(
    "case:no Retry",
    { kind: "and", children: [operator("case", "no", 0), term("Retry", 8)] },
    "no",
  );
  assert.equal(isCaseSmart(explicit, "smart"), false, "an explicit case:no isn't smart");
  assert.deepEqual(toggleCase("case:no Retry", explicit, "smart"), { text: "Retry", cursor: 0 });
});

test("ab writes word:yes, or word:no when whole words are the default", () => {
  assert.deepEqual(toggleWord("retry", undefined, false), { text: "word:yes retry", cursor: 9 });
  assert.deepEqual(toggleWord("retry", undefined, true), { text: "word:no retry", cursor: 8 });
});

test("ab reflects word: over the setting, and removes or flips it", () => {
  const raw = "x word:yes";
  const query = parsed(raw, { kind: "and", children: [term("x", 0), operator("word", "yes", 2)] }, null, "yes");
  assert.equal(isWordPressed(query, false), true);
  assert.equal(isWordPressed(parsed("x", term("x", 0)), true), true, "the setting when the query doesn't say");
  assert.deepEqual(toggleWord(raw, query, false), { text: "x", cursor: 1 });
  assert.deepEqual(toggleWord(raw, query, true), { text: "x word:no", cursor: 9 });
});

test(".* on a box with no terms leaves it alone", () => {
  assert.deepEqual(toggleRegex(""), { text: "", cursor: 0 });
  assert.deepEqual(toggleRegex("f:x", parsed("f:x", operator("f", "x", 0))), { text: "f:x", cursor: 3 });
  assert.equal(isRegexPressed(), false);
});

test(".* round-trips a term with astral characters and regex metacharacters", () => {
  const literal = "😀.x";
  const wrapped = toggleRegex(literal, parsed(literal, term(literal, 0)));
  assert.deepEqual(wrapped, { text: String.raw`/😀\.x/`, cursor: 7 });
  const regexTerm = term(String.raw`😀\.x`, 0, "regex", wrapped.text.length);
  assert.deepEqual(toggleRegex(wrapped.text, parsed(wrapped.text, regexTerm)), { text: literal, cursor: 4 });
});

test(".* turns a regex with a space back into a quoted phrase", () => {
  assert.deepEqual(toggleRegex("/a b/", parsed("/a b/", term("a b", 0, "regex", 5))), { text: '"a b"', cursor: 5 });
});

test("the repo menu writes a repo: scope into an empty box and escapes the name", () => {
  assert.deepEqual(scopeToRepo(""), { text: "", cursor: 0 });
  assert.deepEqual(scopeToRepo("", undefined, "a.b(c)"), {
    text: String.raw`repo:a\.b\(c\)`,
    cursor: 14,
  });
  assert.deepEqual(scopeToRepo("repo:x", parsed("repo:x", operator("repo", "x", 0)), "😀"), {
    text: "repo:😀",
    cursor: 7,
  });
});

test("the repo menu ignores a repo: span past the end of the text", () => {
  const query = parsed("t", { kind: "and", children: [term("t", 0), operator("repo", "x", 50)] });
  assert.deepEqual(scopeToRepo("t", query), { text: "t", cursor: 1 });
});

/** A NOT of `child`, whose span starts one character earlier, at the -. */
const not = (child) => ({ kind: "not", child, span: { start: child.span.start - 1, end: child.span.end } });

test("a facet click adds its filter at the end, and Alt-click the filter that leaves it out", () => {
  // @covers screen:result-facets
  const query = parsed("retry", term("retry", 0));
  assert.deepEqual(toggleFacet("retry", query, "repo:api", false), { text: "retry repo:api", cursor: 14 });
  assert.deepEqual(toggleFacet("retry ", query, "repo:api", true), { text: "retry -repo:api", cursor: 15 });
  // A bucket of several filters is left out as a group.
  const month = "since:2026-09 until:2026-09";
  assert.deepEqual(toggleFacet("retry", query, month, true).text, `retry -(${month})`);
  assert.deepEqual(toggleFacet("", undefined, `author:"Jane Doe"`, false), {
    text: `author:"Jane Doe"`,
    cursor: 17,
  });
});

test("a facet bucket the query keeps or leaves out shows so, and a click takes it back out", () => {
  // @covers screen:result-facets
  const kept = parsed("retry repo:api", { kind: "and", children: [term("retry", 0), operator("repo", "api", 6)] });
  assert.equal(facetState(kept, "repo:api"), "kept");
  assert.equal(facetState(kept, "repo:web"), "none");
  assert.deepEqual(toggleFacet("retry repo:api", kept, "repo:api", true), { text: "retry", cursor: 5 });

  const leftOut = parsed("-repo:api retry", {
    kind: "and",
    children: [not(operator("repo", "api", 1)), term("retry", 10)],
  });
  assert.equal(facetState(leftOut, "repo:api"), "left-out");
  assert.deepEqual(toggleFacet("-repo:api retry", leftOut, "repo:api", false), { text: "retry", cursor: 5 });

  // Each of a month's two filters must be in the query for the month to be kept.
  const raw = "since:2026-09 x until:2026-09";
  const month = parsed(raw, {
    kind: "and",
    children: [operator("since", "2026-09", 0), term("x", 14), operator("until", "2026-09", 16)],
  });
  assert.equal(facetState(month, "since:2026-09 until:2026-09"), "kept");
  assert.equal(facetState(month, "since:2026-08 until:2026-08"), "none");
  assert.deepEqual(toggleFacet(raw, month, "since:2026-09 until:2026-09", false), { text: "x", cursor: 1 });
  // Inside an OR, a filter doesn't scope the whole query.
  const either = parsed("repo:api OR x", {
    kind: "or",
    children: [operator("repo", "api", 0), term("x", 12)],
    span: { start: 0, end: 13 },
  });
  assert.equal(facetState(either, "repo:api"), "none");
});
