// Editing the query box: fix-its, completions and toggles only ever apply to
// the text they were worked out for, an input method's keys are left to it,
// and ⌘Z undoes the panel's own edits.
// @covers msg:query.changed msg:parse.result
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  fromHost,
  lastSent,
  openPanel,
  parsedQuery,
  restore,
  textNode,
  typeAndParse,
  UI_SETTINGS,
  useBrowser,
} from "./harness.mjs";

useBrowser();

const QUERY = '[data-testid="query"]';

/** "sinse:6m" with an unknown-operator error whose fix rewrites it to since:. */
const MISSPELT = parsedQuery("sinse:6m", null, {
  diagnostics: [
    {
      severity: "error",
      code: "unknown_operator",
      message: "Unknown operator sinse:",
      span: { start: 0, end: 6 },
      fixes: [{ title: "Change to since:", edits: [{ span: { start: 0, end: 6 }, newText: "since:" }] }],
    },
  ],
});

/** A panel with the real typing delay, so keystrokes reach the host only after a pause. */
async function panelWithTypingDelay(t) {
  const page = await openPanel(t);
  await page.evaluate(() => document.querySelector('[data-testid="query"]').focus());
  await fromHost(page, "state.restore", { text: "", recent: [], settings: { ...UI_SETTINGS, typingDelayMs: 300 } });
  return page;
}

/** Types `text` and answers its parse with `query` once the typing delay has passed. */
async function typeAndAnswer(page, text, query, completions = []) {
  await page.keyboard.type(text);
  await page.waitForFunction((typed) => globalThis.__sent.at(-1)?.payload.text === typed, text);
  const { seq } = await lastSent(page, "query.changed");
  await fromHost(page, "parse.result", { seq, query, completions });
}

test("⌘. after more typing waits for the new parse instead of applying the old fix to new text", async (t) => {
  const page = await panelWithTypingDelay(t);
  await typeAndAnswer(page, "sinse:6m", MISSPELT);
  await page.keyboard.press("Home");
  await page.keyboard.type("x ");
  await page.keyboard.press("Control+.");
  assert.equal(await page.inputValue(QUERY), "x sinse:6m", "the old fix's span would cut 'x sinse' instead");
  assert.equal((await lastSent(page, "query.changed")).text, "x sinse:6m", "the box is reported at once");
});

test("Tab after more typing doesn't insert the suggestion over the wrong characters", async (t) => {
  const page = await panelWithTypingDelay(t);
  const since = { label: "since:", detail: "Changed within a time window", group: "operator" };
  const insert = { title: "since:", edits: [{ span: { start: 0, end: 3 }, newText: "since:" }] };
  await typeAndAnswer(page, "sin", parsedQuery("sin", textNode("sin", 0)), [{ ...since, insert }]);
  await page.keyboard.type("ce");
  await page.keyboard.press("Tab");
  assert.equal(await page.inputValue(QUERY), "since", "not since:ce");
});

test("Show them on a note only edits the query the results are for", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "x -f:vendor/", parsedQuery("x -f:vendor/", textNode("x", 0)));
  const undo = { title: "Remove -f:vendor/", edits: [{ span: { start: 1, end: 12 }, newText: "" }] };
  const hidden = [{ reason: "pathFilter", filter: "-f:vendor/", count: 1, unit: "files", undo }];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [CODE_LINE_RESULT] });
  await fromHost(page, "search.done", { seq, searchId: "s1", total: 1, truncated: false, ms: 1, hidden });
  // The box now has an error; the results and their note are still the old query's.
  const diagnostics = [
    { severity: "error", code: "unclosed_paren", message: "(", span: { start: 0, end: 1 }, fixes: [] },
  ];
  await typeAndParse(page, "( x -f:vendor/", parsedQuery("( x -f:vendor/", null, { diagnostics }));
  await page.click('[data-testid="show-hidden"]');
  assert.equal(await page.inputValue(QUERY), "( x -f:vendor/");
});
