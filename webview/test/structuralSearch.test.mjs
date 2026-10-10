// struct: finds code by its shape: each match is one result row showing
// every line it spans, with the template's code marked in the term's color,
// each hole's code in its hole's color, and a chip for what each hole took.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fromHost,
  openPanel,
  pageErrors,
  parsedQuery,
  restore,
  searchDone,
  typeAndParse,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

const TEMPLATE = "backoff(:[call], tries=:[n])";
const QUERY_TEXT = `struct:"${TEMPLATE}"`;

/** The parsed query for QUERY_TEXT: one struct: term. */
const parsed = parsedQuery(QUERY_TEXT, {
  kind: "text",
  value: TEMPLATE,
  match: "structural",
  termIndex: 0,
  contentOnly: true,
  span: { start: 0, end: QUERY_TEXT.length },
});

/** webhooks.py's call of backoff over lines 7 to 9, marked as the daemon marks it. */
const SPANNING = {
  kind: "line",
  ref: "webhooks.py:7",
  repoId: "r1",
  path: "http/webhooks.py",
  line: 7,
  text: "    return backoff(",
  hits: [{ start: 11, end: 19, termIndex: 0 }],
  spanLines: [
    {
      text: "        lambda: send(event.url, event.body),",
      hits: [
        { start: 8, end: 43, termIndex: 0, hole: 1 },
        { start: 43, end: 44, termIndex: 0 },
      ],
    },
    {
      text: "        tries=event.max_tries)",
      hits: [
        { start: 8, end: 14, termIndex: 0 },
        { start: 14, end: 29, termIndex: 0, hole: 2 },
        { start: 29, end: 30, termIndex: 0 },
      ],
    },
  ],
  endLine: 9,
  holes: [
    { hole: 1, name: "call", text: "lambda: send(event.url, event.body)" },
    { hole: 2, name: "n", text: "event.max_tries" },
  ],
};

/** The call on line 19, all on one line. */
const ONE_LINE = {
  kind: "line",
  ref: "webhooks.py:19",
  repoId: "r1",
  path: "http/webhooks.py",
  line: 19,
  text: "    return backoff(lambda: send(event.url, None), tries=1)",
  hits: [
    { start: 11, end: 19, termIndex: 0 },
    { start: 19, end: 48, termIndex: 0, hole: 1 },
    { start: 48, end: 56, termIndex: 0 },
    { start: 56, end: 57, termIndex: 0, hole: 2 },
    { start: 57, end: 58, termIndex: 0 },
  ],
  holes: [
    { hole: 1, name: "call", text: "lambda: send(event.url, None)" },
    { hole: 2, name: "n", text: "1" },
  ],
};

/** A panel showing `items` for QUERY_TEXT. */
async function panelWith(t, items) {
  const page = await openPanel(t);
  await restore(page);
  const seq = await typeAndParse(page, QUERY_TEXT, parsed);
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  await searchDone(page, seq, { total: items.length });
  return page;
}

/** Each code result row's lines as "7 text", code marks in [brackets] and hole marks in {n:braces}. */
function rows(page) {
  return page.locator('[data-testid="section-code"] [data-ref]').evaluateAll((found) =>
    found.map((row) =>
      [...row.querySelectorAll(".span-line")]
        .map((line) => {
          const shown = [...line.querySelector(".text").childNodes]
            .map((node) => {
              if (node.nodeName !== "MARK") return node.textContent;
              const hole = /hole-color-(\d)/.exec(node.className);
              return hole ? `{${Number(hole[1]) + 1}:${node.textContent}}` : `[${node.textContent}]`;
            })
            .join("");
          return `${line.querySelector(".line-number").textContent} ${shown}`;
        })
        .join(" | "),
    ),
  );
}

/** Each row's hole chips as ":[name] text". */
function chips(page) {
  return page
    .locator('[data-testid="section-code"] [data-ref]')
    .evaluateAll((found) =>
      found.map((row) =>
        [...row.querySelectorAll('[data-testid="hole-values"] .hole-value')].map((chip) => chip.textContent),
      ),
    );
}

