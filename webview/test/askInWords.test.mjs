// Ask in plain words (board 62): a question ending in ?, or the Ask button,
// goes to the host, the query it becomes lands in the box and runs under a
// line that says what was asked, Broaden asks for a looser query when
// nothing is found, and the line says what Ask needs when it can't run.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fromHost,
  lastSent,
  openPanel,
  pageErrors,
  parsedQuery,
  QUERY,
  restore,
  searchDone,
  sentMessages,
  textNode,
  useBrowser,
} from "./harness.mjs";

useBrowser();

const QUESTION = "which methods use RetryPolicy";
const ANSWER = "kind:method ref:RetryPolicy";

/** A panel with Ask on (unless `settings` says otherwise) and one indexed repo. */
async function askPanel(t, settings) {
  const page = await openPanel(t);
  await restore(page, [], { askEnabled: true, ...settings });
  await fromHost(page, "index.status", {
    repos: [{ repoId: "r1", name: "payments-api", tree: "ready", history: "ready" }],
  });
  return page;
}

/** Answers the newest ask with `query`, and lets its search finish with `total` results. */
async function answered(page, query, total, extra = {}) {
  const { seq } = await lastSent(page, "ask");
  await fromHost(page, "ask.result", { seq, question: QUESTION, query, assumptions: [], ...extra });
  const changed = await lastSent(page, "query.changed");
  assert.equal(changed.text, query, "the query the question became runs");
  await fromHost(page, "parse.result", {
    seq: changed.seq,
    query: parsedQuery(query, textNode(query, 0)),
    completions: [],
  });
  await searchDone(page, changed.seq, { total, ms: 2 });
  return changed.seq;
}

test("Enter on a question asks it, and the answer's query lands in the box and runs", async (t) => {
  // @covers screen:ask-in-words msg:ask msg:ask.result
  const page = await askPanel(t);
  await page.fill(QUERY, `${QUESTION}?`);
  await page.press(QUERY, "Enter");
  assert.deepEqual(await lastSent(page, "ask"), { question: QUESTION, seq: 1 });
  const line = page.locator('[data-testid="asked-line"]');
  assert.match(await line.innerText(), /Asking: which methods use RetryPolicy\?/);
  await answered(page, ANSWER, 2, {
    explanation: "Methods that use RetryPolicy",
    assumptions: ['took "methods" as kind:method'],
  });
  assert.equal(await page.inputValue(QUERY), ANSWER);
  assert.equal(await page.locator('[data-testid="asked-query"]').innerText(), ANSWER);
  assert.equal(
    await page.locator('[data-testid="asked-explanation"]').innerText(),
    'Methods that use RetryPolicy · took "methods" as kind:method',
  );
  assert.match(await line.innerText(), /You asked\s+which methods use RetryPolicy\?/i);
  // Edit question brings the words back as if typed: the host hears them too, so a reopened panel keeps them.
  await page.click('[data-testid="ask-edit"]');
  assert.equal(await page.inputValue(QUERY), `${QUESTION}?`);
  assert.equal((await lastSent(page, "query.changed")).text, `${QUESTION}?`);
  assert.equal(await line.count(), 0);
  assert.deepEqual(pageErrors(page), []);
});

test("Generate query, left of the match toggles, asks the box's words, with or without a ?", async (t) => {
  const page = await askPanel(t);
  const tools = await page
    .locator(".search-bar .tools > *")
    .evaluateAll((nodes) => nodes.map((node) => node.dataset.testid));
  assert.deepEqual(tools.slice(0, 2), ["ask", "toggle-case"]);
  assert.equal((await page.locator('[data-testid="ask"]').innerText()).trim(), "Generate query");
  await page.fill(QUERY, "functions that call charge");
  await page.click('[data-testid="ask"]');
  assert.deepEqual(await lastSent(page, "ask"), { question: "functions that call charge", seq: 1 });
});

test("a box with search words is a query even when it ends in ?, so Enter searches it as typed", async (t) => {
  const page = await askPanel(t);
  await page.fill(QUERY, "type:file match:fuzzy other?");
  await page.press(QUERY, "Enter");
  assert.equal(await lastSent(page, "ask"), undefined, "nothing went to the provider");
  assert.equal(await page.inputValue(QUERY), "type:file match:fuzzy other?", "the ? stays");
  assert.equal((await lastSent(page, "query.changed")).text, "type:file match:fuzzy other?");
  assert.deepEqual(pageErrors(page), []);
});

