// Search in Folder: the scope it writes is typed into the box with the
// cursor after it, so typing adds the search, and ⌘Z brings back the query
// it replaced; notes under the results say what a picked folder can't give.
// @covers screen:search-in-folder
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fromHost,
  lastSent,
  openPanel,
  parsedQuery,
  QUERY,
  restore,
  searchDone,
  textNode,
  typeAndParse,
  UI_SETTINGS,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

/** The box's selection, as [start, end]. */
const selection = (page) => page.locator(QUERY).evaluate((input) => [input.selectionStart, input.selectionEnd]);

test("a folder's scope goes in with the cursor at its end, typing adds to it, and ⌘Z brings the last query back", async (t) => {
  const page = await openPanel(t);
  await fromHost(page, "state.restore", { text: "timeout", recent: [], pinned: [], settings: UI_SETTINGS });
  await waitForSent(page, "query.changed", { text: "timeout" });
  const scoped = "repo:^web-checkout$ f:^src/api/ timeout";
  await fromHost(page, "state.restore", {
    text: scoped,
    seeded: true,
    keepTyping: true,
    recent: ["timeout"],
    pinned: [],
    settings: UI_SETTINGS,
  });
  assert.equal(await page.inputValue(QUERY), scoped);
  assert.deepEqual(await selection(page), [scoped.length, scoped.length], "nothing selected, the cursor at the end");
  await waitForSent(page, "query.changed", { text: scoped });
  await page.keyboard.type(" retry");
  assert.equal(await page.inputValue(QUERY), `${scoped} retry`, "typing adds to the scope");
  await page.locator(QUERY).press("ControlOrMeta+z");
  await page.locator(QUERY).press("ControlOrMeta+z");
  assert.equal(await page.inputValue(QUERY), "timeout", "⌘Z brings back the query the scope replaced");
});

test("a skipped folder's note says why, with the setting that would change it, and No results points to it", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const text = "repo:^web-checkout$ f:^dist/ retry";
  const seq = await typeAndParse(page, text, parsedQuery(text, textNode("retry", 29)));
  await searchDone(page, seq, {
    folderNotes: [
      { folder: "web-checkout/dist/", skipped: "ignored" },
      { folder: "web-checkout/linked/", skipped: "link" },
    ],
  });
  const notes = page.locator('[data-testid="folder-note"]');
  assert.deepEqual(await notes.locator("span").allInnerTexts(), [
    "web-checkout/dist/ isn't searched: .gitignore leaves it out",
    "web-checkout/linked/ isn't searched: it's a link to another folder, and Spelunk doesn't follow links",
  ]);
  assert.equal(await notes.nth(1).locator("button").count(), 0, "nothing to change for a link");
  assert.match(await page.locator('[data-testid="no-results"]').innerText(), /That folder isn't searched; see below/);
  await notes.first().locator('[data-testid="folder-note-setting"]').click();
  assert.deepEqual(await lastSent(page, "settings.open"), { setting: "spelunk.index.includeIgnored" });
});

test("in history, a folder in a Git repo of its own says its commits aren't searched", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const note = { folder: "plugins/stripe-adapter/", gitRepo: "plugins/stripe-adapter/" };
  const files = "f:^plugins/stripe-adapter/ retry";
  const first = await typeAndParse(page, files, parsedQuery(files, textNode("retry", 27)));
  await searchDone(page, first, { folderNotes: [note] });
  assert.equal(await page.locator('[data-testid="folder-note"]').count(), 0, "its files are searched: no note");
  const text = "f:^plugins/stripe-adapter/ author:ana";
  const seq = await typeAndParse(page, text, parsedQuery(text, textNode("x", 0), { mode: "history" }));
  await searchDone(page, seq, { folderNotes: [note] });
  assert.equal(
    await page.locator('[data-testid="folder-note"] span').innerText(),
    "plugins/stripe-adapter/ is a Git repo of its own: its files are searched, but not its commits",
  );
  assert.match(
    await page.locator('[data-testid="no-results"]').innerText(),
    /That folder's commits aren't searched; see below/,
  );
});

test("a search with no folder notes shows none", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  await searchDone(page, seq);
  assert.equal(await page.locator('[data-testid="folder-note"]').count(), 0);
});
