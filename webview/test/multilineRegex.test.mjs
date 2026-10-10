// A regex with \n matches across lines: each match is one result row that
// shows every line it spans, the context lines sit around the whole span,
// and the preview marks all of it.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  fromHost,
  lastSent,
  openPanel,
  pageErrors,
  parsedQuery,
  QUERY,
  restore,
  searchDone,
  typeAndParse,
  useBrowser,
  waitForSent,
} from "./harness.mjs";

useBrowser();

const QUERY_TEXT = String.raw`/except TransientError:\n\s*delay/`;
const REGEX = String.raw`except TransientError:\n\s*delay`;

/** The parsed query for QUERY_TEXT: one regex term. */
const parsed = parsedQuery(QUERY_TEXT, {
  kind: "text",
  value: REGEX,
  match: "regex",
  termIndex: 0,
  span: { start: 0, end: QUERY_TEXT.length },
});

/** client.py around its two except clauses. */
const CLIENT = {
  4: "            return self._post()",
  5: "        except TransientError:",
  6: "            delay = self.retry_policy.next_delay()",
  7: "        raise TransientError()",
  11: "            return self._delete()",
  12: "        except TransientError:",
  13: "            delay = 0",
  14: "",
};

/** The result for the match that starts on `line` and ends on the next, with `n` context lines. */
function spanMatch(line, n = 0) {
  const lines = (from, to) =>
    Array.from({ length: to - from + 1 }, (_, index) => CLIENT[from + index]).filter((text) => text !== undefined);
  return {
    ...CODE_LINE_RESULT,
    ref: `client.py:${line}`,
    path: "src/payments/client.py",
    line,
    text: CLIENT[line],
    hits: [{ start: 8, end: 30, termIndex: 0 }],
    spanLines: [{ text: CLIENT[line + 1], hits: [{ start: 12, end: 17, termIndex: 0 }] }],
    endLine: line + 1,
    ...(n > 0 ? { contextBefore: lines(line - n, line - 1), contextAfter: lines(line + 2, line + 1 + n) } : {}),
  };
}

/** A panel showing `items` for QUERY_TEXT, with `contextLines` set. */
async function panelWith(t, items, contextLines = 0) {
  const page = await openPanel(t);
  await restore(page, [], { contextLines });
  const seq = await typeAndParse(page, QUERY_TEXT, parsed);
  await fromHost(page, "search.batch", { seq, searchId: "s1", items });
  await searchDone(page, seq, { total: items.length });
  return page;
}

/** Each code result row as "[5] text | [6] text": its lines' numbers and text, marks in [brackets]. */
function spanRows(page) {
  return page.locator('[data-testid="section-code"] [data-ref]').evaluateAll((rows) =>
    rows.map((row) =>
      [...row.querySelectorAll(".span-line, :scope > .line-number")]
        .map((line) => {
          const number = line.classList.contains("line-number") ? line : line.querySelector(".line-number");
          const text = line.classList.contains("line-number")
            ? row.querySelector(".text")
            : line.querySelector(".text");
          const shown = [...text.childNodes]
            .map((node) => (node.nodeName === "MARK" ? `[${node.textContent}]` : node.textContent))
            .join("");
          return `${number.textContent} ${shown}`;
        })
        .join(" | "),
    ),
  );
}

test("a match across lines is one row showing each of its lines, marked", async (t) => {
  // @covers screen:multiline-regex syntax:regex
  const page = await panelWith(t, [spanMatch(5), spanMatch(12)]);
  assert.deepEqual(await spanRows(page), [
    "5 [except TransientError:] | 6     [delay] = self.retry_policy.next_delay()",
    "12 [except TransientError:] | 13     [delay] = 0",
  ]);
  // One result per match: the header counts two, and ↓ steps from match to match, never into a match's lines.
  assert.match(await page.locator('[data-testid="section-code"] .section-title').innerText(), /2 in 1 file/);
  assert.equal(await page.locator('[data-testid="section-code"] [data-testid="code-group"] .count').innerText(), "2");
  await waitForSent(page, "result.select", { ref: "client.py:5" });
  await page.keyboard.press("ArrowDown");
  await waitForSent(page, "result.select", { ref: "client.py:12" });
  assert.equal(await page.locator('[data-ref="client.py:12"]').getAttribute("aria-selected"), "true");
  assert.deepEqual(pageErrors(page), []);
});

test("a match longer than the lines a result carries says where it ends", async (t) => {
  // @covers screen:multiline-regex
  const long = {
    ...spanMatch(5),
    spanLines: Array.from({ length: 10 }, (_, index) => ({ text: `line ${6 + index}`, hits: [] })),
    endLine: 25,
  };
  const page = await panelWith(t, [long]);
  assert.equal(await page.locator('[data-ref="client.py:5"] .span-line').count(), 12);
  const more = page.locator('[data-ref="client.py:5"] [data-testid="span-more"]');
  assert.equal(await more.locator(".line-number").textContent(), "⋯");
  assert.equal(await more.locator(".text").textContent(), "10 more lines, to line 25");
});