test("a question too vague to search gets a question back, and the box keeps the question", async (t) => {
  const page = await askPanel(t);
  await page.fill(QUERY, "where are other files?");
  await page.press(QUERY, "Enter");
  const { seq } = await lastSent(page, "ask");
  await fromHost(page, "ask.result", {
    seq,
    question: "where are other files",
    assumptions: [],
    clarify: "Other than which files?",
  });
  const line = page.locator('[data-testid="asked-line"]');
  assert.match(await line.innerText(), /Spelunk asks\s+Other than which files\?/i);
  assert.equal(await page.inputValue(QUERY), "where are other files?", "nothing replaced the question");
  const searched = (await sentMessages(page, "query.changed")).map((message) => message.payload.text);
  assert.ok(
    searched.every((text) => text === "where are other files?"),
    `nothing but the typed words was searched: ${searched.join(" | ")}`,
  );
  assert.equal(await page.locator('[data-testid="ask-search-words"]').count(), 1);
  assert.deepEqual(pageErrors(page), []);
});

test("a query that doesn't parse is shown with its error, so it can be fixed by hand", async (t) => {
  // cspell:ignore methd
  const page = await askPanel(t);
  await page.fill(QUERY, `${QUESTION}?`);
  await page.press(QUERY, "Enter");
  const { seq } = await lastSent(page, "ask");
  await fromHost(page, "ask.result", {
    seq,
    question: QUESTION,
    query: "kind:methd ref:RetryPolicy",
    assumptions: [],
    error: "The query it wrote doesn't work: Use one of: function, method, class, interface, type, other",
  });
  assert.equal(await page.inputValue(QUERY), "kind:methd ref:RetryPolicy");
  assert.match(await page.locator('[data-testid="asked-error"]').innerText(), /doesn't work: Use one of/);
});

test("no results for a question offers Broaden, which asks for a looser query", async (t) => {
  const page = await askPanel(t);
  await page.fill(QUERY, `${QUESTION}?`);
  await page.press(QUERY, "Enter");
  await answered(page, "kind:function ref:charge_card", 0);
  const note = page.locator('[data-testid="broaden-note"]');
  assert.match(await note.innerText(), /Try a looser search for your question\?/);
  await page.click('[data-testid="broaden"]');
  assert.deepEqual(await lastSent(page, "ask"), {
    question: QUESTION,
    seq: 2,
    broaden: "kind:function ref:charge_card",
  });
});

test("without a key the line says so, Set API key asks the host, and the words can be searched instead", async (t) => {
  // @covers msg:ask.setup
  const page = await askPanel(t);
  await page.fill(QUERY, `${QUESTION}?`);
  await page.press(QUERY, "Enter");
  const { seq } = await lastSent(page, "ask");
  await fromHost(page, "ask.result", { seq, question: QUESTION, setup: "noKey" });
  const line = page.locator('[data-testid="asked-line"]');
  assert.match(await line.innerText(), /needs an Anthropic API key/);
  await page.click('[data-testid="ask-setup"]');
  assert.deepEqual(await lastSent(page, "ask.setup"), { action: "key" });
  await page.click('[data-testid="ask-search-words"]');
  assert.equal((await lastSent(page, "query.changed")).text, `${QUESTION}?`);
  assert.equal(await line.count(), 0);
});

test("with Ask off, a question is searched as words, and the button says Ask is off", async (t) => {
  const page = await askPanel(t, { askEnabled: false });
  await page.fill(QUERY, `${QUESTION}?`);
  await page.press(QUERY, "Enter");
  assert.equal(await lastSent(page, "ask"), undefined);
  assert.equal((await lastSent(page, "query.changed")).text, `${QUESTION}?`);
  await page.click('[data-testid="ask"]');
  const line = page.locator('[data-testid="asked-line"]');
  assert.match(await line.innerText(), /Ask is off/);
  await page.click('[data-testid="ask-setup"]');
  assert.deepEqual(await lastSent(page, "ask.setup"), { action: "settings" });
});
