// Context lines: with spelunk.results.contextLines above 0, each code result
// shows dimmed lines around its match. Matches close together share one run
// of lines, and "⋯" marks the lines skipped between runs.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  fromHost,
  lastSent,
  openPanel,
  pageErrors,
  parsedQuery,
  restore,
  searchDone,
  sentMessages,
  textNode,
  typeAndParse,
  UI_SETTINGS,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

/** client.py as it reads around the matches of _retry. */
const CLIENT = {
  12: "    def charge(self, amount, card):",
  13: "        request = self._build(amount, card)",
  14: "        return self._retry(lambda: self.session.post(request))",
  15: "",
  16: "    def refund(self, charge_id):",
  17: "        return self._retry(lambda: self.session.delete(charge_id))",
  18: "",
  19: "    def _build(self, amount, card):",
  29: "        }",
  30: "",
  31: "    def _retry(self, call):",
  32: "        for attempt in range(self.config.attempts):",
  33: "            try:",
};

/** The code result for a match of _retry on `line` of `file`, with `n` lines of context from `lines`. */
function match(line, n, { file = "src/payments/client.py", lines = CLIENT } = {}) {
  const text = lines[line];
  const start = text.indexOf("_retry");
  const range = (from, to) =>
    Array.from({ length: to - from + 1 }, (_, index) => from + index).filter((at) => at in lines);
  return {
    ...CODE_LINE_RESULT,
    ref: `${file}:${line}`,
    path: file,
    line,
    text,
    hits: [{ start, end: start + 6, termIndex: 0 }],
    contextBefore: range(line - n, line - 1).map((at) => lines[at]),
    contextAfter: range(line + 1, line + n).map((at) => lines[at]),
  };
}

/** A panel searching _retry with `contextLines` set, showing `batches` of results. */
async function panelWith(t, contextLines, ...batches) {
  const page = await openPanel(t);
  await restore(page, [], { contextLines });
  const seq = await typeAndParse(page, "_retry", parsedQuery("_retry", textNode("_retry", 0)));
  for (const items of batches) await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  await searchDone(page, seq, { total: batches.flat().length });
  return page;
}

/**
 * The code section as it reads: "12" for a context line, "[14]" for a
 * match, "⋯" for a gap and "# path" for a file header.
 */
function codeSection(page) {
  return page.locator('[data-testid="section-code"] .list').evaluate((list) =>
    [...list.querySelectorAll('[data-testid="code-group"], [data-ref], .context-line, .context-gap')].map((row) => {
      if (row.dataset.testid === "code-group") return `# ${row.querySelector(".path").textContent}`;
      if (row.classList.contains("context-gap")) return "⋯";
      const number = row.querySelector(".line-number").textContent;
      return row.dataset.ref ? `[${number}]` : number;
    }),
  );
}

/** The text a row shows for `line`, as the code section draws it. */
const textOf = (page, line) =>
  page
    .locator(`[data-testid="section-code"] :is([data-line="${line}"], [data-ref$=":${line}"]) .text`)
    .first()
    .evaluate((text) => text.textContent);

test("without the setting, results show only their matching lines", async (t) => {
  // @covers setting:results.contextLines
  const page = await panelWith(t, 0, [match(14, 2), match(17, 2)]);
  assert.deepEqual(await codeSection(page), ["# src/payments/client.py", "[14]", "[17]"]);
  assert.equal(await textOf(page, 14), "return self._retry(lambda: self.session.post(request))", "its indent dropped");
});

test("context lines run around each match, and close matches share one run", async (t) => {
  // @covers screen:context-lines setting:results.contextLines
  const page = await panelWith(t, 2, [match(14, 2), match(17, 2), match(31, 2)]);
  assert.deepEqual(await codeSection(page), [
    "# src/payments/client.py",
    "12",
    "13",
    "[14]",
    "15",
    "16",
    "[17]",
    "18",
    "19",
    "⋯",
    "29",
    "30",
    "[31]",
    "32",
    "33",
  ]);
  // The run drops only the indentation every line shares, so the code keeps its shape.
  assert.equal(await textOf(page, 12), "def charge(self, amount, card):");
  assert.equal(await textOf(page, 13), "    request = self._build(amount, card)");
  assert.equal(await textOf(page, 14), "    return self._retry(lambda: self.session.post(request))");
  assert.equal(await textOf(page, 15), "", "a blank line stays");
  assert.equal(await page.locator('[data-ref$=":14"] mark').textContent(), "_retry");
  assert.match(await page.locator('[data-testid="section-code"] .section-title').innerText(), /3 in 1 file/);
  assert.deepEqual(pageErrors(page), []);
});

test("a match inside the last one's context takes its line over instead of showing it twice", async (t) => {
  const lines = { 1: "a = 1", 2: "_retry()", 3: "_retry()", 4: "b = 2", 5: "c = 3", 6: "_retry()", 7: "d = 4" };
  const file = "x.py";
  const page = await panelWith(t, 2, [
    match(2, 2, { file, lines }),
    match(3, 2, { file, lines }),
    match(6, 2, { file, lines }),
  ]);
  assert.deepEqual(await codeSection(page), ["# x.py", "1", "[2]", "[3]", "4", "5", "[6]", "7"]);
});

test("runs join across batches, as Load more appends them", async (t) => {
  const page = await panelWith(t, 1, [match(14, 1)], [match(17, 1)], [match(31, 1)]);
  assert.deepEqual(await codeSection(page), [
    "# src/payments/client.py",
    "13",
    "[14]",
    "15",
    "16",
    "[17]",
    "18",
    "⋯",
    "30",
    "[31]",
    "32",
  ]);
});

