// The pure query edits behind fix-its, completions, the Aa / .* toggles and
// the repo menu (src/queryEdit.ts), run in Node on edge-case inputs: empty
// text, spans past either end, overlapping or unsorted edits, and astral
// characters, which take two UTF-16 code units.
import assert from "node:assert/strict";
import { test } from "node:test";
import { applyEdits, isRegexPressed, scopeToRepo, toggleCase, toggleRegex } from "./out/queryEdit.mjs";

/** A text edit replacing start..end. */
const edit = (start, end, newText) => ({ span: { start, end }, newText });

/** A ParsedQuery for `raw` with `root`; case:yes when `caseValue` says so. */
const parsed = (raw, root, caseValue = null) => ({
  version: 1,
  raw,
  root,
  mode: "workingTree",
  diagnostics: [],
  globals: { case: caseValue, count: null, type: null },
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

test("Aa removes an explicit case:yes along with one neighbouring space", () => {
  const raw = "x case:yes";
  const query = parsed(raw, { kind: "and", children: [term("x", 0), operator("case", "yes", 2)] }, "yes");
  assert.deepEqual(toggleCase(raw, query, false), { text: "x", cursor: 1 });
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
