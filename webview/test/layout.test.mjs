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
  sentMessages,
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
  // 360px leaves no room for the preview, so the selection shows on the row alone.
  assert.equal(await page.getAttribute('[data-ref="l20"]', "aria-selected"), "true");
  const box = await page.locator(QUERY).boundingBox();
  assert.ok(box && box.y >= 0 && box.y + box.height <= 260, `query box at ${JSON.stringify(box)}`);
  assert.ok((await page.locator("#body").boundingBox()).height >= 120);
  assert.deepEqual(pageErrors(page), []);
});

/** The left and right edges of the element `selector` matches, in pixels. */
const edges = (page, selector) =>
  page.evaluate((wanted) => {
    const { left, right } = document.querySelector(wanted).getBoundingClientRect();
    return { left: Math.round(left), right: Math.round(right) };
  }, selector);

test("a result line, path or name wider than the panel never pushes the preview off it", async (t) => {
  const minified = "retry;var a=function(){return{a:1,b:2}};".repeat(8);
  const long = `src/${"generated_".repeat(30)}client.py`;
  const items = [
    { ...CODE_LINE_RESULT, text: minified, hits: [{ start: 0, end: 5, termIndex: 0 }] },
    { ...CODE_LINE_RESULT, ref: "l2", path: long },
    { kind: "file", ref: "f1", repoId: "r1", path: long, nameHits: [], dirty: false },
    {
      kind: "symbol",
      ref: "s1",
      repoId: "r1",
      path: "a.py",
      line: 1,
      name: "retry".repeat(60),
      symbolKind: "function",
      hits: [],
    },
  ];
  for (const item of items) {
    const page = await openPanel(t);
    await page.setViewportSize({ width: 1200, height: 600 });
    await restore(page);
    const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
    await fromHost(page, "search.batch", { seq, searchId: "s1", items: [item] });
    const body = await edges(page, "#body");
    const preview = await edges(page, "#preview");
    assert.ok(
      preview.right <= body.right,
      `${item.kind}: preview spans ${preview.left}..${preview.right} in ${body.right}`,
    );
    assert.ok(preview.right - preview.left > 300, `${item.kind}: preview ${preview.right - preview.left}px wide`);
  }
});

test("the preview sits beside the results down to 641px, and below that hides and isn't asked for", async (t) => {
  const page = await openPanel(t);
  await page.setViewportSize({ width: 600, height: 600 });
  await restore(page);
  const seq = await typeAndParse(page, "retry", parsedQuery("retry", textNode("retry", 0)));
  const items = Array.from({ length: 40 }, (_, index) => ({ ...CODE_LINE_RESULT, ref: `l${index}`, line: index + 1 }));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  await page.keyboard.press("ArrowDown");
  await page.waitForTimeout(100); // past the preview's debounce
  assert.deepEqual(await sentMessages(page, "result.select"), [], "no preview asked for while it can't show");

  // Wide enough again: the selected result's preview is asked for once, and shows beside the results.
  for (const width of [641, 720, 1200]) {
    await page.setViewportSize({ width, height: 600 });
    await waitForSent(page, "result.select", { ref: "l1" });
    const body = await page.locator("#body").boundingBox();
    const results = await page.locator("#results").boundingBox();
    const preview = await page.locator("#preview").boundingBox();
    assert.ok(preview.x >= results.x + results.width, `${width}px: preview beside the results`);
    assert.ok(preview.y + preview.height <= body.y + body.height + 1, `${width}px: preview inside the panel`);
    assert.ok(preview.width > results.width, `${width}px: preview the wider of the two`);
  }
  assert.equal((await sentMessages(page, "result.select")).length, 1);
});

/** A file-name result for `path`, with every detail a row can show after it. */
const fileRow = (ref, path, nameHits) => ({
  kind: "file",
  ref,
  repoId: "r1",
  path,
  nameHits,
  dirty: false,
  rankReason: "test",
  owners: ["@acme/payments-core", "@jane"],
  lastCommit: { author: "Someone With A Long Name", at: "2026-01-01T00:00:00Z" },
});

/** Whether a file-name row cuts its path, and where the details after it sit. */
const rowLayout = (page, ref) =>
  page.evaluate((wanted) => {
    const row = document.querySelector(`[data-ref="${wanted}"]`);
    const path = row.querySelector(".path");
    const details = row.querySelector(".details");
    return {
      pathCut: path.scrollWidth > path.clientWidth,
      detailsBelow: details.getBoundingClientRect().top >= path.getBoundingClientRect().bottom,
      detailsInside: details.getBoundingClientRect().right <= row.getBoundingClientRect().right,
    };
  }, ref);