test("context lines sit before a match's first line and after its last", async (t) => {
  // @covers screen:multiline-regex setting:results.contextLines
  const page = await panelWith(t, [spanMatch(5, 1), spanMatch(12, 1)], 1);
  const section = await page.locator('[data-testid="section-code"] .list').evaluate((list) =>
    [...list.querySelectorAll("[data-ref], .context-line, .context-gap")].map((row) => {
      if (row.classList.contains("context-gap")) return "⋯";
      if (!row.dataset.ref) return row.querySelector(".line-number").textContent;
      return `[${[...row.querySelectorAll(".span-line .line-number")].map((number) => number.textContent).join("-")}]`;
    }),
  );
  assert.deepEqual(section, ["4", "[5-6]", "7", "⋯", "11", "[12-13]", "14"]);
  // The run keeps the code's shape: only the indentation every line shares is dropped.
  assert.deepEqual(await spanRows(page), [
    "5 [except TransientError:] | 6     [delay] = self.retry_policy.next_delay()",
    "12 [except TransientError:] | 13     [delay] = 0",
  ]);
  assert.equal(await page.locator('.context-line[data-line="4"] .text').textContent(), "    return self._post()");
});

test("the preview marks every line of the match and names them in its header", async (t) => {
  // @covers screen:multiline-regex msg:preview.result
  const page = await panelWith(t, [spanMatch(5)]);
  await waitForSent(page, "result.select", { ref: "client.py:5" });
  await fromHost(page, "preview.result", {
    ref: "client.py:5",
    preview: {
      kind: "file",
      path: "src/payments/client.py",
      firstLine: 4,
      lines: [CLIENT[4], CLIENT[5], CLIENT[6], CLIENT[7]],
      focusLine: 5,
      focusEndLine: 6,
      hits: [
        { line: 5, ranges: [{ start: 8, end: 30, termIndex: 0 }] },
        { line: 6, ranges: [{ start: 12, end: 17, termIndex: 0 }] },
      ],
      dirtyLines: [],
    },
  });
  const preview = page.locator('[data-testid="preview"]');
  assert.match(await preview.locator(".preview-header").innerText(), /lines 5–6/);
  const focused = await preview.locator(".code-line.focus .line-number").allTextContents();
  assert.deepEqual(focused, ["5", "6"]);
  assert.equal(await preview.locator("mark").count(), 2);
});

test("a single-line preview still names one line", async (t) => {
  const page = await panelWith(t, [spanMatch(5)]);
  await waitForSent(page, "result.select", { ref: "client.py:5" });
  await fromHost(page, "preview.result", {
    ref: "client.py:5",
    preview: {
      kind: "file",
      path: "src/payments/client.py",
      firstLine: 5,
      lines: [CLIENT[5]],
      focusLine: 5,
      hits: [],
      dirtyLines: [],
    },
  });
  assert.match(await page.locator('[data-testid="preview"] .preview-header').innerText(), /line 5(?!–)/);
});

test("Replace explains it can't change a match across lines, which stays marked", async (t) => {
  // @covers screen:multiline-regex msg:replace.plan
  const page = await panelWith(t, [spanMatch(5)]);
  await page.click('[data-testid="toggle-replace"]');
  await page.fill('[data-testid="replacement"]', "wait");
  const request = await waitForSent(page, "replace.preview", { replacement: "wait" });
  await fromHost(page, "replace.plan", {
    seq: request.seq,
    error: String.raw`replace changes one line at a time, so it can't replace a regex with \n`,
  });
  assert.equal(
    await page.textContent('[data-testid="replace-summary"]'),
    String.raw`Replace changes one line at a time, so it can't replace a regex with \n.`,
  );
  assert.equal(await page.isDisabled('[data-testid="replace-all"]'), true);
  assert.deepEqual(await spanRows(page), [
    "5 [except TransientError:] | 6     [delay] = self.retry_policy.next_delay()",
  ]);
  assert.deepEqual(pageErrors(page), []);
});

test("the results glimpse under suggestions shows a match across lines by its first line", async (t) => {
  // @covers screen:multiline-regex
  const page = await openPanel(t);
  await restore(page);
  const typed = `${QUERY_TEXT} s`;
  await page.fill(QUERY, typed);
  const { seq } = await lastSent(page, "query.changed");
  const s = {
    kind: "text",
    value: "s",
    match: "literal",
    termIndex: 1,
    span: { start: typed.length - 1, end: typed.length },
  };
  const root = { kind: "and", children: [parsed.root, s], span: { start: 0, end: typed.length } };
  await fromHost(page, "parse.result", {
    seq,
    query: parsedQuery(typed, root),
    completions: [
      {
        label: "since:",
        detail: "Changed since",
        group: "operator",
        insert: { title: "Insert since:", edits: [{ span: s.span, newText: "since:" }] },
      },
    ],
    searchText: QUERY_TEXT,
  });
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [spanMatch(5)] });
  await searchDone(page, seq, { total: 1 });
  const glimpse = page.locator('[data-testid="results-glimpse"]');
  assert.deepEqual(await glimpse.locator(".row.line .line-number").allTextContents(), ["5"]);
  assert.equal(await glimpse.locator(".span-line").count(), 0);
  assert.deepEqual(pageErrors(page), []);
});