test("each file starts its own run, and matches out of line order start a new one", async (t) => {
  const other = { 1: "import _retry", 2: "", 3: "x = 1" };
  const page = await panelWith(t, 1, [
    match(31, 1),
    match(14, 1),
    match(1, 1, { file: "tests/test_client.py", lines: other }),
  ]);
  assert.deepEqual(await codeSection(page), [
    "# src/payments/client.py",
    "30",
    "[31]",
    "32",
    "⋯",
    "13",
    "[14]",
    "15",
    "# tests/test_client.py",
    "[1]",
    "2",
  ]);
});

test("the arrow keys step through matches only, and context lines are hidden from screen readers", async (t) => {
  const page = await panelWith(t, 2, [match(14, 2), match(17, 2), match(31, 2)]);
  const selected = () => page.locator('[data-testid="section-code"] .row.selected').evaluate((row) => row.dataset.ref);
  assert.equal(await selected(), "src/payments/client.py:14", "the first match starts selected");
  await page.keyboard.press("ArrowDown");
  assert.equal(await selected(), "src/payments/client.py:17");
  await page.keyboard.press("ArrowDown");
  assert.equal(await selected(), "src/payments/client.py:31");
  await page.keyboard.press("ArrowUp");
  assert.equal(await selected(), "src/payments/client.py:17");

  const lines = page.locator('[data-testid="context-line"], [data-testid="context-gap"]');
  for (const hidden of await lines.evaluateAll((rows) => rows.map((row) => row.getAttribute("aria-hidden")))) {
    assert.equal(hidden, "true");
  }
  assert.equal(
    await page.locator('[data-testid="context-line"][role], [data-testid="context-line"][data-ref]').count(),
    0,
  );
});

test("a replace preview keeps a match's place among its context lines", async (t) => {
  const lines = { 41: "    def __init__(self):", 42: "        self.retry_policy = RetryPolicy()", 43: "" };
  const item = {
    ...CODE_LINE_RESULT,
    text: lines[42],
    hits: [{ start: 13, end: 25, termIndex: 0 }],
    contextBefore: [lines[41]],
    contextAfter: [lines[43]],
  };
  const page = await openPanel(t);
  await restore(page, [], { contextLines: 1 });
  const seq = await typeAndParse(page, "retry_policy", parsedQuery("retry_policy", textNode("retry_policy", 0)));
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [item] });
  await searchDone(page, seq, { total: 1 });
  const row = page.locator('[data-kind="line"] .text');
  assert.equal(await row.evaluate((text) => text.textContent), "    self.retry_policy = RetryPolicy()");

  await page.click('[data-testid="toggle-replace"]');
  await page.fill('[data-testid="replacement"]', "backoff");
  const request = await waitForSent(page, "replace.preview", { replacement: "backoff" });
  const edits = [{ start: 13, end: 25, newText: "backoff" }];
  await fromHost(page, "replace.plan", {
    seq: request.seq,
    plan: {
      matches: 1,
      truncated: false,
      files: [{ ...item, file: "/w/c.py", lines: [{ line: 42, text: lines[42], edits }] }],
    },
  });
  assert.equal(await row.locator("del").textContent(), "retry_policy");
  assert.equal(await row.locator("ins").textContent(), "backoff");
  const shown = await row.evaluate((text) => text.textContent);
  assert.ok(shown.startsWith("    self."), `"${shown}" is still indented like the lines around it`);
});

test("the Context buttons show the setting and ask the host to change it", async (t) => {
  // @covers msg:context.set screen:context-lines
  const page = await panelWith(t, 0, [match(14, 0)]);
  const options = page.locator('[data-testid="section-code"] [data-testid="context-switch"] button');
  assert.deepEqual(await options.allTextContents(), ["0", "±1", "±2", "±3"]);
  const pressed = () => options.evaluateAll((buttons) => buttons.map((option) => option.ariaPressed));
  assert.deepEqual(await pressed(), ["true", "false", "false", "false"]);
  assert.equal(await page.getByRole("group", { name: "Context lines" }).count(), 1);

  await options.nth(2).click();
  assert.deepEqual(await lastSent(page, "context.set"), { lines: 2 });
  assert.deepEqual(await pressed(), ["false", "false", "true", "false"], "pressed at once");
  assert.equal(
    await page.evaluate(() => document.activeElement?.dataset.testid),
    "query",
    "the box keeps the keys, so ↑↓ still move through the results",
  );

  // The host saves the setting and sends it back, which runs the query again.
  const before = (await sentMessages(page, "query.changed")).length;
  await fromHost(page, "state.restore", {
    text: "_retry",
    recent: [],
    pinned: [],
    settings: { ...UI_SETTINGS, contextLines: 2 },
  });
  const changes = await sentMessages(page, "query.changed");
  assert.equal(changes.length, before + 1, "the query runs again");
  const { seq } = changes.at(-1).payload;
  await fromHost(page, "parse.result", { seq, query: parsedQuery("_retry", textNode("_retry", 0)), completions: [] });
  await fromHost(page, "search.batch", { seq, searchId: "s2", items: [match(14, 2)] });
  assert.deepEqual(await pressed(), ["false", "false", "true", "false"]);
  assert.deepEqual(await codeSection(page), ["# src/payments/client.py", "12", "13", "[14]", "15", "16"]);
});
