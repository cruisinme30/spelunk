// The order control in each results heading: a menu of best match, path
// order and recently changed, an arrow for the direction, and the times a
// recently-changed search shows for each file.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  FILE_NAME_RESULT,
  QUERY,
  fromHost,
  lastSent,
  openPanel,
  parsedQuery,
  restore,
  searchDone,
  sentMessages,
  textNode,
  typeAndParse,
  useBrowser,
} from "./harness.mjs";

useBrowser();

const HOURS = 3600 * 1000;

/** A code result on `line` of `path`, with `fields` over it. */
const codeLine = (path, line, fields) => ({ ...CODE_LINE_RESULT, ref: `${path}:${line}`, path, line, ...fields });

/** A panel with the Order setting at `order`, showing a file name and a code line for `text` parsed as `query`. */
async function panelWithOrder(t, order, { text = "retry_policy", query, items } = {}) {
  const page = await openPanel(t);
  await restore(page, [], { order });
  const parsed = query ?? parsedQuery(text, textNode(text, 0));
  const seq = await typeAndParse(page, text, parsed);
  const shown = items ?? [FILE_NAME_RESULT, CODE_LINE_RESULT];
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: shown });
  await searchDone(page, seq, { total: shown.length });
  return page;
}

/** What the order control of `section` says: its button, then its arrow. */
const orderControl = (page, section = "section-code") =>
  page
    .locator(`[data-testid="${section}"] .order-control`)
    .evaluate((control) => [
      control.querySelector('[data-testid="order-button"]').textContent,
      control.querySelector('[data-testid="order-direction"]').textContent,
    ]);

/** The open menu's choices: their names, and which one is checked or unavailable. */
const menuChoices = (page) =>
  page
    .locator('[data-testid="section-code"] [data-testid="order-menu"] [role="menuitemradio"]')
    .evaluateAll((items) =>
      items.map(
        (item) =>
          `${item.querySelector(".name > span").textContent}${item.ariaChecked === "true" ? " ✓" : ""}${
            item.getAttribute("aria-disabled") === "true" ? " (unavailable)" : ""
          }`,
      ),
    );

test("each heading names the order, and its menu asks the host to save another", async (t) => {
  // @covers msg:order.set setting:results.order screen:recent-order
  const page = await panelWithOrder(t, "best");
  assert.deepEqual(await orderControl(page, "section-files"), ["Best match first", "↓"]);
  assert.deepEqual(await orderControl(page), ["Best match first", "↓"]);
  const opener = page.locator('[data-testid="section-code"] [data-testid="order-button"]');
  assert.equal(await opener.getAttribute("aria-expanded"), "false");

  await opener.click();
  assert.equal(await opener.getAttribute("aria-expanded"), "true");
  assert.deepEqual(await menuChoices(page), ["Best match first ✓", "Path order", "Recently changed"]);
  assert.equal(
    await page.evaluate(() => document.activeElement?.dataset.order),
    "best",
    "the menu opens on the checked choice",
  );

  await page.locator('[data-testid="section-code"] [data-order="recent"]').click();
  assert.deepEqual(await lastSent(page, "order.set"), { order: "recent" });
  assert.equal(
    await page.evaluate(() => document.activeElement?.dataset.testid),
    "query",
    "the box keeps the keys while the search runs again",
  );
});

test("the arrow says which way path and recently changed run, and flips it", async (t) => {
  // @covers msg:order.set
  const recent = await panelWithOrder(t, "recent");
  assert.deepEqual(await orderControl(recent), ["Recently changed", "↓ newest"]);
  await recent.locator('[data-testid="section-code"] [data-testid="order-direction"]').click();
  assert.deepEqual(await lastSent(recent, "order.set"), { order: "recent:asc" });

  const oldest = await panelWithOrder(t, "recent:asc");
  assert.deepEqual(await orderControl(oldest), ["Recently changed", "↑ oldest"]);
  await oldest.locator('[data-testid="section-code"] [data-testid="order-direction"]').click();
  assert.deepEqual(await lastSent(oldest, "order.set"), { order: "recent" });

  const byPath = await panelWithOrder(t, "file");
  assert.deepEqual(await orderControl(byPath), ["Path order", "↑ a to z"]);
  await byPath.locator('[data-testid="section-code"] [data-testid="order-direction"]').click();
  assert.deepEqual(await lastSent(byPath, "order.set"), { order: "file:desc" });

  // Best match runs one way: its arrow shows the direction but isn't a button.
  const best = await panelWithOrder(t, "best");
  const arrow = best.locator('[data-testid="section-code"] [data-testid="order-direction"]');
  assert.equal(await arrow.evaluate((element) => element.tagName), "SPAN");
  assert.equal(await arrow.getAttribute("aria-disabled"), "true");
});

