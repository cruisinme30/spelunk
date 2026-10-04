// The welcome page: the shortcut choice, what to index, and each repo's
// indexing.
// @covers msg:welcome.state msg:welcome.choose msg:welcome.shortcut msg:welcome.start
import assert from "node:assert/strict";
import { test } from "node:test";
import { fromHost, lastSent, openWelcome, sentMessages, useBrowser } from "./harness.mjs";

useBrowser();

const SETTINGS = {
  preset: "quickOpen",
  historyDepth: "2y",
  symbols: true,
  mac: true,
};
const REPOS = [
  { repoId: "a", name: "payments-api", tree: "ready", history: "ready" },
  {
    repoId: "w",
    name: "web-checkout",
    tree: "ready",
    history: "indexing",
    progress: 0.64,
  },
  { repoId: "s", name: "shared-libs", tree: "queued", history: "queued" },
];

/** A welcome page that has received SETTINGS and REPOS. */
async function welcomePage(t, settings = SETTINGS) {
  const page = await openWelcome(t);
  assert.equal((await sentMessages(page, "ready")).length, 1);
  await fromHost(page, "welcome.state", settings);
  await fromHost(page, "index.status", { repos: REPOS });
  return page;
}

test("the welcome page offers the shortcuts and shows each repo's indexing", async (t) => {
  // @covers screen:first-run
  const page = await welcomePage(t);
  const cards = page.locator(".choice");
  assert.equal(await cards.count(), 3);
  assert.match(await cards.nth(0).innerText(), /Take over\s*⌘P\s*Recommended/);
  assert.match(await cards.nth(0).innerText(), /Quick Open moves to ⌥⌘P/);
  assert.match(await cards.nth(1).innerText(), /Take over\s*⇧⌘F/);
  assert.equal(await page.isChecked('[data-testid="shortcut"][value="quickOpen"]'), true);

  const rows = page.locator('[data-testid="welcome-repo"]');
  assert.match(await rows.nth(0).innerText(), /payments-api\s*Files, symbols and history ready\s*Ready/);
  assert.match(await rows.nth(1).innerText(), /web-checkout\s*History 64%\s*Indexing/);
  assert.match(await rows.nth(2).innerText(), /shared-libs\s*Starts when web-checkout finishes\s*Queued/);
  assert.match(await page.locator('[data-testid="start"]').innerText(), /Start searching\s*⌘P/);
});

test("each choice is sent to the host, which saves it in Settings", async (t) => {
  const page = await welcomePage(t);
  await page.check('[data-testid="shortcut"][value="findInFiles"]');
  assert.deepEqual(await lastSent(page, "welcome.choose"), {
    preset: "findInFiles",
  });
  await page.check('[data-testid="shortcut"][value="custom"]');
  assert.deepEqual(await lastSent(page, "welcome.shortcut"), {});
  await page.selectOption('[data-testid="history-depth"]', "6m");
  assert.deepEqual(await lastSent(page, "welcome.choose"), {
    historyDepth: "6m",
  });
  await page.uncheck('[data-testid="symbols"]');
  assert.deepEqual(await lastSent(page, "welcome.choose"), { symbols: false });
  await page.click('[data-testid="start"]');
  assert.deepEqual(await lastSent(page, "welcome.start"), {});
  await page.click('[data-testid="open-help"]');
  assert.deepEqual(await lastSent(page, "help.open"), {});
});

test("off a Mac the keys are written with Ctrl and Alt", async (t) => {
  const page = await welcomePage(t, {
    ...SETTINGS,
    preset: "findInFiles",
    mac: false,
  });
  assert.match(await page.locator(".choice").nth(0).innerText(), /Take over\s*Ctrl\+P/);
  assert.equal(await page.isChecked('[data-testid="shortcut"][value="findInFiles"]'), true);
  assert.match(await page.locator('[data-testid="start"]').innerText(), /Ctrl\+Shift\+F/);
});
