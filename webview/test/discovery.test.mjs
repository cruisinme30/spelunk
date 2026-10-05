// Discovering the query language: operator suggestions, the repo menu, the
// type:file note and the help page.
// @covers msg:help.open msg:help.try msg:settings.open
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fromHost,
  lastSent,
  openHelp,
  openPanel,
  parsedQuery,
  QUERY,
  restore,
  searchDone,
  sentMessages,
  textNode,
  typeAndParse,
  useBrowser,
} from "./harness.mjs";

useBrowser();

/** An operator suggestion that replaces `start`..`end` with `label`. */
const operatorCompletion = (label, detail, start, end) => ({
  label,
  detail,
  group: "operator",
  insert: { title: `Insert ${label}`, edits: [{ span: { start, end }, newText: label }] },
});

/** A code-line result for "timeout". */
const TIMEOUT_LINE_RESULT = {
  kind: "line",
  ref: "l1",
  repoId: "r1",
  path: "a.py",
  line: 43,
  text: "self.timeout = 10",
  hits: [{ start: 5, end: 12, termIndex: 0 }],
};

/** "timeout s" with since: and sym: offered for the "s", as the daemon answers it. */
async function suggestingOperators(t) {
  const page = await openPanel(t);
  await restore(page);
  await page.fill(QUERY, "timeout s");
  const { seq } = await lastSent(page, "query.changed");
  const root = { kind: "and", children: [textNode("timeout", 0), textNode("s", 8, 1)], span: { start: 0, end: 9 } };
  await fromHost(page, "parse.result", {
    seq,
    query: parsedQuery("timeout s", root),
    completions: [
      operatorCompletion("since:", "Only changes inside a time window", 8, 9),
      operatorCompletion("sym:", "Symbol definitions", 8, 9),
    ],
    searchText: "timeout",
  });
  await fromHost(page, "search.batch", { seq, searchId: "s1", items: [TIMEOUT_LINE_RESULT] });
  await searchDone(page, seq, { total: 41, ms: 2 });
  return { page, seq };
}

test("operator suggestions explain each operator and keep the results in view", async (t) => {
  // @covers screen:operator-suggestions
  const { page } = await suggestingOperators(t);
  const options = page.locator('[data-testid="completion"]');
  assert.equal(await options.count(), 2);
  const groupTitle = page.locator('[data-testid="completions"] [role="presentation"]').first();
  assert.match(await groupTitle.innerText(), /starting with “s”/i);
  const since = await options.first().innerText();
  assert.match(since, /since:/);
  assert.match(since, /30d/, "the selected operator shows its example values");
  assert.match(
    await page.locator('[data-testid="search-as-typed"]').innerText(),
    /Search for timeout and s as plain text/,
  );
  const glimpse = await page.locator('[data-testid="results-glimpse"]').innerText();
  assert.match(glimpse, /Results for timeout keep updating/i);
  assert.match(glimpse, /41 matches/i);
  assert.match(await page.locator('[data-testid="keys"]').innerText(), /insert operator/);
});

test("the case: suggestion shows its description and values like every other operator", async (t) => {
  const page = await openPanel(t);
  await restore(page);
  await page.fill(QUERY, "ca");
  const { seq } = await lastSent(page, "query.changed");
  await fromHost(page, "parse.result", {
    seq,
    query: parsedQuery("ca", textNode("ca", 0)),
    completions: [operatorCompletion("case:", "Match case", 0, 2)],
  });
  const option = await page.locator('[data-testid="completion"]').first().innerText();
  assert.match(option, /Case-sensitive or not/);
  assert.match(option, /\byes\b[\s\S]*\bno\b/);
});

/** A since: value suggestion as the daemon sends it. */
const sinceValue = ({ label, detail, context, note, section }) => ({
  label,
  detail,
  context,
  note,
  ...(section ? { section } : {}),
  group: "value",
  insert: { title: `Insert since:${label}`, edits: [{ span: { start: 8, end: 14 }, newText: `since:${label} ` }] },
});