/** The color of the pixel at `x`, `y` of a PNG, read in the page. */
const pixel = (page, png, x, y) =>
  page.evaluate(
    async ([base64, px, py]) => {
      // Decoded here: the page's content security policy lets it fetch nothing, data: URLs included.
      const bytes = Uint8Array.from(atob(base64), (character) => character.codePointAt(0));
      const image = await createImageBitmap(new Blob([bytes], { type: "image/png" }));
      const canvas = new OffscreenCanvas(image.width, image.height);
      const context = canvas.getContext("2d");
      context.drawImage(image, 0, 0);
      return [...context.getImageData(px, py, 1, 1).data].join(",");
    },
    [png.toString("base64"), x, y],
  );

test("a file name's path takes the row first, and a highlight cut by its ellipsis leaves nothing after it", async (t) => {
  const page = await openPanel(t);
  await page.setViewportSize({ width: 760, height: 500 });
  await restore(page, [], { order: "file" });
  const seq = await typeAndParse(page, "payment", parsedQuery("payment", textNode("payment", 0)));
  const fits = "src/payments/retry/payment_retry_policy.py";
  const cut = `tests/payments/${"integration/".repeat(4)}gateway.py`;
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [fileRow("fits", fits, [{ start: 19, end: 26 }]), fileRow("cut", cut, [{ start: 6, end: 13 }])],
  });
  const layout = { fits: await rowLayout(page, "fits"), cut: await rowLayout(page, "cut") };
  // The path fits on its line, so the badges, the author and the repo go below it rather than cut it short.
  assert.deepEqual(layout.fits, { pathCut: false, detailsBelow: true, detailsInside: true });
  assert.equal(layout.cut.pathCut, true);

  // Cut inside the highlight: the highlight is left out whole, so its color doesn't run on past the ellipsis.
  await page.setViewportSize({ width: 700, height: 500 });
  await page.evaluate(() => {
    const path = document.querySelector('[data-ref="cut"] .path');
    path.style.flex = "none";
    path.style.maxWidth = `${path.querySelector("mark").offsetLeft + 20}px`;
  });
  const path = page.locator('[data-ref="cut"] .path');
  const box = await path.boundingBox();
  const png = await page.locator('[data-ref="cut"]').screenshot();
  const row = await page.locator('[data-ref="cut"]').boundingBox();
  const y = Math.round(box.y - row.y + box.height / 2);
  const end = Math.round(box.x - row.x + box.width) - 2;
  assert.equal(await pixel(page, png, end, y), await pixel(page, png, end + 6, y), "the end of the path shows the row");
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

test("a file group's header and the preview's title stay on one line, cutting the path, not the repo", async (t) => {
  const page = await openPanel(t);
  await page.setViewportSize({ width: 700, height: 500 });
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  const seq = await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  const path = `tests/payments/${"integration/".repeat(3)}retry_policy_test.py`;
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [{ ...CODE_LINE_RESULT, path, rankReason: "test", owners: ["@acme/payments-core"] }],
  });
  await waitForSent(page, "result.select", { ref: "l1" });
  await fromHost(page, "preview.result", {
    ref: "l1",
    preview: {
      kind: "file",
      path,
      firstLine: 42,
      lines: ["self.retry_policy = RetryPolicy()"],
      focusLine: 42,
      hits: [],
      dirtyLines: [],
    },
  });
  // How many lines a node's text is laid out on.
  const lines = (selector) =>
    page.locator(selector).evaluate((node) => {
      const range = document.createRange();
      range.selectNodeContents(node);
      return new Set([...range.getClientRects()].map((rect) => Math.round(rect.top))).size;
    });
  for (const selector of ['[data-testid="code-group"] .repo', '[data-testid="code-group"] .path', ".preview-title"]) {
    assert.equal(await lines(selector), 1, `${selector} keeps to one line`);
  }
  assert.equal(await page.locator('[data-testid="code-group"] .repo').innerText(), "payments-api");
  const cut = await page
    .locator('[data-testid="code-group"] .path')
    .evaluate((node) => node.scrollWidth > node.clientWidth);
  assert.ok(cut, "the path gives way, with an ellipsis");
});
