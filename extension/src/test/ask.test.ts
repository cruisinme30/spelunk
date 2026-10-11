// Ask: a question becomes a query through a provider, the parser checks it,
// and the panel hears the outcome.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  type AskAnswer,
  Asker,
  type AskProvider,
  type AskRequest,
  EXAMPLES,
  effortFor,
  effortLevelsOf,
  fakeProviderFromEnvironment,
  parseAnswer,
  systemPrompt,
  userPrompt,
} from "../ask";
import type Anthropic from "@anthropic-ai/sdk";
import type { ParsedQuery } from "../protocol.gen";
import type { AskEffort } from "../settings";

/** A parsed query with no diagnostics, or with one error. */
function parsed(raw: string, error?: string): ParsedQuery {
  return {
    version: 1,
    raw,
    root: { kind: "text", value: raw, match: "literal", termIndex: 0, span: { start: 0, end: raw.length } },
    globals: { case: null, count: null, type: null },
    mode: "workingTree",
    diagnostics: error
      ? [{ severity: "error", code: "unknown_operator", message: error, span: { start: 0, end: 1 }, fixes: [] }]
      : [],
  };
}

/** A provider that answers from a script and records what it was asked. */
function scriptedProvider(answers: (string | AskAnswer)[]) {
  const requests: AskRequest[] = [];
  const provider: AskProvider = {
    translate: (request) => {
      requests.push(request);
      const next = answers.shift();
      if (next === undefined) return Promise.reject(new Error("out of answers"));
      return Promise.resolve(typeof next === "string" ? { query: next, explanation: "x", assumptions: [] } : next);
    },
  };
  return { provider, requests };
}

/** An Asker over `provider`, with a parser that rejects queries listed in `broken` with that message. */
function asker(provider?: AskProvider, broken: Record<string, string> = {}, shareNames = true) {
  const parsedTexts: string[] = [];
  const instance = new Asker({
    parse: (text) => {
      parsedTexts.push(text);
      return Promise.resolve(parsed(text, broken[text]));
    },
    guide: () => Promise.resolve("symbol: kind: ref:"),
    setup: () => Promise.resolve(provider ? { kind: "ready", provider } : { kind: "noKey" }),
    names: () =>
      Promise.resolve(
        shareNames ? { repos: ["payments-api"], teams: ["@acme/payments-core"] } : { repos: [], teams: [] },
      ),
    today: () => "2026-10-11",
  });
  return { instance, parsedTexts };
}

test("a question becomes the provider's query, checked by the parser", async () => {
  // @covers msg:ask msg:ask.result rpc:query/guide
  const { provider, requests } = scriptedProvider([
    { query: "kind:method ref:RetryPolicy", explanation: "Methods that use RetryPolicy", assumptions: [] },
  ]);
  const { instance, parsedTexts } = asker(provider);
  const outcome = await instance.ask("which methods use RetryPolicy");
  assert.deepEqual(outcome, {
    query: "kind:method ref:RetryPolicy",
    explanation: "Methods that use RetryPolicy",
    assumptions: [],
  });
  assert.deepEqual(parsedTexts, ["kind:method ref:RetryPolicy"], "the parser checked the query");
  const request = requests[0];
  assert.ok(request);
  assert.equal(request.question, "which methods use RetryPolicy");
  assert.equal(request.guide, "symbol: kind: ref:");
  assert.equal(request.today, "2026-10-11");
  assert.deepEqual(request.repos, ["payments-api"]);
  assert.deepEqual(request.teams, ["@acme/payments-core"]);
  assert.equal(request.retry, undefined);
});

test("a question too vague to search gets a question back, and nothing is searched", async () => {
  const { provider } = scriptedProvider([
    { query: "", explanation: "", assumptions: [], clarify: "Other than which files?" },
  ]);
  const { instance, parsedTexts } = asker(provider);
  assert.deepEqual(await instance.ask("Where are other files"), { clarify: "Other than which files?" });
  assert.deepEqual(parsedTexts, [], "no query, so nothing to check");
  const system = systemPrompt({ guide: "g", repos: [], teams: [] });
  assert.match(system, /Never guess what the question leaves out/);
  assert.match(system, /wants code, not text that\s+mentions it: add in:code/);
  assert.match(system, /never search terms/);
});

