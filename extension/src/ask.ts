// Ask: a question in plain words becomes a Spelunk query. A provider (an
// LLM) writes the query from the query language's guide and a few worked
// examples; the daemon's parser then checks it, one repair is asked for
// when it doesn't parse, and the panel shows the query before it runs. Only
// the question, the repo names and the owner team names leave the machine,
// never any code.
import Anthropic from "@anthropic-ai/sdk";
import type { AskResultMessage, ParsedQuery } from "./protocol.gen";
import { ASK_EFFORTS, type AskEffort } from "./settings";

/** What a provider is asked for: the question, and the context the query may use. */
export interface AskRequest {
  question: string;
  /** The query language, as query/guide describes it. */
  guide: string;
  /** Today, as YYYY-MM-DD, for "this week" and the like. */
  today: string;
  /** The repo names open in the window, for repo:, when the user shares them. */
  repos: string[];
  /** The CODEOWNERS team names, for owner:, when the user shares them. */
  teams: string[];
  /** A query the provider wrote before and what went wrong with it: it is asked to write a better one. */
  retry?: { query: string; problem: "parse" | "noResults"; detail: string };
}

/** A provider's answer. */
export interface AskAnswer {
  query: string;
  explanation: string;
  assumptions: string[];
}

/** Something that turns a question into a query: an LLM, or a fake in tests. */
export interface AskProvider {
  translate(request: AskRequest, signal?: AbortSignal): Promise<AskAnswer>;
}

/** Whether Ask can run: off in settings, no API key yet, or ready with its provider. */
export type AskSetup = { kind: "off" } | { kind: "noKey" } | { kind: "ready"; provider: AskProvider };

/** A question's outcome, as the panel hears it (AskResultMessage without its seq and question). */
export type AskOutcome = Omit<AskResultMessage, "seq" | "question">;

/** What the host gives an Asker: the daemon's parser and guide, the settings, and the names to share. */
export interface AskDependencies {
  parse(text: string): Promise<ParsedQuery>;
  guide(): Promise<string>;
  setup(): Promise<AskSetup>;
  /** The repo and team names to send, or none when the user keeps them back. */
  names(): Promise<{ repos: string[]; teams: string[] }>;
  today?: () => string;
}

/** Worked examples from the wiki's Recipes page, so the provider writes queries the way the docs do. */
export const EXAMPLES: readonly [question: string, query: string][] = [
  ["Where is RetryPolicy defined?", "symbol:RetryPolicy"],
  ["Every interface whose name has Retry", "kind:interface symbol:Retry"],
  ["Every Go function whose name has parse", "kind:function parse lang:go"],
  ["Where is RetryPolicy used, not defined?", "ref:RetryPolicy"],
  ["Which functions use kmalloc?", "kind:function ref:kmalloc"],
  ["Which methods mention timeout?", "kind:method content:timeout"],
  ["Every call of backoff with a tries argument", 'struct:"backoff(:[call], tries=:[n])"'],
  ["Python files with retry in the name", "type:file language:python retry"],
  ["Lines that mention retry, without the files named after it", "content:retry"],
  ["What do the comments say about retrying?", "retry in:comments"],
  ["The error message users see about the budget", '"budget spent" in:strings'],
  ["Retries outside the tests", "retry -is:test"],
  ["Timeouts outside vendored code, either spelling", "(timeout OR time_out) -file:vendor/"],
  ["Who introduced retry_policy?", "type:added retry_policy"],
  ["When did retry_after go away?", "type:removed retry_after"],
  ["What did Jane change in the tests?", "author:jane is:test"],
  ["Tests Jane changed this month that mention a timeout", String.raw`author:jane since:30d file:_test\.py$ timeout`],
  ["Flaky-test fixes in the web repo", 'message:"fix flaky" repo:web'],
  ["Where was retry_after used in the v1.0.0 release?", "at:v1.0.0 retry_after"],
  ["How did RetryPolicy look six months ago?", "at:6m RetryPolicy"],
  ["TODOs in what I haven't committed yet", "is:changed TODO"],
  ["Retries in the files open in my editor", "is:open retry"],
  ["Retries in the files the payments team owns", "retry owner:payments-core"],
  ["Files nobody owns yet", "type:file owner:none"],
  ["Retry results in the payments repo only", "repo:payments retry"],
  ["Commits about timeouts in the last two weeks", "type:commit since:2w timeout"],
];

