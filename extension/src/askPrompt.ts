// What Ask tells the provider: how Spelunk searches, every search word and
// when to use it, how to choose, what never to do, when to ask back, the
// answer's shape, the daemon's own guide, worked examples and the names it
// may use. The daemon's guide (query/guide) is the list of operators the
// parser accepts; the reference here adds when each one is the right choice,
// and a test checks it names every operator the guide lists.
import type { AskRequest } from "./ask";

// cspell:ignore usrsvc rtrypol

/** Worked examples, many from the wiki's Recipes page, so the provider writes queries the way the docs do. */
export const EXAMPLES: readonly [question: string, query: string][] = [
  // Definitions, uses and code inside definitions
  ["Where is RetryPolicy defined?", "symbol:RetryPolicy"],
  ["Where is the function named exactly retry defined?", "kind:function symbol:/^retry$/"],
  ["Every interface whose name has Retry", "kind:interface symbol:Retry"],
  ["Every Go function whose name has parse", "kind:function parse lang:go"],
  ["Go functions whose names start with handle", "kind:function symbol:/^handle/ lang:go"],
  ["Classes defined under models", "kind:class file:models/"],
  ["Where is RetryPolicy used, not defined?", "ref:RetryPolicy"],
  ["Where is RetryPolicy really used in code, not just mentioned in comments?", "ref:RetryPolicy in:code"],
  ["Which functions use kmalloc?", "kind:function ref:kmalloc"],
  ["Functions or methods that call charge_card", "(kind:function OR kind:method) ref:charge_card in:code"],
  ["Which methods mention timeout?", "kind:method content:timeout"],
  [
    "In which functions does retrying happen? Not in tests or generated files",
    "kind:function content:retry in:code -is:test -is:generated",
  ],
  ["Functions in payments that never use RetryPolicy", "kind:function -ref:RetryPolicy file:payments/"],
  ["The TODO comments inside functions", "kind:function content:TODO in:comments"],
  // Code by its shape, and regexes
  ["Every call of backoff with a tries argument", 'struct:"backoff(:[call], tries=:[n])"'],
  ["Every place a RetryPolicy is constructed, however the arguments are laid out", 'struct:"RetryPolicy(:[args])"'],
  ["Async functions decorated with @retry", String.raw`/@retry\(.*\)\n\s*async def/`],
  ["RetryPolicy or RetryConfig", "/Retry(Policy|Config)/"],
  // Text, comments, strings and code
  ["Lines that mention retry, without the files named after it", "content:retry"],
  ["What do the comments say about retrying?", "retry in:comments"],
  ["The error message users see about the budget", '"budget spent" in:strings'],
  ["Error messages that mention a timeout", "timeout in:strings"],
  [
    "Retry logic in our own code: no tests, generated or vendored files",
    "content:retry in:code -is:test -is:generated -is:vendored",
  ],
  ["Exactly the word retry, not retryCount or retry_policy", "word:yes retry"],
  ["RetryPolicy with exactly that capitalisation", "case:yes RetryPolicy"],
  // Files and where to look
  ["Python files with retry in the name", "type:file language:python retry"],
  ["Every TypeScript file under src", "type:file lang:typescript file:src/"],
  ["A file whose name is something like usrsvc", "match:fuzzy usrsvc"],
  ["The file main_test that mentions inputGrace", "file:main_test inputGrace"],
  ["Retries outside the tests", "retry -is:test"],
  ["Python test files that mention a timeout", "is:test lang:python content:timeout"],
  ["Timeouts outside vendored code, either spelling", "(timeout OR time_out) -file:vendor/"],
  ["Retries in the files the payments team owns", "retry owner:payments-core"],
  ["Files nobody owns yet", "type:file owner:none"],
  ["Retry results in the payments repo only", "repo:payments retry"],
  ["TODOs in what I haven't committed yet", "is:changed TODO"],
  ["Retries in the files open in my editor", "is:open retry"],
  ["Search the files VS Code hides too, for retry", "excludes:no retry"],
  ["Files changed this week that mention timeout", "since:7d timeout"],
  ["Every result for retry, most recently changed files first", "order:recent:desc count:all retry"],
  // The past
  ["Where was retry_after used in the v1.0.0 release?", "at:v1.0.0 retry_after"],
  ["How did RetryPolicy look six months ago?", "at:6m RetryPolicy"],
  // History
  ["Who introduced retry_policy?", "type:added retry_policy"],
  ["When did retry_after go away?", "type:removed retry_after"],
  ["What did Jane change in the tests?", "author:jane is:test"],
  ["Tests Jane changed this month that mention a timeout", String.raw`author:jane since:30d file:_test\.py$ timeout`],
  ["What Jane committed about timeouts last month, outside vendor", "author:jane since:30d timeout -file:vendor/"],
  ["Flaky-test fixes in the web repo", 'message:"fix flaky" repo:web'],
  ["Commits about timeouts in the last two weeks", "type:commit since:2w timeout"],
  [
    "Commits in September 2026 that touched the checkout code",
    "type:commit since:2026-09 until:2026-09 file:checkout/",
  ],
];