test("an order typed in the query is rewritten there instead of the setting", async (t) => {
  const text = "retry order:file:desc";
  const order = { kind: "op", op: "order", value: "file:desc", match: "literal", span: { start: 6, end: 21 } };
  const query = parsedQuery(
    text,
    { kind: "and", children: [textNode("retry", 0), order], span: { start: 0, end: 21 } },
    {
      globals: { case: null, count: null, type: null, order: "file:desc" },
    },
  );
  const page = await panelWithOrder(t, "best", { text, query });
  assert.deepEqual(await orderControl(page), ["Path order", "↓ z to a"]);

  await page.locator('[data-testid="section-code"] [data-testid="order-button"]').click();
  await page.locator('[data-testid="section-code"] [data-order="recent"]').click();
  assert.equal(await page.inputValue(QUERY), "retry order:recent:desc");
  assert.equal((await sentMessages(page, "order.set")).length, 0, "the setting stays as it is");

  const fresh = await panelWithOrder(t, "best", { text, query });
  await fresh.locator('[data-testid="section-code"] [data-testid="order-direction"]').click();
  assert.equal(await fresh.inputValue(QUERY), "retry order:file:asc", "the arrow flips the typed direction");
});

test("at a past revision, recently changed is unavailable and the setting's falls back to best match", async (t) => {
  const query = parsedQuery("retry at:v1.0.0", textNode("retry", 0), {
    globals: { case: null, count: null, type: null, at: "v1.0.0" },
  });
  const page = await panelWithOrder(t, "recent", { text: "retry at:v1.0.0", query });
  assert.deepEqual(await orderControl(page), ["Best match first", "↓"]);
  await page.locator('[data-testid="section-code"] [data-testid="order-button"]').click();
  assert.deepEqual(await menuChoices(page), ["Best match first ✓", "Path order", "Recently changed (unavailable)"]);
  await page.locator('[data-testid="section-code"] [data-order="recent"]').click({ force: true });
  assert.equal((await sentMessages(page, "order.set")).length, 0);
});

test("the menu moves with ↑↓ and Esc closes it back to its button", async (t) => {
  const page = await panelWithOrder(t, "best");
  const opener = page.locator('[data-testid="section-code"] [data-testid="order-button"]');
  await opener.click();
  await page.keyboard.press("ArrowDown");
  assert.equal(await page.evaluate(() => document.activeElement?.dataset.order), "file");
  await page.keyboard.press("End");
  assert.equal(await page.evaluate(() => document.activeElement?.dataset.order), "recent");
  await page.keyboard.press("Escape");
  assert.equal(await opener.getAttribute("aria-expanded"), "false");
  assert.equal(await page.evaluate(() => document.activeElement?.dataset.testid), "order-button");
  assert.equal((await sentMessages(page, "panel.close")).length, 0, "Esc closes the menu, not the panel");
});

test("recently changed results say when each file changed", async (t) => {
  // @covers screen:recent-order
  const now = Date.now();
  const iso = (hoursAgo) => new Date(now - hoursAgo * HOURS).toISOString();
  const items = [
    { ...FILE_NAME_RESULT, path: "http/retry_policy.py", ref: "f2", modifiedAt: iso(24) },
    codeLine("src/payments/client.py", 42, { dirty: true, modifiedAt: iso(1) }),
    codeLine("src/api/checkout.ts", 17, { lastCommit: { sha: "abc1234", author: "Marta Ruiz", at: iso(2) } }),
    codeLine("http/retry.py", 6, { modifiedAt: iso(24) }),
    codeLine("scripts/old.py", 3, {}),
  ];
  const page = await panelWithOrder(t, "recent", { items });
  const headers = await page
    .locator('[data-testid="section-code"] [data-testid="code-group"]')
    .evaluateAll((groups) =>
      groups.map((group) =>
        [
          group.querySelector(".path").textContent,
          ...[...group.querySelectorAll(".changed, .badge.warn")].map((note) => note.textContent),
        ].join(" | "),
      ),
    );
  assert.deepEqual(headers, [
    "src/payments/client.py | Uncommitted | 1 hour ago",
    "src/api/checkout.ts | 2 hours ago",
    "http/retry.py | 1 day ago",
    "scripts/old.py",
  ]);
  const author = await page
    .locator('[data-testid="section-code"] .changed')
    .nth(1)
    .evaluate((note) => note.title || note.dataset.hoverTitle);
  assert.equal(author, "Last commit by Marta Ruiz");
  const fileRow = await page.locator('[data-testid="section-files"] .changed').textContent();
  assert.equal(fileRow, "Modified 1 day ago");
});