/** The system prompt: the rules, the guide, the examples and the names. It is the same for every question in a window, so it caches. */
export function systemPrompt(request: Pick<AskRequest, "guide" | "repos" | "teams">): string {
  const lines = [
    "You turn a question about a codebase into one query in Spelunk's search language; the user's editor then runs",
    "the query and shows it to them. Answer only with the JSON asked for.",
    "",
    "Rules:",
    "- Use only the search words the guide lists, spelled as it spells them. Spaces mean AND; OR with parentheses",
    "  joins alternatives; - in front leaves something out.",
    "- Keep the question's names (identifiers, paths, people, repos) exactly as written, and never invent any.",
    "- Pick the most specific form: a definition by name is symbol:; its uses are ref:; the functions (or methods,",
    "  classes) that use a name are kind:function ref:Name; who changed something or when is a commit search",
    "  (type:commit with author:, since:, until:, message:); what added or removed a line is type:added or",
    "  type:removed; everything else searches the current files.",
    '- Read spans of time plainly: "last month" is since:30d, "this week" since:7d, "yesterday" since:yesterday,',
    "  and say each such reading in assumptions.",
    "- explanation: one short sentence in plain words on what the query finds. assumptions: each reading you had",
    "  to choose; an empty list when none.",
    "",
    "The query language:",
    request.guide.trim(),
    "",
    "Questions and the queries they become:",
    ...EXAMPLES.map(([question, query]) => `- ${question} → ${query}`),
  ];
  if (request.repos.length > 0) lines.push("", `Repos open in the editor (for repo:): ${request.repos.join(", ")}`);
  if (request.teams.length > 0) lines.push(`Owner teams in CODEOWNERS (for owner:): ${request.teams.join(", ")}`);
  if (request.repos.length === 0 && request.teams.length === 0) {
    lines.push("", "No repo or team names were shared: don't use repo: or owner: unless the question names one.");
  }
  return lines.join("\n");
}

/** The user turn: today, the question, and what to fix when a query is being retried. */
export function userPrompt(request: Pick<AskRequest, "question" | "today" | "retry">): string {
  const lines = [`Today is ${request.today}.`, `Question: ${request.question}`];
  if (request.retry?.problem === "parse") {
    lines.push(
      `Spelunk rejected the query ${JSON.stringify(request.retry.query)}: ${request.retry.detail}`,
      "Write a corrected query.",
    );
  } else if (request.retry) {
    lines.push(
      `The query ${JSON.stringify(request.retry.query)} found nothing.`,
      "Write one looser query for the same question: fewer words, part of a name, or a filter dropped. Say in",
      "assumptions what you loosened.",
    );
  }
  return lines.join("\n");
}

/** The shape the provider answers in. */
const ANSWER_SCHEMA = {
  type: "object",
  properties: {
    query: { type: "string", description: "The Spelunk query" },
    explanation: { type: "string", description: "One short sentence on what the query finds" },
    assumptions: { type: "array", items: { type: "string" }, description: "Each reading that had to be chosen" },
  },
  required: ["query", "explanation", "assumptions"],
  additionalProperties: false,
} as const;

/** Reads an answer out of the text a provider returned, however it wrapped it. */
export function parseAnswer(text: string): AskAnswer {
  const start = text.indexOf("{");
  const end = text.lastIndexOf("}");
  if (start === -1 || end < start) throw new Error("the provider answered without a query");
  const raw: unknown = JSON.parse(text.slice(start, end + 1));
  if (typeof raw !== "object" || raw === null) throw new Error("the provider's answer isn't an object");
  const answer = raw as Record<string, unknown>;
  const query = typeof answer["query"] === "string" ? answer["query"].trim() : "";
  if (query === "") throw new Error("the provider answered without a query");
  const assumptions = Array.isArray(answer["assumptions"])
    ? answer["assumptions"].filter((item): item is string => typeof item === "string" && item.trim() !== "")
    : [];
  return { query, explanation: typeof answer["explanation"] === "string" ? answer["explanation"] : "", assumptions };
}

/** How long one provider call may take before Ask gives up on it. */
const PROVIDER_TIMEOUT_MS = 45_000;

/** The effort levels a model takes, as the models API reports them; undefined when it couldn't be asked. */
const effortLevels = new Map<string, Promise<Set<AskEffort> | undefined>>();

/** The effort levels `model` takes, asked of Anthropic once per model and kept for the session. */
export function effortLevelsOf(client: Pick<Anthropic, "models">, model: string): Promise<Set<AskEffort> | undefined> {
  let levels = effortLevels.get(model);
  if (!levels) {
    levels = client.models.retrieve(model).then(
      (info) => {
        const effort = info.capabilities?.effort;
        return effort ? new Set(ASK_EFFORTS.filter((level) => effort[level].supported)) : new Set<AskEffort>();
      },
      () => undefined, // eslint-disable-line unicorn/no-useless-undefined -- the levels couldn't be asked
    );
    effortLevels.set(model, levels);
  }
  return levels;
}

/**
 * The effort to send: the one asked for when the model takes it, else the
 * nearest level it does take (Claude 4.5 and older take none). When Anthropic
 * couldn't say, the one asked for goes as is.
 */
export function effortFor(levels: Set<AskEffort> | undefined, effort: AskEffort): { effort?: AskEffort } {
  if (levels === undefined) return { effort };
  if (levels.size === 0) return {};
  if (levels.has(effort)) return { effort };
  const nearest = [...ASK_EFFORTS].sort(
    (a, b) =>
      Math.abs(ASK_EFFORTS.indexOf(a) - ASK_EFFORTS.indexOf(effort)) -
      Math.abs(ASK_EFFORTS.indexOf(b) - ASK_EFFORTS.indexOf(effort)),
  );
  const level = nearest.find((candidate) => levels.has(candidate));
  return level === undefined ? {} : { effort: level };
}