test("a query that doesn't parse is sent back once for a repair", async () => {
  // cspell:ignore symbl simbol
  const { provider, requests } = scriptedProvider(["symbl:Retry", "symbol:Retry"]);
  const { instance } = asker(provider, { "symbl:Retry": "Unknown search word symbl:" });
  const outcome = await instance.ask("where is Retry defined");
  assert.equal(outcome.query, "symbol:Retry");
  assert.equal(outcome.error, undefined);
  assert.deepEqual(requests[1]?.retry, {
    query: "symbl:Retry",
    problem: "parse",
    detail: "Unknown search word symbl:",
  });
});

test("a query still broken after the repair is shown with its error", async () => {
  const { provider, requests } = scriptedProvider(["symbl:Retry", "simbol:Retry"]);
  const { instance } = asker(provider, {
    "symbl:Retry": "Unknown search word symbl:",
    "simbol:Retry": "Unknown search word simbol:",
  });
  const outcome = await instance.ask("where is Retry defined");
  assert.equal(outcome.query, "simbol:Retry", "the panel shows the last query, so the user can fix it");
  assert.equal(outcome.error, "The query it wrote doesn't work: Unknown search word simbol:");
  assert.equal(requests.length, 2, "one repair, not a loop");
});

test("broaden asks for one looser query from the one that found nothing", async () => {
  const { provider, requests } = scriptedProvider([
    { query: "ref:charge", explanation: "Uses of charge", assumptions: ["dropped kind:function"] },
  ]);
  const { instance } = asker(provider);
  const outcome = await instance.ask("functions that call charge_card", "kind:function ref:charge_card");
  assert.equal(outcome.query, "ref:charge");
  assert.deepEqual(outcome.assumptions, ["dropped kind:function"]);
  assert.deepEqual(requests[0]?.retry, { query: "kind:function ref:charge_card", problem: "noResults", detail: "" });
});

test("without a key, or with the provider failing, the panel hears why", async () => {
  const { instance } = asker();
  assert.deepEqual(await instance.ask("anything"), { setup: "noKey" });
  const failing: AskProvider = {
    translate: () => Promise.reject(new Error("Couldn't reach Anthropic; check the network")),
  };
  assert.deepEqual(await asker(failing).instance.ask("anything"), {
    error: "Couldn't reach Anthropic; check the network",
  });
});

test("names stay home when the user keeps them back", async () => {
  const { provider, requests } = scriptedProvider(["retry"]);
  await asker(provider, {}, false).instance.ask("retries");
  const [request] = requests;
  assert.ok(request);
  assert.deepEqual(request.repos, []);
  assert.deepEqual(request.teams, []);
  const prompt = systemPrompt({ guide: "g", repos: [], teams: [] });
  assert.match(prompt, /No repo or team names were shared/);
});

test("the prompts carry the guide, every example, the names, today and the retry", () => {
  const system = systemPrompt({ guide: "THE GUIDE", repos: ["web", "api"], teams: ["@acme/core"] });
  assert.match(system, /THE GUIDE/);
  assert.match(system, /Repos open in the editor \(for repo:\): web, api/);
  assert.match(system, /Owner teams in CODEOWNERS \(for owner:\): @acme\/core/);
  for (const [question, query] of EXAMPLES) assert.ok(system.includes(`${question} → ${query}`), question);
  const user = userPrompt({ question: "who changed timeouts?", today: "2026-10-11" });
  assert.equal(user, "Today is 2026-10-11.\nQuestion: who changed timeouts?");
  const repair = userPrompt({
    question: "q",
    today: "2026-10-11",
    retry: { query: "bad:x", problem: "parse", detail: "Unknown search word bad:" },
  });
  assert.match(repair, /rejected the query "bad:x": Unknown search word bad:/);
  const broaden = userPrompt({
    question: "q",
    today: "2026-10-11",
    retry: { query: "kind:function ref:x", problem: "noResults", detail: "" },
  });
  assert.match(broaden, /found nothing/);
});

