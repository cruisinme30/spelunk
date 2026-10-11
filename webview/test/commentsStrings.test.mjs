// in: keeps the matches in comments, in quoted text or in code: its chip
// says which in plain words, and its note under the results offers to
// show what it hid.
import assert from "node:assert/strict";
import { test } from "node:test";
import { openPanel, parsedQuery, QUERY, restore, searchDone, textNode, typeAndParse, useBrowser } from "./harness.mjs";

useBrowser();

/** "retry <in>" parsed, with in: (or -in:) at the top level after retry. */
function inQuery(text, value, negated) {
  const start = text.indexOf(negated ? "-in:" : "in:");
  const op = { kind: "op", op: "in", value, span: { start: start + (negated ? 1 : 0), end: text.length } };
  const inNode = negated ? { kind: "not", child: op, span: { start, end: text.length } } : op;
  return parsedQuery(text, {
    kind: "and",
    children: [textNode("retry", 0), inNode],
    span: { start: 0, end: text.length },
  });
}

test("the in: chip says where matches are, in plain words", async (t) => {
  // @covers op:in screen:comments-strings
  const page = await openPanel(t);
  await restore(page);
  for (const [text, value, negated, label] of [
    ["retry in:comments", "comments", false, "comments"],
    ["retry in:strings", "strings", false, "quoted text"],
    ["retry in:code", "code", false, "code only"],
    ["retry -in:comments", "comments", true, "comments"],
  ]) {
    const seq = await typeAndParse(page, text, inQuery(text, value, negated));
    await searchDone(page, seq, { total: 1 });
    const chip = page.locator('[data-testid="chips"] .chip.tone-match');
    assert.equal(await chip.locator(".chip-label").textContent(), "in", text);
    assert.equal(await chip.locator("code").textContent(), label, text);
    const chips = await page.locator('[data-testid="chips"]').innerText();
    assert.equal(chips.includes("NOT"), negated, `${text}: ${chips}`);
  }
});

test("in:'s note counts what it hid, and Show them takes it out", async (t) => {
  // @covers op:in screen:comments-strings
  const page = await openPanel(t);
  await restore(page);
  const text = "retry in:comments";
  const seq = await typeAndParse(page, text, inQuery(text, "comments", false));
  const undo = { title: "Show them", edits: [{ span: { start: 5, end: 17 }, newText: "" }] };
  await searchDone(page, seq, {
    total: 5,
    hidden: [{ reason: "in", filter: text.slice(6), count: 24, unit: "matches", undo }],
  });
  const note = page.locator('[data-testid="hidden-notes"] .note');
  assert.equal(await note.locator("span").first().innerText(), "24 matches hidden by in:comments");
  await note.locator('[data-testid="show-hidden"]').click();
  assert.equal(await page.inputValue(QUERY), "retry");
});