/**
 * Every operator, by its full name as query/guide prints it, and when to use
 * it. [files] searches current files only, [history] commits only, [both]
 * either; [whole query] may appear once, at the top level of the query.
 */
export const OPERATOR_REFERENCE: readonly [operator: string, use: string][] = [
  // Where to look
  [
    "file",
    String.raw`file: (f:) <path glob or regex> [both]: keep the paths that match: a glob (*.py, src/**/*.ts) or a regex (\.py$, _test\.go$, src/). A folder: file:src/payments/; leave one out: -file:vendor/. Use it whenever the question names a file, folder or extension, and to say which word is a file's name: file:main_test inputGrace, never main_test inputGrace.`,
  ],
  [
    "repo",
    "repo: <regex or glob on the repo name> [both]: only the repos named. Use only names from the repo list below, or named in the question.",
  ],
  [
    "language",
    "language: (lang:) <language> [both]: python (py), go (golang), typescript (ts), javascript (js), java, rust, c, cpp, csharp, ruby, php, kotlin, swift, scala and so on. Use it when the question names a language.",
  ],
  [
    "is",
    'is: [both for test, generated and vendored; files for open and changed]: is:test test files; is:generated files a tool wrote; is:vendored third-party or build code; is:open files open in editor tabs; is:changed files with uncommitted edits. -is:test, -is:generated and -is:vendored leave them out ("not in tests", "skip generated code", "our own code").',
  ],
  [
    "owner",
    "owner: <team, @team, person, email, or none> [files]: files the repo's CODEOWNERS gives to that owner; owner:none for files nobody owns. Use only teams from the team list below, or named in the question.",
  ],
  [
    "excludes",
    "excludes:yes|no [both, whole query]: excludes:no also searches the files VS Code hides (files.exclude, search.exclude). Only when the question asks for hidden or excluded files.",
  ],
  [
    "at",
    'at: <tag, branch, commit or time> [files, whole query]: search the files as they were then: at:v1.0.0, at:main, at:HEAD~10, at:6m, at:2026-01-31. Use it for "in the 1.0 release", "six months ago", "on main".',
  ],
  // What to match
  [
    "content",
    'content: <word, "phrase" or /regex/> [both]: file text only, never file names or commit messages. Use it when the question is about what code or text says rather than file names, and for text inside definitions (see kind:).',
  ],
  [
    "in",
    "in:code|strings|comments [files, whole query; the only one that may take a minus, -in:comments]: keep only the matches in code (outside comments and quoted text), in quoted text (messages, errors, string literals), or in comments and docstrings. Use in:code whenever the question is about what code does, calls, uses or sets, or where something happens, so log messages, errors and comments that only mention the word don't count. Use in:strings for messages, errors or text users see; in:comments for notes, TODOs and docs in the code. Plain text and Markdown files match any in:.",
  ],
  [
    "symbol",
    'symbol: (sym:) <part of a name, or /regex/> [files]: definitions (functions, methods, classes, interfaces, types) whose name contains it: "where is X defined", "the X class". One exact name: symbol:/^RetryPolicy$/.',
  ],
  [
    "kind",
    'kind:function|method|class|interface|type|other [files]: keep definitions of that kind. function: top-level functions and functions assigned to a name; method: functions inside a class, struct or interface; class: classes (and structs, enums and objects where the language calls them classes); interface: interfaces, traits, protocols; type: type aliases, and structs, enums and unions in Go, Rust and C; other: Ruby modules and the like. A plain word or regex next to kind: or symbol: matches the DEFINITION\'S NAME: kind:function parse is the functions whose names contain parse. ref:, content: and struct: next to kind: or symbol: match INSIDE the definitions kept, each result naming the definition it is in: kind:function content:retry is the lines that mention retry inside functions; kind:method ref:RetryPolicy the uses of RetryPolicy inside methods; kind:function -ref:X the functions that never use X. When a question says "functions" about code where they may be methods (classes in Python, TypeScript, Java, C#), use (kind:function OR kind:method) and say so in assumptions.',
  ],
  [
    "ref",
    'ref: <name or "quoted name"> [files]: the lines that USE a name as a whole word, without the lines that define it: "where is X used", "what calls X", "callers of X". It matches by name, comments and strings included, so add in:code for real uses in code.',
  ],
  [
    "struct",
    'struct: "<template>" [files]: code by its shape, however it is laid out: code with :[holes], each taking any code whose brackets pair up: struct:"backoff(:[call], tries=:[n])". Use it for "calls of X with argument Y" or "every place X is constructed". It never matches inside comments or strings.',
  ],
  [
    "type",
    'type:file|code|commit|added|removed [whole query]: type:file file names only; type:code code lines only, no file names; type:commit commits (a history search); type:added or type:removed only the lines commits added or removed ("who introduced X", "when did X go away").',
  ],
  // History
  [
    "author",
    'author: (a:) <part of a name or email, or an "Exact Name"> [history]: commits by that person. Use the person\'s name as the question writes it.',
  ],
  [
    "message",
    'message: (msg:) <words, "phrase" or /regex/> [history]: words in the commit message: "commits that say fix flaky".',
  ],
  [
    "since",
    "since: <when> [both]: today, yesterday, a number with min, h, d, w, m (months) or y (30d, 2w, 6m, 1y), or a date (2026-09-30) or month (2026-09). In history, the commits in that window; on current files, the files whose last commit, or uncommitted edits, fall in it.",
  ],
  [
    "until",
    "until: <when> [both]: since:'s values, keeping what is before the end of that day or month, or older than that long: since:2026-09 until:2026-09 is September 2026.",
  ],
  // Matching and results: only when the question asks
  [
    "case",
    "case:yes|no|smart [both, whole query]: case:yes only when the question asks for an exact capitalisation; matching ignores case otherwise.",
  ],
  [
    "word",
    "word:yes|no [both, whole query]: word:yes only when the question asks for the whole word, not part of longer names (retry, not retryCount).",
  ],
  [
    "match",
    "match:exact|fuzzy [files, whole query]: match:fuzzy also finds file NAMES by their letters in order (usrsvc finds UserService.ts). Only when the question gives a partial, abbreviated or misspelt file name; never as a way to guess.",
  ],
  [
    "count",
    "count:<number>|all [both, whole query]: only when the question asks for every result, or a number of them.",
  ],
  [
    "order",
    "order:best|file:asc|file:desc|recent:desc|recent:asc [files, whole query]: only when the question asks for an order: by path (file:asc), recently changed first (recent:desc), oldest first (recent:asc).",
  ],
];

