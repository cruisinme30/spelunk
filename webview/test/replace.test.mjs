// The Replace row: it previews a replace on the result rows, applies only a
// preview it has, drops answers to older requests, and says what it did.
// @covers msg:replace.preview msg:replace.plan msg:replace.apply msg:replace.done msg:replace.save
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fromHost,
  lastSent,
  pageErrors,
  panelWithResults,
  QUERY,
  sentMessages,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

const ROW = '[data-testid="replace-row"]';
const REPLACEMENT = '[data-testid="replacement"]';
const SUMMARY = '[data-testid="replace-summary"]';
const REPLACE_ALL = '[data-testid="replace-all"]';
const CODE_ROW_TEXT = '[data-kind="line"] .text';

/** A plan replacing the code result's retry_policy (client.py line 42, UTF-16 [5,17)) with `newText`. */
const planFor = (newText) => ({
  matches: 1,
  truncated: false,
  files: [
    {
      repoId: "r1",
      path: "src/payments/client.py",
      file: "/work/src/payments/client.py",
      lines: [{ line: 42, text: "self.retry_policy = RetryPolicy()", edits: [{ start: 5, end: 17, newText }] }],
    },
  ],
});

/** Opens the Replace row with ⇄ over a panel of results, types `replacement` and returns its preview request. */
async function previewReplace(t, replacement) {
  const page = await panelWithResults(t);
  await page.click('[data-testid="toggle-replace"]');
  assert.equal(await page.isVisible(ROW), true);
  await page.fill(REPLACEMENT, replacement);
  const request = await waitForSent(page, "replace.preview", { text: "retry_policy", replacement });
  return { page, request };
}

test("the Replace row previews a replace on the result rows and applies it on ⌘↵", async (t) => {
  // @covers screen:replace-in-files
  const { page, request } = await previewReplace(t, "backoff");
  assert.equal(await page.isDisabled(REPLACE_ALL), true, "no preview yet");
  await fromHost(page, "replace.plan", { seq: request.seq, plan: planFor("backoff") });
  assert.equal(await page.textContent(SUMMARY), "1 match in 1 file");
  assert.equal(await page.textContent(`${CODE_ROW_TEXT} del`), "retry_policy");
  assert.equal(await page.textContent(`${CODE_ROW_TEXT} ins`), "backoff");
  assert.match(await page.innerText('[data-testid="keys"]'), /⌘↵\s*replace all\s+Esc\s*close replace/);

  await page.focus(REPLACEMENT);
  await page.keyboard.press("Meta+Enter");
  const apply = await lastSent(page, "replace.apply");
  assert.deepEqual(apply, { seq: apply.seq, text: "retry_policy", replacement: "backoff", matches: 1 });
  assert.ok(apply.seq > request.seq);
  assert.equal(await page.isDisabled(REPLACE_ALL), true, "one apply at a time");

  await fromHost(page, "replace.done", { seq: apply.seq, replaced: 1, files: 1, skipped: ["src/old.py"] });
  assert.equal(await page.textContent(SUMMARY), "Replaced 1 match in 1 file");
  assert.match(await page.textContent('[data-testid="replace-note"]'), /unsaved.*Skipped 1 file.*src\/old\.py/s);
  assert.equal(await page.locator(`${CODE_ROW_TEXT} del`).count(), 0, "the rows show the search again");
  await page.click('[data-testid="replace-save"]');
  assert.equal((await sentMessages(page, "replace.save")).length, 1);
  assert.deepEqual(pageErrors(page), []);
});

test("Replace all stays off without a preview, and an older preview's answer is dropped", async (t) => {
  const { page, request } = await previewReplace(t, "a");
  await page.fill(REPLACEMENT, "ab");
  const newer = await waitForSent(page, "replace.preview", { replacement: "ab" });
  await fromHost(page, "replace.plan", { seq: request.seq, plan: planFor("a") });
  assert.equal(await page.isDisabled(REPLACE_ALL), true, "the answer for 'a' is out of date");
  await fromHost(page, "replace.plan", { seq: newer.seq, error: "replace needs exactly one search term" });
  assert.equal(await page.textContent(SUMMARY), "Replace needs exactly one search term.");
  assert.equal(await page.isDisabled(REPLACE_ALL), true);
  await page.click(REPLACE_ALL, { force: true });
  assert.equal((await sentMessages(page, "replace.apply")).length, 0);

  await fromHost(page, "replace.plan", { seq: newer.seq, plan: { matches: 20_001, truncated: true, files: [] } });
  assert.equal(await page.textContent(SUMMARY), "Too many matches to replace at once. Narrow the search.");
  assert.equal(await page.isDisabled(REPLACE_ALL), true);
});

