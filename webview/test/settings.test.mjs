// The UI settings the host sends with state.restore: how long the panel waits
// after a keystroke, and whether it shows the parsed query.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fromHost,
  openPanel,
  parsedQuery,
  QUERY,
  sentMessages,
  textNode,
  UI_SETTINGS,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

test("the panel waits typingDelayMs after the last keystroke before it asks for a search", async (t) => {
  // @covers setting:typingDelayMs
  const page = await openPanel(t);
  await fromHost(page, "state.restore", { text: "", recent: [], settings: { ...UI_SETTINGS, typingDelayMs: 300 } });
  await page.fill(QUERY, "retry");
  assert.equal((await sentMessages(page, "query.changed")).length, 0, "nothing is sent while typing");
  const sent = await waitForSent(page, "query.changed", { text: "retry" });
  assert.equal(sent.text, "retry");
});

test("the parsed query is shown unless ui.showParsedQuery is off", async (t) => {
  // @covers setting:ui.showParsedQuery
  for (const showParsedQuery of [true, false]) {
    const page = await openPanel(t);
    await fromHost(page, "state.restore", { text: "", recent: [], settings: { ...UI_SETTINGS, showParsedQuery } });
    await page.fill(QUERY, "retry");
    const { seq } = await waitForSent(page, "query.changed", { text: "retry" });
    await fromHost(page, "parse.result", { seq, query: parsedQuery("retry", textNode("retry", 0)), completions: [] });
    const chips = await page.locator('[data-testid="chips"]').innerText();
    if (showParsedQuery) assert.match(chips, /retry/);
    else assert.equal(chips, "");
  }
});
