// A query seeded from the editor: the box types it over the last query, so
// ⌘Z brings that back, and selects it, so typing replaces it. A restored
// query, as before, goes in with the cursor at its end.
// @covers screen:seed-from-editor
import assert from "node:assert/strict";
import { test } from "node:test";
import { fromHost, openPanel, QUERY, UI_SETTINGS, useBrowser, waitForSent } from "./harness.mjs";

useBrowser();

/** Sends a state.restore with this text, seeded or not. */
const restoreText = (page, text, seeded) =>
  fromHost(page, "state.restore", {
    text,
    ...(seeded ? { seeded } : {}),
    recent: [],
    pinned: [],
    settings: UI_SETTINGS,
  });

/** The box's selection, as [start, end]. */
const selection = (page) => page.locator(QUERY).evaluate((input) => [input.selectionStart, input.selectionEnd]);

test("a seeded query replaces the box's text, selected, and ⌘Z brings the last query back", async (t) => {
  const page = await openPanel(t);
  await restoreText(page, "timeout", false);
  await waitForSent(page, "query.changed", { text: "timeout" });
  assert.deepEqual(await selection(page), [7, 7], "a restored query: the cursor at its end");

  await restoreText(page, "RetryPolicy", true);
  assert.equal(await page.inputValue(QUERY), "RetryPolicy");
  assert.deepEqual(await selection(page), [0, 11], "selected, so typing replaces it");
  await waitForSent(page, "query.changed", { text: "RetryPolicy" });

  await page.locator(QUERY).press("ControlOrMeta+z");
  assert.equal(await page.inputValue(QUERY), "timeout", "⌘Z brings back what it replaced");
  await waitForSent(page, "query.changed", { text: "timeout" });
});

test("typing over a seeded query replaces it", async (t) => {
  const page = await openPanel(t);
  await restoreText(page, "timeout", false);
  await restoreText(page, "RetryPolicy", true);
  await page.keyboard.type("backoff");
  assert.equal(await page.inputValue(QUERY), "backoff");
});

test("a seeded query that is already in the box changes nothing", async (t) => {
  const page = await openPanel(t);
  await restoreText(page, "RetryPolicy", false);
  await restoreText(page, "RetryPolicy", true);
  assert.deepEqual(await selection(page), [11, 11]);
});
