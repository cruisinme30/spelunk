// Code as the editor draws it: the theme's syntax colors the host sends
// (syntax.lines) color results, context lines and the preview, matches
// included, and the preview marks whitespace as editor.renderWhitespace says.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  fromHost,
  openPanel,
  pageErrors,
  parsedQuery,
  restore,
  searchDone,
  textNode,
  typeAndParse,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

const PATH = CODE_LINE_RESULT.path;
/** The theme's colors by index: 1 a keyword's, 2 a string's, 3 a comment's. */
const COLORS = ["", "#c586c0", "#ce9178", "#6a9955"];
const ITALIC = 512;

/** `return "a"  # b`, indented with a tab: a keyword, a string and a comment. */
const LINE = '\treturn "a"  # b';
const TOKENS = [0, 0, 1, 1, 7, 0, 8, 2, 11, 0, 13, 3 + ITALIC];

/** A code result for `LINE`, its "return" matched, with a context line above. */
const RESULT = {
  ...CODE_LINE_RESULT,
  line: 8,
  text: LINE,
  hits: [{ start: 1, end: 7, termIndex: 0 }],
  contextBefore: ["\tif x:"],
};

/** A panel showing RESULT for "return", with `settings`, its preview open. */
async function panelWithCode(t, settings = {}) {
  const page = await openPanel(t);
  await restore(page, [], { contextLines: 1, ...settings });
  const seq = await typeAndParse(page, "return", parsedQuery("return", textNode("return", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [RESULT] });
  await searchDone(page, seq, { total: 1 });
  await waitForSent(page, "result.select", { ref: RESULT.ref });
  const preview = {
    kind: "file",
    path: PATH,
    firstLine: 7,
    lines: ["\tif x:", LINE, "\tpass   "],
    focusLine: 8,
    hits: [{ line: 8, ranges: [{ start: 1, end: 7, termIndex: 0 }] }],
    dirtyLines: [],
  };
  await fromHost(page, "preview.result", { ref: RESULT.ref, preview });
  return page;
}

/** The colored runs of `code`, as [text, color, italic?]. */
const runs = (locator) =>
  locator.evaluate((code) =>
    [...code.querySelectorAll("span.syn")].map((span) => [
      span.textContent,
      getComputedStyle(span).color,
      getComputedStyle(span).fontStyle === "italic",
    ]),
  );

const RESULT_TEXT = `[data-ref="${RESULT.ref}"] code.text`;
const PREVIEW_LINE = '[data-testid="preview"] .code-line[data-line="8"] code';

test("code is drawn plain until its colors come, then in the theme's colors, matches kept", async (t) => {
  // @covers msg:syntax.lines setting:ui.syntaxHighlighting
  const page = await panelWithCode(t, { renderWhitespace: "none" });
  assert.deepEqual(await runs(page.locator(RESULT_TEXT)), [], "no colors yet");
  await fromHost(page, "syntax.lines", {
    colors: COLORS,
    lines: [
      { path: PATH, text: LINE, tokens: TOKENS },
      { path: PATH, text: "\tif x:", tokens: [0, 0, 1, 1, 3, 0] },
    ],
  });
  const purple = "rgb(197, 134, 192)";
  const orange = "rgb(206, 145, 120)";
  const green = "rgb(106, 153, 85)";
  // The row drops the tab, so its tokens shift by one; "return" is also the match, so it's colored inside its mark.
  assert.deepEqual(await runs(page.locator(RESULT_TEXT)), [
    ["return", purple, false],
    ['"a"', orange, false],
    ["# b", green, true],
  ]);
  assert.equal(await page.locator(`${RESULT_TEXT} mark .syn`).textContent(), "return");
  assert.equal(await page.locator(RESULT_TEXT).textContent(), 'return "a"  # b', "the text itself is unchanged");
  assert.deepEqual(await runs(page.locator(".context-line code.text")), [["if", purple, false]]);
  assert.deepEqual(await runs(page.locator(PREVIEW_LINE)), [
    ["return", purple, false],
    ['"a"', orange, false],
    ["# b", green, true],
  ]);

  // A theme change sends the colors again from scratch; with syntax colors off it sends none.
  await fromHost(page, "syntax.lines", { colors: [], lines: [], reset: true });
  assert.deepEqual(await runs(page.locator(RESULT_TEXT)), []);
  assert.equal(await page.locator(RESULT_TEXT).textContent(), 'return "a"  # b');
  assert.deepEqual(pageErrors(page), []);
});

test("code drawn again is colored again, and a line that shows a replace stays plain", async (t) => {
  const page = await panelWithCode(t);
  await fromHost(page, "syntax.lines", { colors: COLORS, lines: [{ path: PATH, text: LINE, tokens: TOKENS }] });
  await page.locator(RESULT_TEXT).evaluate((code) => code.replaceChildren('return "a"  # b'));
  await page.waitForFunction((selector) => document.querySelector(selector).querySelector(".syn"), RESULT_TEXT);
  assert.equal((await runs(page.locator(RESULT_TEXT))).length, 3);
  await page.locator(RESULT_TEXT).evaluate((code) => {
    const del = document.createElement("del");
    del.textContent = "return";
    const ins = document.createElement("ins");
    ins.textContent = "yield";
    code.replaceChildren(del, ins, ' "a"  # b');
  });
  await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 0)));
  assert.deepEqual(await runs(page.locator(RESULT_TEXT)), []);
});

/** The preview's whitespace marks, line by line: "·" a space, "→" a tab, "_" anything else. */
const marks = (page) =>
  page.locator('[data-testid="preview"] .code-line code').evaluateAll((codes) =>
    codes.map((code) => {
      let out = "";
      const walker = document.createTreeWalker(code, NodeFilter.SHOW_TEXT);
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const mark = node.parentElement.closest(".ws");
        out += mark
          ? (mark.classList.contains("ws-tab") ? "→" : "·").repeat(node.data.length)
          : "_".repeat(node.data.length);
      }
      return out;
    }),
  );

test("the preview marks whitespace as editor.renderWhitespace says", async (t) => {
  // @covers setting:ui.renderWhitespace
  const cases = {
    none: ["______", "________________", "________"],
    // As in a selection: the matched line's whitespace only.
    selection: ["______", "→______·___··_·_", "________"],
    boundary: ["→_____", "→__________··___", "→____···"],
    trailing: ["______", "________________", "_____···"],
    all: ["→__·__", "→______·___··_·_", "→____···"],
  };
  for (const [renderWhitespace, expected] of Object.entries(cases)) {
    const page = await panelWithCode(t, { renderWhitespace });
    assert.deepEqual(await marks(page), expected, renderWhitespace);
    await page.close();
  }
});

test("whitespace marks draw over their character: the text keeps its width and copies as written", async (t) => {
  const page = await panelWithCode(t, { renderWhitespace: "all", tabSize: 2 });
  const line = page.locator('[data-testid="preview"] .code-line[data-line="9"] code');
  assert.equal(await line.textContent(), "\tpass   ");
  const [dot, arrow] = await Promise.all([
    line
      .locator(".ws-space")
      .first()
      .evaluate((mark) => getComputedStyle(mark, "::before").content),
    line.locator(".ws-tab").evaluate((mark) => getComputedStyle(mark, "::before").content),
  ]);
  assert.deepEqual([dot, arrow], ['"·"', '"→"']);
  assert.equal(await line.evaluate((code) => getComputedStyle(code).tabSize), "2", "editor.tabSize");
});