/** Anthropic's Claude as the provider, through the official SDK. */
export function anthropicProvider(options: { apiKey: string; model: string; effort: AskEffort }): AskProvider {
  const client = new Anthropic({ apiKey: options.apiKey, maxRetries: 1, timeout: PROVIDER_TIMEOUT_MS });
  return {
    async translate(request, signal) {
      const effort = effortFor(await effortLevelsOf(client, options.model), options.effort);
      let response: Anthropic.Message;
      try {
        response = await client.messages.create(
          {
            model: options.model,
            max_tokens: 1024,
            system: [{ type: "text", text: systemPrompt(request), cache_control: { type: "ephemeral" } }],
            messages: [{ role: "user", content: userPrompt(request) }],
            output_config: { ...effort, format: { type: "json_schema", schema: ANSWER_SCHEMA } },
          },
          signal ? { signal } : {},
        );
      } catch (error) {
        throw new Error(describeAnthropicError(error), { cause: error });
      }
      if (response.stop_reason === "refusal") throw new Error("Anthropic declined to answer this question");
      const text = response.content.find((block) => block.type === "text")?.text ?? "";
      return parseAnswer(text);
    },
  };
}

/** What went wrong with a call to Anthropic, in words the panel can show. */
function describeAnthropicError(error: unknown): string {
  if (error instanceof Anthropic.AuthenticationError) return "Anthropic refused the API key; set it again";
  if (error instanceof Anthropic.PermissionDeniedError) return "This API key may not use that model";
  if (error instanceof Anthropic.NotFoundError) return "Anthropic doesn't know that model (spelunk.ask.model)";
  if (error instanceof Anthropic.RateLimitError) return "Anthropic is rate-limiting; try again in a moment";
  if (error instanceof Anthropic.APIConnectionError) return "Couldn't reach Anthropic; check the network";
  if (error instanceof Anthropic.APIError) return `Anthropic answered with an error: ${error.message}`;
  return error instanceof Error ? error.message : String(error);
}

/**
 * A provider for test builds, from SPELUNK_ASK_FAKE: a JSON object from each
 * question to its answer (a query string, or {query, explanation,
 * assumptions}); a question it doesn't list is an error. Undefined when the
 * variable isn't set.
 */
export function fakeProviderFromEnvironment(environment: NodeJS.ProcessEnv = process.env): AskProvider | undefined {
  const raw = environment["SPELUNK_ASK_FAKE"];
  if (!raw) return undefined;
  const answers = JSON.parse(raw) as Record<string, string | AskAnswer>;
  return {
    translate(request) {
      const key = request.retry ? `${request.question} [${request.retry.problem}]` : request.question;
      const answer = answers[key] ?? answers[request.question];
      if (answer === undefined) return Promise.reject(new Error(`no fake answer for ${JSON.stringify(key)}`));
      if (typeof answer === "string") return Promise.resolve({ query: answer, explanation: "", assumptions: [] });
      return Promise.resolve(answer);
    },
  };
}

/** The first error a parsed query carries, as the panel words it. */
function firstError(query: ParsedQuery): string | undefined {
  return query.diagnostics.find((diagnostic) => diagnostic.severity === "error")?.message;
}

/** Turns questions into queries through its provider, checking each with the parser. */
export class Asker {
  private guideText: string | undefined;

  /** An Asker over `deps`: the parser and guide, the setup and the names to share. */
  constructor(private readonly deps: AskDependencies) {}

  /**
   * Answers `question`: the query it became, checked by the parser (with one
   * repair asked for when the first doesn't parse), or why there is none.
   * `broadenFrom` is a query that found nothing: one looser query is asked
   * for instead.
   */
  async ask(question: string, broadenFrom?: string, signal?: AbortSignal): Promise<AskOutcome> {
    const setup = await this.deps.setup();
    if (setup.kind !== "ready") return { setup: setup.kind };
    try {
      const base = await this.request(question);
      const retry =
        broadenFrom === undefined ? {} : { retry: { query: broadenFrom, problem: "noResults" as const, detail: "" } };
      let answer = await setup.provider.translate({ ...base, ...retry }, signal);
      let problem = firstError(await this.deps.parse(answer.query));
      if (problem !== undefined) {
        answer = await setup.provider.translate(
          { ...base, retry: { query: answer.query, problem: "parse", detail: problem } },
          signal,
        );
        problem = firstError(await this.deps.parse(answer.query));
      }
      return {
        query: answer.query,
        explanation: answer.explanation,
        assumptions: answer.assumptions,
        ...(problem === undefined ? {} : { error: `The query it wrote doesn't work: ${problem}` }),
      };
    } catch (error) {
      if (signal?.aborted) return { error: "Stopped" };
      return { error: error instanceof Error ? error.message : String(error) };
    }
  }

  /** Everything but the question's own words: the guide (read once), the names and today. */
  private async request(question: string): Promise<AskRequest> {
    this.guideText ??= await this.deps.guide();
    const { repos, teams } = await this.deps.names();
    const today = this.deps.today?.() ?? new Date().toISOString().slice(0, 10);
    return { question, guide: this.guideText, today, repos, teams };
  }
}
