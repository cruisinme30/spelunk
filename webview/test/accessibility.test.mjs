// What keyboard and screen-reader users get: which option ↑↓ is on, a preview
// that doesn't read itself out, the empty box's queries announced, and a repo
// menu driven from the keyboard.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  fromHost,
  lastSent,
  openPanel,
  parsedQuery,
  QUERY,
  restore,
  sentMessages,
  textNode,
  typeAndParse,
  useBrowser,
} from "./harness.mjs";

useBrowser();

const attribute = (page, name) => page.getAttribute(QUERY, name);

/** The edit that replaces the typed "s" with `label`. */
const insert = (label) => ({ title: label, edits: [{ span: { start: 0, end: 1 }, newText: label }] });

test("the box points screen readers at the suggestion or the result ↑↓ is on", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  await page.fill(QUERY, "s");
  const { seq } = await lastSent(page, "query.changed");
  const completions = ["since:", "sym:"].map((label) => ({
    label,
    detail: label,
    group: "operator",
    insert: insert(label),
  }));
  await fromHost(page, "parse.result", { seq, query: parsedQuery("s", textNode("s", 0)), completions });
  assert.equal(await attribute(page, "aria-controls"), "completions");
  assert.equal(await attribute(page, "aria-activedescendant"), "completion-0");
  await page.keyboard.press("ArrowDown");
  assert.equal(await attribute(page, "aria-activedescendant"), "completion-1");

  await page.keyboard.press("Escape"); // suggestions close, the word searched as typed; the results take ↑↓
  const { seq: searched } = await lastSent(page, "query.changed");
  await fromHost(page, "search.batch", { seq: searched, searchId: "s1", items: [CODE_LINE_RESULT] });
  const row = page.locator('[data-testid="result"]').first();
  assert.equal(await attribute(page, "aria-activedescendant"), await row.getAttribute("id"));
  assert.match(await attribute(page, "aria-controls"), /section-code-list/);

  await page.fill(QUERY, ""); // the empty box: no results left to point at
  const { seq: emptied } = await lastSent(page, "query.changed");
  await fromHost(page, "parse.result", { seq: emptied, query: parsedQuery("", null), completions: [] });
  assert.equal(await attribute(page, "aria-activedescendant"), null);
});

test("the result lists hold only results, and the preview isn't read out on every ↑↓", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [CODE_LINE_RESULT] });
  await fromHost(page, "search.done", {
    seq,
    searchId: "s1",
    total: 2,
    truncated: false,
    hidden: [],
    ms: 1,
    nextCursor: "2",
  });
  const inListbox = (selector) =>
    page
      .locator(selector)
      .first()
      .evaluate((node) => node.closest('[role="listbox"]') !== null);
  assert.equal(await inListbox('[data-testid="result"]'), true);
  assert.equal(await inListbox('[data-testid="load-more"]'), false);
  assert.equal(await page.getAttribute('[data-testid="preview"]', "aria-live"), null);
});

test("↑↓ in the empty box says which query ↵ would run", async (t) => {
  const page = await openPanel(t);
  await restore(page, ["retry_policy", "timeout"]);
  await page.focus(QUERY);
  await page.keyboard.press("ArrowDown");
  assert.equal(await page.textContent('[data-testid="announcer"]'), "timeout");
});

test("the repo menu takes the focus, moves with ↑↓, and Esc closes it back to its button", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  const repos = [
    { repoId: "a", name: "payments-api", tree: "ready", history: "off" },
    { repoId: "w", name: "web-checkout", tree: "ready", history: "off" },
  ];
  await fromHost(page, "index.status", { repos });
  await page.focus('[data-testid="repos"]');
  await page.keyboard.press("Enter");
  const focused = () => page.evaluate(() => document.activeElement?.innerText?.replaceAll(/\s+/g, " ").trim());
  assert.equal(await focused(), "✓ All repos 2", "the picked row");
  await page.keyboard.press("ArrowDown");
  assert.equal(await focused(), "payments-api");
  await page.keyboard.press("End");
  assert.equal(await focused(), "web-checkout");
  await page.keyboard.press("Escape");
  assert.equal(await page.isVisible('[data-testid="repo-menu"]'), false);
  assert.equal(await page.evaluate(() => document.activeElement?.dataset.testid), "repos");
  assert.equal((await sentMessages(page, "panel.close")).length, 0, "Esc closed the menu, not the panel");

  await page.keyboard.press("Enter");
  await page.keyboard.press("Tab"); // leaving the menu closes it
  assert.equal(await page.isVisible('[data-testid="repo-menu"]'), false);
});

/** WCAG's relative luminance of a CSS color, as computed styles give it ("rgb(…)"). */
function luminance(color) {
  const [red, green, blue] = color
    .match(/[\d.]+/g)
    .slice(0, 3)
    .map((value) => {
      const channel = Number(value) / 255;
      return channel <= 0.039_28 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
    });
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
}

/** WCAG's contrast ratio of two CSS colors. */
function contrast(foreground, background) {
  const [lighter, darker] = [luminance(foreground), luminance(background)].sort((a, b) => b - a);
  return (lighter + 0.05) / (darker + 0.05);
}

test("under a high contrast light theme the operator colors stay readable and the selected row is outlined", async (t) => {
  const page = await openPanel(t);
  // What VS Code gives a webview under High Contrast Light.
  await page.evaluate(() => {
    document.body.className = "vscode-high-contrast vscode-high-contrast-light";
    const colors = {
      "--vscode-editor-background": "#ffffff",
      "--vscode-quickInput-background": "#ffffff",
      "--vscode-editorWidget-background": "#ffffff",
      "--vscode-foreground": "#292929",
      "--vscode-contrastActiveBorder": "#006bbd",
    };
    for (const [name, value] of Object.entries(colors)) document.documentElement.style.setProperty(name, value);
  });
  await restore(page);
  const tones = await page.$$eval(".sheet .card-title", (titles) =>
    titles.map((title) => [title.className, getComputedStyle(title).color]),
  );
  assert.ok(tones.length > 0);
  for (const [name, color] of tones) {
    const ratio = contrast(color, "rgb(255, 255, 255)");
    assert.ok(ratio >= 4.5, `${name}: ${ratio.toFixed(2)}:1 on white`);
  }

  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [CODE_LINE_RESULT] });
  const row = await page.$eval('[data-ref="l1"]', (element) => {
    const style = getComputedStyle(element);
    return { outline: style.outlineStyle, background: style.backgroundColor };
  });
  assert.deepEqual(row, { outline: "dashed", background: "rgba(0, 0, 0, 0)" });
});
