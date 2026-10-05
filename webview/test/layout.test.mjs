// The panel, the help page and the welcome page in small spaces: a short
// bottom panel, or a narrow editor split. Everything stays reachable and
// nothing widens the page past its edge.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  fromHost,
  openHelp,
  openPanel,
  openWelcome,
  pageErrors,
  parsedQuery,
  QUERY,
  restore,
  textNode,
  typeAndParse,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

test("in a short panel the query box stays in sight and the results keep some room", async (t) => {
  const page = await openPanel(t);
  await page.setViewportSize({ width: 360, height: 260 });
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "indexing", history: "ready", progress: 0.4 }],
  });
  const diagnostics = [
    {
      severity: "warning",
      code: "slow",
      message: "This may be slow. ".repeat(6),
      span: { start: 0, end: 5 },
      fixes: [],
    },
  ];
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0), { diagnostics }));
  const items = Array.from({ length: 30 }, (_, index) => ({ ...CODE_LINE_RESULT, ref: `l${index}`, line: index + 1 }));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  for (let step = 0; step < 20; step++) await page.keyboard.press("ArrowDown");
  await waitForSent(page, "result.select", { ref: "l20" });
  const box = await page.locator(QUERY).boundingBox();
  assert.ok(box && box.y >= 0 && box.y + box.height <= 260, `query box at ${JSON.stringify(box)}`);
  assert.ok((await page.locator("#body").boundingBox()).height >= 120);
  assert.deepEqual(pageErrors(page), []);
});

/** How far the page is wider than its window, in pixels (0 when it fits). */
const horizontalOverflow = (page) =>
  page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

test("the help page and the welcome page fit a narrow editor without scrolling sideways", async (t) => {
  for (const width of [200, 360]) {
    const help = await openHelp(t);
    await help.setViewportSize({ width, height: 500 });
    assert.equal(await horizontalOverflow(help), 0, `help at ${width}px`);

    const welcome = await openWelcome(t);
    await welcome.setViewportSize({ width, height: 500 });
    await fromHost(welcome, "welcome.state", { preset: "none", historyDepth: "all", symbols: false, mac: false });
    await fromHost(welcome, "index.status", {
      repos: [{ repoId: "a", name: "payments-api", tree: "indexing", history: "ready", progress: 0.3 }],
    });
    assert.equal(await horizontalOverflow(welcome), 0, `welcome at ${width}px`);
  }
});

test("the help page's operator table keeps Try buttons and examples readable at every width", async (t) => {
  for (const width of [1200, 800, 480, 360]) {
    const help = await openHelp(t);
    await help.setViewportSize({ width, height: 720 });
    const layout = await help.evaluate(() => {
      /** How many lines an element's text takes. */
      const lines = (node) => {
        const range = document.createRange();
        range.selectNodeContents(node);
        return new Set([...range.getClientRects()].map((rect) => Math.round(rect.top))).size;
      };
      const rows = [...document.querySelectorAll('[data-testid="operator-row"]')];
      return {
        tryLines: Math.max(...rows.map((row) => lines(row.querySelector('[data-testid="try"]')))),
        // An example breaks only between its words, never inside one.
        brokenExamples: rows
          .map((row) => row.querySelector(".operator-example code"))
          .filter((code) => lines(code) > code.textContent.split(/[\s|]+/).length)
          .map((code) => code.textContent),
      };
    });
    assert.equal(layout.tryLines, 1, `Try wraps at ${width}px`);
    assert.deepEqual(layout.brokenExamples, [], `examples broken mid-word at ${width}px`);
    assert.equal(await horizontalOverflow(help), 0, `help at ${width}px`);
  }
});