test("when the matches changed since the preview, nothing is replaced and the new preview shows", async (t) => {
  const { page, request } = await previewReplace(t, "backoff");
  await fromHost(page, "replace.plan", { seq: request.seq, plan: planFor("backoff") });
  await page.click(REPLACE_ALL);
  const apply = await lastSent(page, "replace.apply");
  await fromHost(page, "replace.plan", { seq: apply.seq, plan: { ...planFor("backoff"), matches: 2 } });
  await fromHost(page, "replace.done", { seq: apply.seq, replaced: 0, files: 0, skipped: [], changed: true });
  assert.equal(await page.textContent(SUMMARY), "2 matches in 1 file");
  assert.match(await page.textContent('[data-testid="replace-note"]'), /changed since the preview/);
  assert.equal(await page.isDisabled(REPLACE_ALL), false);
});

test("Esc closes the Replace row, drops its preview and gives the query box the focus", async (t) => {
  const { page, request } = await previewReplace(t, "backoff");
  await fromHost(page, "replace.plan", { seq: request.seq, plan: planFor("backoff") });
  await page.focus(REPLACEMENT);
  await page.keyboard.press("Escape");
  assert.equal(await page.isVisible(ROW), false);
  assert.equal(await page.getAttribute('[data-testid="toggle-replace"]', "aria-expanded"), "false");
  assert.equal(await page.locator(`${CODE_ROW_TEXT} mark`).count(), 1, "marked as a search again");
  assert.equal(await page.evaluate(() => document.activeElement?.id), "query");
  // ⌥⌘F in the query box opens it again.
  await page.focus(QUERY);
  await page.keyboard.press("Meta+Alt+KeyF");
  assert.equal(await page.isVisible(ROW), true);
});

test("closing the row while Replace all runs leaves Replace working when it opens again", async (t) => {
  const { page, request } = await previewReplace(t, "backoff");
  await fromHost(page, "replace.plan", { seq: request.seq, plan: planFor("backoff") });
  await page.focus(REPLACEMENT);
  await page.keyboard.press("Meta+Enter");
  const apply = await lastSent(page, "replace.apply");
  await page.keyboard.press("Escape"); // before the host answers

  // Opened again while the host still runs it: the row waits for that answer instead of previewing over it.
  await page.click('[data-testid="toggle-replace"]');
  const previews = (await sentMessages(page, "replace.preview")).length;
  await fromHost(page, "replace.done", { seq: apply.seq, replaced: 1, files: 1, skipped: [] });
  const next = await waitForSent(page, "replace.preview", { replacement: "backoff" });
  assert.equal((await sentMessages(page, "replace.preview")).length, previews + 1);
  assert.notEqual(next.seq, apply.seq);
  await fromHost(page, "replace.plan", { seq: next.seq, plan: planFor("backoff") });
  assert.equal(await page.textContent(SUMMARY), "1 match in 1 file");
  assert.equal(await page.isDisabled(REPLACE_ALL), false);

  // Closed for good before the answer: the next opening previews at once.
  await page.focus(REPLACEMENT);
  await page.keyboard.press("Meta+Enter");
  const second = await lastSent(page, "replace.apply");
  await page.keyboard.press("Escape");
  await fromHost(page, "replace.done", { seq: second.seq, replaced: 1, files: 1, skipped: [] });
  await page.click('[data-testid="toggle-replace"]');
  await page.fill(REPLACEMENT, "delay");
  await waitForSent(page, "replace.preview", { replacement: "delay" });
  assert.deepEqual(pageErrors(page), []);
});

test("⌘↵ right after typing a new replacement previews it rather than applying text the rows never showed", async (t) => {
  const { page, request } = await previewReplace(t, "backoff");
  await fromHost(page, "replace.plan", { seq: request.seq, plan: planFor("backoff") });
  // Typed and applied before the typing delay asks for the new preview.
  await page.evaluate((selector) => {
    const input = document.querySelector(selector);
    input.value = "backoff_ms";
    input.dispatchEvent(new Event("input"));
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", metaKey: true, ctrlKey: true }));
  }, REPLACEMENT);
  assert.equal((await sentMessages(page, "replace.apply")).length, 0);
  assert.equal((await lastSent(page, "replace.preview")).replacement, "backoff_ms");
});
