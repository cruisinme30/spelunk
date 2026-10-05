// The search panel and the welcome page given hostile or broken host
// messages: markup in every string the daemon passes through (paths, code,
// commit messages, names), offsets that overlap, run backwards or past the
// text, malformed payloads, late messages of an old search, a lot of results,
// and a corrupt saved state. Nothing may run, throw or go missing.
// @covers msg:search.batch msg:search.done msg:parse.result msg:preview.result msg:index.status msg:state.restore
import assert from "node:assert/strict";
import { test } from "node:test";
import { openPanel, pageErrors, parsedQuery, restore, typeAndParse, useBrowser } from "./harness.mjs";

useBrowser();

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