test("since: lists time windows in groups, with when each starts and what changed", async (t) => {
  // @covers screen:since-values
  const page = await openPanel(t);
  await restore(page);
  await page.fill(QUERY, "timeout since:");
  const { seq } = await lastSent(page, "query.changed");
  const since = { kind: "op", op: "since", value: "", match: "literal", span: { start: 8, end: 14 } };
  await fromHost(page, "parse.result", {
    seq,
    query: parsedQuery("timeout since:", {
      kind: "and",
      children: [textNode("timeout", 0), since],
      span: { start: 0, end: 14 },
    }),
    completions: [
      sinceValue({
        label: "today",
        detail: "Today",
        context: "Since midnight",
        note: "5 files changed",
        section: "Calendar days",
      }),
      sinceValue({
        label: "yesterday",
        detail: "Yesterday and today",
        context: "Since Fri, Oct 2, 00:00",
        note: "9 files changed",
      }),
      sinceValue({
        label: "2h",
        detail: "Last 2 hours",
        context: "Since 08:04",
        note: "2 files changed",
        section: "The last few hours",
      }),
    ],
  });
  const list = page.locator("#completions");
  assert.equal(await list.locator(".completion-group").first().innerText(), "TIME WINDOWS");
  assert.deepEqual(await list.locator(".completion-section").allInnerTexts(), ["CALENDAR DAYS", "THE LAST FEW HOURS"]);
  const today = await page.locator('[data-testid="completion"]').first().innerText();
  assert.match(today, /today[\s\S]*Today[\s\S]*Since midnight[\s\S]*5 files changed/);
  assert.equal(await list.locator(".completion-operator.tone-history").count(), 3, "values take since:'s color");
  assert.match(await page.locator('[data-testid="value-hint"]').innerText(), /m is months/);
  await page.keyboard.press("Tab");
  assert.equal(await page.inputValue(QUERY), "timeout since:today ");
});

test("Tab inserts the suggested operator at the cursor", async (t) => {
  // @covers screen:operator-suggestions
  const { page } = await suggestingOperators(t);
  await page.keyboard.press("Tab");
  assert.equal(await page.inputValue(QUERY), "timeout since:");
  assert.equal((await lastSent(page, "query.changed")).text, "timeout since:");
});

test("Enter or Esc closes the suggestions and searches the word as typed", async (t) => {
  for (const key of ["Enter", "Escape"]) {
    const { page } = await suggestingOperators(t);
    await page.keyboard.press(key);
    assert.equal(await page.locator('[data-testid="completions"]').isVisible(), false, key);
    const message = await lastSent(page, "query.changed");
    assert.deepEqual([message.text, message.asTyped], ["timeout s", true], key);
    assert.equal((await sentMessages(page, "panel.close")).length, 0, `${key} doesn't close the panel`);
  }
});

test("? while suggesting opens the help page", async (t) => {
  const { page } = await suggestingOperators(t);
  await page.keyboard.press("?");
  assert.equal((await sentMessages(page, "help.open")).length, 1);
  assert.equal(await page.inputValue(QUERY), "timeout s");
});

const REPOS = [
  { repoId: "a", name: "payments-api", tree: "ready", history: "off" },
  { repoId: "w", name: "web-checkout", tree: "ready", history: "off" },
];

test("the repo menu scopes the query to one repo, or back to all", async (t) => {
  // @covers op:repo
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", { repos: REPOS });
  await typeAndParse(page, "timeout", parsedQuery("timeout", textNode("timeout", 0)));
  assert.equal(await page.locator('[data-testid="repos"]').innerText(), "All repos · 2");

  await page.click('[data-testid="repos"]');
  const options = page.locator('[data-testid="repo-option"]');
  const labels = (await options.allInnerTexts()).map((text) => text.replaceAll(/\s+/g, " ").trim());
  assert.deepEqual(labels, ["✓ All repos 2", "payments-api", "web-checkout"]);
  await options.nth(2).click();
  assert.equal(await page.inputValue(QUERY), "repo:web-checkout timeout");
  assert.equal(await page.locator('[data-testid="repo-menu"]').isVisible(), false);

  const scoped = {
    kind: "op",
    op: "repo",
    value: "web-checkout",
    match: "literal",
    span: { start: 0, end: 17 },
    resolved: { label: "web-checkout" },
  };
  const { seq: scopedSeq } = await lastSent(page, "query.changed");
  const root = { kind: "and", children: [scoped, textNode("timeout", 18)], span: { start: 0, end: 25 } };
  await fromHost(page, "parse.result", {
    seq: scopedSeq,
    query: parsedQuery("repo:web-checkout timeout", root),
    completions: [],
  });
  assert.equal(await page.locator('[data-testid="repos"]').innerText(), "web-checkout");

  await page.click('[data-testid="repos"]');
  await options.first().click();
  assert.equal(await page.inputValue(QUERY), "timeout");
});

