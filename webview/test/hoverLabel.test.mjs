// Hover labels: resting the mouse on a control shows its name, quickly, in a
// label the panel draws itself, since VS Code's webviews show no native
// tooltips.
import assert from "node:assert/strict";
import { test } from "node:test";
import { openPanel, pageErrors, useBrowser } from "./harness.mjs";

useBrowser();

const LABEL = '[data-testid="hover-label"]';

/** The hover label's text, or undefined while it's hidden. */
const shownLabel = (page) =>
  page.evaluate((selector) => {
    const label = document.querySelector(selector);
    return label && !label.hidden ? label.textContent : undefined;
  }, LABEL);

// @covers screen:hover-labels
test("resting on an icon shows its name within half a second, and leaving hides it", async (t) => {
  const page = await openPanel(t);
  await page.hover('[data-testid="toggle-replace"]');
  assert.equal(await shownLabel(page), undefined, "not at once: a passing mouse shows nothing");
  await page.waitForSelector(`${LABEL}:not([hidden])`, { timeout: 500 });
  assert.equal(await shownLabel(page), "Replace in current files (⌥⌘F)");
  // No native tooltip on top of it: the title is set aside while the mouse is there.
  assert.equal(await page.getAttribute('[data-testid="toggle-replace"]', "title"), null);
  await page.mouse.move(0, 400);
  assert.equal(await shownLabel(page), undefined);
  assert.equal(await page.getAttribute('[data-testid="toggle-replace"]', "title"), "Replace in current files (⌥⌘F)");
  assert.deepEqual(pageErrors(page), []);
});

// @covers screen:hover-labels
test("moving from one icon straight to the next shows the next name at once", async (t) => {
  const page = await openPanel(t);
  await page.hover('[data-testid="toggle-case"]');
  await page.waitForSelector(`${LABEL}:not([hidden])`);
  await page.hover('[data-testid="toggle-word"]');
  assert.equal(await shownLabel(page), "Match whole word (word:yes)");
});

test("the two icons at the start of the boxes have names too", async (t) => {
  const page = await openPanel(t);
  await page.hover(".search-bar > .query-icon");
  await page.waitForSelector(`${LABEL}:not([hidden])`);
  assert.equal(await shownLabel(page), "Search");
  await page.click('[data-testid="toggle-replace"]');
  await page.hover(".replace-row .query-icon");
  await page.waitForFunction((selector) => document.querySelector(selector)?.textContent === "Replace with", LABEL);
});

test("a click hides the label, and a name the click changed is the one kept", async (t) => {
  const page = await openPanel(t);
  await page.hover('[data-testid="toggle-replace"]');
  await page.waitForSelector(`${LABEL}:not([hidden])`);
  await page.click('[data-testid="toggle-replace"]');
  assert.equal(await shownLabel(page), undefined);
  // The panel renames a control while the mouse is on it: the new name survives the mouse leaving.
  await page.hover('[data-testid="toggle-case"]');
  await page.evaluate(() => document.querySelector('[data-testid="toggle-case"]').setAttribute("title", "Renamed"));
  await page.mouse.move(0, 400);
  assert.equal(await page.getAttribute('[data-testid="toggle-case"]', "title"), "Renamed");
});

test("near the panel's edge the label stays inside it", async (t) => {
  const page = await openPanel(t);
  await page.setViewportSize({ width: 420, height: 400 });
  await page.hover('[data-testid="open-settings"]');
  await page.waitForSelector(`${LABEL}:not([hidden])`);
  const box = await page.locator(LABEL).boundingBox();
  assert.ok(box.x >= 0 && box.x + box.width <= 420, `label at ${JSON.stringify(box)}`);
});