test("a provider's answer is read out of whatever wraps it", () => {
  assert.deepEqual(parseAnswer('{"query":"symbol:X","explanation":"e","assumptions":["a"]}'), {
    query: "symbol:X",
    explanation: "e",
    assumptions: ["a"],
  });
  assert.deepEqual(parseAnswer('Sure:\n```json\n{"query":" ref:X ","explanation":"","assumptions":[]}\n```'), {
    query: "ref:X",
    explanation: "",
    assumptions: [],
  });
  assert.deepEqual(parseAnswer('{"query":"","explanation":"","assumptions":[],"clarify":" Which bug? "}'), {
    query: "",
    explanation: "",
    assumptions: [],
    clarify: "Which bug?",
  });
  // A query wins over a stray question back.
  assert.equal(parseAnswer('{"query":"ref:X","explanation":"","assumptions":[],"clarify":"?"}').clarify, undefined);
  assert.throws(() => parseAnswer("no json here"), /without a query/);
  assert.throws(() => parseAnswer('{"explanation":"e","assumptions":[]}'), /without a query/);
});

test("the effort sent is the one asked for when the model takes it, else the nearest, or none", () => {
  // @covers setting:ask.effort
  const all = new Set<AskEffort>(["low", "medium", "high", "max"]);
  assert.deepEqual(effortFor(all, "high"), { effort: "high" });
  // Claude 4.5 and older take no effort at all.
  assert.deepEqual(effortFor(new Set(), "high"), {});
  // A model without "max" gets the nearest level it has.
  assert.deepEqual(effortFor(new Set<AskEffort>(["low", "medium", "high"]), "max"), { effort: "high" });
  assert.deepEqual(effortFor(new Set<AskEffort>(["medium"]), "low"), { effort: "medium" });
  // When Anthropic couldn't be asked, the setting goes as is.
  assert.deepEqual(effortFor(undefined, "medium"), { effort: "medium" });
});

const YES = { supported: true };

test("the effort levels of a model come from Anthropic's models API, once per model", async () => {
  // @covers setting:ask.effort
  let calls = 0;
  const client = {
    models: {
      retrieve: (id: string) => {
        calls += 1;
        if (id === "unknown") return Promise.reject(new Error("not found"));
        return Promise.resolve({
          capabilities: {
            effort: { low: YES, medium: YES, high: YES, max: id === "big" ? YES : { supported: false } },
          },
        });
      },
    },
  } as unknown as Pick<Anthropic, "models">;
  assert.deepEqual([...((await effortLevelsOf(client, "big")) ?? [])], ["low", "medium", "high", "max"]);
  assert.deepEqual([...((await effortLevelsOf(client, "small")) ?? [])], ["low", "medium", "high"]);
  assert.equal(await effortLevelsOf(client, "unknown"), undefined);
  await effortLevelsOf(client, "big");
  assert.equal(calls, 3, "each model is asked about once");
});

test("the fake provider of test builds answers from SPELUNK_ASK_FAKE, by question and by retry", async () => {
  const provider = fakeProviderFromEnvironment({
    SPELUNK_ASK_FAKE: JSON.stringify({
      "q?": "symbol:Q",
      "q? [parse]": { query: "symbol:Q2", explanation: "fixed", assumptions: [] },
    }),
  });
  assert.ok(provider);
  const base = { guide: "", today: "", repos: [], teams: [] };
  assert.equal((await provider.translate({ ...base, question: "q?" })).query, "symbol:Q");
  const repaired = await provider.translate({
    ...base,
    question: "q?",
    retry: { query: "x", problem: "parse", detail: "" },
  });
  assert.equal(repaired.query, "symbol:Q2");
  await assert.rejects(provider.translate({ ...base, question: "unknown" }), /no fake answer/);
  assert.equal(fakeProviderFromEnvironment({}), undefined);
});