test("a type:file note counts the code matches for the query's words and offers them", async (t) => {
  // @covers screen:file-names-only
  const page = await openPanel(t);
  await restore(page);
  const type = { kind: "op", op: "type", value: "file", match: "literal", span: { start: 0, end: 9 } };
  const root = { kind: "and", children: [type, textNode("retry", 10)], span: { start: 0, end: 15 } };
  const seq = await typeAndParse(
    page,
    "type:file retry",
    parsedQuery("type:file retry", root, { globals: { case: null, count: null, type: "file" } }),
  );
  const chips = await page.locator('[data-testid="chips"]').innerText();
  assert.match(chips, /file names only/);
  assert.match(chips, /name\s*retry/);
  const undo = { title: "Remove type:file", edits: [{ span: { start: 0, end: 10 }, newText: "" }] };
  await searchDone(page, seq, { hidden: [{ reason: "type", filter: "type:file", count: 38, unit: "matches", undo }] });
  const note = page.locator('[data-testid="hidden-notes"] .note');
  assert.equal(await note.locator("span").first().innerText(), "38 code matches for retry hidden by type:file");
  await note.locator('[data-testid="show-hidden"]').click();
  assert.equal(await page.inputValue(QUERY), "retry");
});

test("the help page lists every operator with an example, and Try runs it", async (t) => {
  // @covers screen:help-page
  const page = await openHelp(t);
  assert.equal(await page.locator('[data-testid="operator-row"]').count(), 19);
  assert.equal(await page.locator('[data-testid="example"]').count(), 5);
  // Each operator reads by its full name, with its short name beside it.
  assert.deepEqual(await page.locator('[data-testid="short-name"]').allTextContents(), [
    "c:",
    "w:",
    "f:",
    "r:",
    "l:",
    "t:",
    "s:",
    "i:",
    "a:",
    "m:",
    "d:",
    "n:",
    "o:",
  ]);
  await page.locator('[data-testid="operator-row"]').first().locator('[data-testid="try"]').click();
  assert.deepEqual(await lastSent(page, "help.try"), { query: "case:yes RetryPolicy" });
  await page.click('[data-testid="open-settings"]');
  assert.equal((await sentMessages(page, "settings.open")).length, 1);
});

test("a symbol search offers the text search, and no definitions say why", async (t) => {
  // @covers screen:symbol-definitions screen:no-results-while-indexing
  const page = await openPanel(t);
  await restore(page);
  await fromHost(page, "index.status", {
    repos: [{ repoId: "web", name: "web-checkout", tree: "indexing", history: "queued", progress: 0.64 }],
  });
  const text = "case:yes sym:retrypolicy";
  const caseNode = { kind: "op", op: "case", value: "yes", match: "literal", span: { start: 0, end: 8 } };
  const sym = { kind: "op", op: "sym", value: "retrypolicy", match: "literal", span: { start: 9, end: 24 } };
  const root = { kind: "and", children: [caseNode, sym], span: { start: 0, end: 24 } };
  const seq = await typeAndParse(
    page,
    text,
    parsedQuery(text, root, { globals: { case: "yes", count: null, type: null } }),
  );
  const ignoreCase = { title: "Ignore case", edits: [{ span: { start: 0, end: 9 }, newText: "" }] };
  const asText = {
    title: "Search retrypolicy as text",
    edits: [{ span: { start: 9, end: 24 }, newText: "retrypolicy" }],
  };
  await searchDone(page, seq, {
    hidden: [
      { reason: "case", filter: "case:yes", count: 4, unit: "definitions", undo: ignoreCase },
      { reason: "symbol", filter: "sym:retrypolicy", count: 6, unit: "matches", undo: asText },
    ],
  });
  assert.match(await page.locator('[data-testid="indexing-banner"]').innerText(), /Indexing web-checkout · 64%/);
  const none = page.locator('[data-testid="no-results"]');
  assert.match(await none.innerText(), /No symbol is named retrypolicy/);
  assert.match(await none.innerText(), /case:yes is on, so capital letters have to match/);
  const notes = page.locator('[data-testid="hidden-notes"] .note');
  assert.equal(await notes.nth(0).locator("span").first().innerText(), "4 definitions hidden by case:yes");
  assert.equal(await notes.nth(1).locator('[data-testid="show-hidden"]').innerText(), "Search retrypolicy as text · 6");
  await notes.nth(1).locator('[data-testid="show-hidden"]').click();
  assert.equal(await page.inputValue(QUERY), "case:yes retrypolicy");
});
