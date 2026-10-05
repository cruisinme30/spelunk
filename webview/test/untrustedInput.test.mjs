// The search panel and the welcome page given hostile or broken host
// messages: markup in every string the daemon passes through (paths, code,
// commit messages, names), offsets that overlap, run backwards or past the
// text, malformed payloads, late messages of an old search, a lot of results,
// and a corrupt saved state. Nothing may run, throw or go missing.
// @covers msg:search.batch msg:search.done msg:parse.result msg:preview.result msg:index.status msg:state.restore
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fromHost,
  openPanel,
  pageErrors,
  parsedQuery,
  restore,
  textNode,
  typeAndParse,
  useBrowser,
} from "./harness.mjs";

useBrowser();

/** A code-line result with `text` and `hits`. */
const codeLine = (ref, text, hits, extra = {}) => ({
  kind: "line",
  ref,
  repoId: "r1",
  path: "src/a.py",
  line: 1,
  text,
  hits,
  ...extra,
});

/** A panel searching "retry" as seq `seq` (returned), with no results yet. */
async function searchingPanel(t) {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  return { page, seq };
}

test("a diagnostic span before or past the text marks its end instead of throwing", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  for (const span of [
    { start: -3, end: 1e9 },
    { start: 2e9, end: 2e9 },
  ]) {
    const diagnostics = [{ severity: "error", code: "odd_span", message: "Odd", span, fixes: [] }];
    await typeAndParse(page, "abc", parsedQuery("abc", null, { diagnostics }));
    assert.equal(await page.locator('[data-code="odd_span"]').count(), 1, JSON.stringify(span));
  }
  assert.deepEqual(pageErrors(page), []);
});

/** The highlighted runs of row `ref`'s code: [text, class] per <mark>, and the whole text. */
const marksOf = (page, ref) =>
  page.locator(`[data-ref="${ref}"] code.text`).evaluate((code) => ({
    text: code.textContent,
    marks: [...code.querySelectorAll("mark")].map((mark) => [mark.textContent, mark.className]),
  }));

test("hits that overlap, run backwards, fall outside the line or aren't numbers keep every character", async (t) => {
  const { page, seq } = await searchingPanel(t);
  const hits = [
    { start: 8, end: 2, termIndex: 0 }, // backwards
    { start: -4, end: 3, termIndex: -1 }, // starts before the line; a negative term
    { start: 2, end: 6, termIndex: 1 }, // overlaps the one before
    { start: 5, end: 5, termIndex: 2 }, // empty
    { start: 100, end: 200, termIndex: 3 }, // past the end
    { start: Number.NaN, end: 9, termIndex: 0 },
    { start: 7, end: 9, termIndex: 1.5 },
  ];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [codeLine("l1", "hello world", hits)] });
  assert.deepEqual(await marksOf(page, "l1"), {
    text: "hello world",
    marks: [
      ["hel", "term-color-3"],
      ["lo ", "term-color-1"],
      ["or", "term-color-0"],
    ],
  });
});

test("a hit never splits an emoji in half", async (t) => {
  const { page, seq } = await searchingPanel(t);
  // "😀" is two UTF-16 units: hits end and start in its middle.
  const items = [codeLine("l1", "a😀b😀c", [{ start: 2, end: 4, termIndex: 0 }])];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  assert.deepEqual(await marksOf(page, "l1"), { text: "a😀b😀c", marks: [["😀b", "term-color-0"]] });
});

test("tabs, control and right-to-left override characters in a line render as text", async (t) => {
  const { page, seq } = await searchingPanel(t);
  const text = "x\t\u0000‮evil‬ retry";
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [codeLine("l1", text, [{ start: 10, end: 15 }])],
  });
  assert.deepEqual(await marksOf(page, "l1"), { text, marks: [["retry", "term-color-0"]] });
});