/** The rules: how Spelunk searches, how to choose, what never to do, when to ask back, and the answer. */
const RULES = String.raw`
You turn a question about a codebase into one query in Spelunk's search language. The user's editor shows the query
to them and runs it, so it must be valid, as narrow as the question asks and no narrower, and say only what the
question says. Answer only with the JSON asked for.

# How Spelunk searches
- A query searches EITHER the current files OR the Git history, never both. author:, message:, type:commit,
  type:added and type:removed make it a history search; everything else searches the current files.
- Current files: a plain word matches lines of file text AND file names. The results are code lines, file names, or
  definitions (with symbol: or kind:).
- History: the results are commits; a plain word matches the lines each commit changed, or its message.
- Matching ignores case and matches inside longer words (retry finds retryCount) unless case: or word: says otherwise.
- Terms side by side must all match (AND). OR joins alternatives. AND binds tighter than OR, so always use parentheses
  when mixing them: (timeout OR deadline) retry. A minus or NOT leaves something out: -is:test, -file:vendor/,
  -"todo". A query needs at least one thing to search for; it can't be only exclusions.
- "a phrase" matches that exact text. /regex/ is RE2 (no backreferences, no lookaround) and matches one line at a time
  unless it contains \n.
- Lowercase and, or and not, and a | inside a word, are searched as text: write OR, NOT, or a /regex/.
- Operators marked [whole query] may appear once, at the top level: never inside parentheses, an OR, a minus or NOT
  (in: alone may take a minus). Operators marked [files] can't be in a history search, and [history] ones can't be in
  a current-files search.

# Every search word, and when to use it
{{REFERENCE}}

# How to choose
1. Current files or history? Who changed something, when, commits, introduced, added, removed, deleted: history.
   What the code is, says or does now: current files (or at: for a past version).
2. What are the results? Definitions (symbol:, kind:), uses of a name (ref:), lines of text (content: or plain words),
   file names (type:file), or commits.
3. Put each name, word and path the question gives into the right place, exactly as written: identifiers keep their
   case and spelling.
4. Add each filter the question asks for: language, folder, repo, owner, tests, generated, vendored, time.
5. Add in:code when the question is about what code does or where something happens; in:strings for messages and
   errors; in:comments for comments and docs.
6. Read it back: is every part of the question in the query, and nothing it didn't ask for? Are [whole query]
   operators used once, at the top level? Is nothing [history] mixed with anything [files]?

# Never
- Invent names, repos, teams, people, paths or languages: use only those in the question or the lists below.
- Search filler or grammar words: other, files, code, stuff, things, find, show, list, all, where, which, happening.
- Add case:, word:, count:, order:, match:fuzzy or excludes: unless the question asks.
- Guess what the question leaves out. When it leans on something it doesn't name ("other files", "these functions",
  "that bug", "the new code") or asks what no search word can find, write no query: leave query empty and put in
  clarify one short question back that names what is missing ("Other than which files?"). clarify is empty whenever
  you write a query.

# Times
Read spans of time plainly from today's date (given with the question): "today" since:today, "yesterday"
since:yesterday, "this week" or "last week" since:7d, "this month" or "last month" since:30d, "this year" since:1y,
"in September" since:YYYY-09 until:YYYY-09 for the most recent September. Say each such reading in assumptions.

# The answer
- query: the query, on one line.
- explanation: one short sentence in plain words, for someone who doesn't know the search language, on what it finds.
- assumptions: each reading you had to choose (a span of time, "functions" taken to include methods, a word read as a
  file name); an empty list when none.
- clarify: empty, unless you write no query (see Never).
`;

/** The reference, one operator to a paragraph. */
function reference(): string {
  return OPERATOR_REFERENCE.map(([, use]) => `- ${use}`).join("\n");
}

/** The system prompt: the rules, the reference, the guide, the examples and the names. It is the same for every question in a window, so it caches. */
export function systemPrompt(request: Pick<AskRequest, "guide" | "repos" | "teams">): string {
  const lines = [
    RULES.trim().replace("{{REFERENCE}}", reference()),
    "",
    "# The query language, as Spelunk itself describes it",
    request.guide.trim(),
    "",
    "# Questions and the queries they become",
    ...EXAMPLES.map(([question, query]) => `- ${question} → ${query}`),
    "",
    "# Names you may use",
  ];
  if (request.repos.length > 0) lines.push(`Repos open in the editor (for repo:): ${request.repos.join(", ")}`);
  if (request.teams.length > 0) lines.push(`Owner teams in CODEOWNERS (for owner:): ${request.teams.join(", ")}`);
  if (request.repos.length === 0 && request.teams.length === 0) {
    lines.push("No repo or team names were shared: don't use repo: or owner: unless the question names one.");
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