test("a struct: match is one row of its lines, its holes marked apart from its code and listed below", async (t) => {
  // @covers screen:structural-search op:struct
  const page = await panelWith(t, [SPANNING, ONE_LINE]);
  assert.deepEqual(await rows(page), [
    "7 return [backoff(] | 8     {1:lambda: send(event.url, event.body)}[,] | 9     [tries=]{2:event.max_tries}[)]",
    "19 return [backoff(]{1:lambda: send(event.url, None)}[, tries=]{2:1}[)]",
  ]);
  assert.deepEqual(await chips(page), [
    [":[call]lambda: send(event.url, event.body)", ":[n]event.max_tries"],
    [":[call]lambda: send(event.url, None)", ":[n]1"],
  ]);
  // Each hole keeps one color in every row and chip.
  const colors = await page
    .locator('[data-testid="hole-values"] .hole-value')
    .evaluateAll((found) => found.map((chip) => /hole-color-\d/.exec(chip.className)[0]));
  assert.deepEqual(colors, ["hole-color-0", "hole-color-1", "hole-color-0", "hole-color-1"]);
  // One result per match: ↓ steps from match to match.
  assert.match(await page.locator('[data-testid="section-code"] .section-title').innerText(), /2 in 1 file/);
  await waitForSent(page, "result.select", { ref: "webhooks.py:7" });
  await page.keyboard.press("ArrowDown");
  await waitForSent(page, "result.select", { ref: "webhooks.py:19" });
  assert.deepEqual(pageErrors(page), []);
});

test("the struct: chip names the template", async (t) => {
  // @covers screen:structural-search
  const page = await panelWith(t, [SPANNING]);
  const chip = page.locator('[data-testid="chips"] .chip.term');
  assert.equal(await chip.locator(".chip-label").textContent(), "struct");
  assert.equal(await chip.locator("code").textContent(), TEMPLATE);
});

test("a hole's text the daemon cut stays one line in its chip", async (t) => {
  const long = { ...ONE_LINE, holes: [{ hole: 1, name: "call", text: `${"argument, ".repeat(7)}…` }] };
  const page = await panelWith(t, [long]);
  const chip = page.locator('[data-testid="hole-values"] .hole-value').first();
  assert.equal(await chip.getAttribute("title"), `:[call] took ${"argument, ".repeat(7)}…`);
  const box = await chip.boundingBox();
  assert.ok(box.height < 24, `the chip is ${box.height}px tall`);
});

test("the preview marks the template's code and each hole in its color", async (t) => {
  // @covers screen:structural-search msg:preview.result
  const page = await panelWith(t, [SPANNING]);
  await waitForSent(page, "result.select", { ref: "webhooks.py:7" });
  await fromHost(page, "preview.result", {
    ref: "webhooks.py:7",
    preview: {
      kind: "file",
      path: "http/webhooks.py",
      firstLine: 6,
      lines: [
        "    # backoff(fn, tries=N) gives up after N tries",
        SPANNING.text,
        SPANNING.spanLines[0].text,
        SPANNING.spanLines[1].text,
      ],
      focusLine: 7,
      focusEndLine: 9,
      hits: [
        { line: 7, ranges: SPANNING.hits },
        { line: 8, ranges: SPANNING.spanLines[0].hits },
        { line: 9, ranges: SPANNING.spanLines[1].hits },
      ],
      dirtyLines: [],
    },
  });
  const preview = page.locator('[data-testid="preview"]');
  assert.match(await preview.locator(".preview-header").innerText(), /lines 7–9/);
  assert.equal(await preview.locator("mark.hole-color-0").textContent(), "lambda: send(event.url, event.body)");
  assert.equal(await preview.locator("mark.hole-color-1").textContent(), "event.max_tries");
  assert.equal(await preview.locator("mark:not(.hole)").count(), 4);
});

test("Replace explains it can't change a struct: template, whose rows stay marked", async (t) => {
  // @covers screen:structural-search msg:replace.plan
  const page = await panelWith(t, [ONE_LINE]);
  await page.click('[data-testid="toggle-replace"]');
  await page.fill('[data-testid="replacement"]', "retry");
  const request = await waitForSent(page, "replace.preview", { replacement: "retry" });
  await fromHost(page, "replace.plan", {
    seq: request.seq,
    error: "replace changes text or a regex, so it can't replace a struct: template",
  });
  assert.equal(
    await page.textContent('[data-testid="replace-summary"]'),
    "Replace changes text or a regex, so it can't replace a struct: template.",
  );
  assert.equal(await page.isDisabled('[data-testid="replace-all"]'), true);
  assert.equal((await rows(page))[0], "19 return [backoff(]{1:lambda: send(event.url, None)}[, tries=]{2:1}[)]");
});

test("the sheet's struct: entry puts the cursor between the quotes", async (t) => {
  // @covers screen:structural-search screen:empty-box
  const page = await openPanel(t);
  await restore(page);
  await page.locator('[data-testid="sheet-op"]', { hasText: "struct:" }).click();
  assert.equal(await page.inputValue('[data-testid="query"]'), 'struct:""');
  const cursor = await page.locator('[data-testid="query"]').evaluate((input) => input.selectionStart);
  assert.equal(cursor, 8);
});
