// The words of the help page. Kept as data, apart from the page
// that renders it, so tests can run every example against the real daemon.
import type { OpName } from "./protocol.gen";
import type { Tone } from "./sheet";

export interface OperatorRow {
  operator: string;
  what: string;
  example: string;
}

export interface OperatorGroup {
  name: string;
  tone: Tone;
  rows: OperatorRow[];
}

/** Every operator and piece of syntax, with an example to try (16 rows, as on the cheat sheet). */
export const OPERATOR_GROUPS: OperatorGroup[] = [
  {
    name: "Matching",
    tone: "match",
    rows: [
      {
        operator: "case:yes|no",
        what: "Match capital letters exactly, or not. Default is no.",
        example: "case:yes RetryPolicy",
      },
      { operator: '"…"', what: "Exact phrase, spaces included.", example: '"read timeout"' },
      { operator: "/…/", what: "Treat the text as a regular expression.", example: "/Retry(Policy|Config)/" },
    ],
  },
  {
    name: "Logic",
    tone: "logic",
    rows: [
      { operator: "a b · AND", what: "Both must match. A space means AND.", example: "retry timeout" },
      { operator: "OR", what: "Either one matches.", example: "timeout OR deadline" },
      { operator: "( )", what: "Group terms.", example: "(timeout OR retry) f:tests/" },
      { operator: "-", what: "Exclude a word or anything an operator matches.", example: "timeout -f:vendor/" },
    ],
  },
  {
    name: "Scope",
    tone: "scope",
    rows: [
      { operator: "f:", what: "Regex on the full file path.", example: "f:.*test\\.py$ timeout" },
      { operator: "repo:", what: "Regex on the repo name.", example: "repo:payments retry" },
      { operator: "lang:", what: "Only files in this language.", example: "lang:python retry" },
      { operator: "type:", what: "Return only file names, code lines, or commits.", example: "type:file retry" },
      {
        operator: "sym:",
        what: "Where a class, function or method is defined. Current files only.",
        example: "sym:RetryPolicy",
      },
    ],
  },
  {
    name: "History",
    tone: "history",
    rows: [
      {
        operator: "author:",
        what: "Commits by this person. Text matches inside their diffs.",
        example: "author:jane timeout",
      },
      { operator: "msg:", what: "Words in the commit message.", example: 'msg:"fix flaky"' },
      {
        operator: "since:",
        what: "Commits in the window. On code results, files changed in it, including your uncommitted edits.",
        example: "since:2w timeout",
      },
    ],
  },
  {
    name: "Output",
    tone: "output",
    rows: [
      { operator: "count:", what: "How many results to return, or all. Default is 500.", example: "count:all retry" },
    ],
  },
];

/** Every operator has a row: adding one to the schema fails to compile until it is documented here. */
export const DOCUMENTED_OPERATORS: Record<OpName, true> = {
  case: true,
  f: true,
  repo: true,
  lang: true,
  type: true,
  sym: true,
  author: true,
  msg: true,
  since: true,
  count: true,
};

export const BASICS: string[] = [
  "Plain words match file names and file contents together.",
  "Words separated by spaces must all appear in the same file. Use quotes for an exact phrase.",
  "Results update as you type. Matching ignores case unless you add `case:yes`.",
  "Adding `author:` or `msg:` switches the results to commits, and text then matches inside each commit’s diff.",
];

export const COMBINING: string[] = [
  "AND binds tighter than OR. `a b OR c` means `(a b) OR c`.",
  "Use parentheses whenever you mix AND and OR, so the query reads the way you mean it.",
  "The `-` sign works on words, phrases and operators alike.",
  "The parsed query under the search box shows exactly how your query was read.",
];

export interface Example {
  title: string;
  query: string;
}

export const EXAMPLES: Example[] = [
  {
    title: "Tests Jane changed this month that mention a timeout",
    query: "author:jane since:30d f:_test\\.py$ timeout",
  },
  { title: "Where is RetryPolicy defined?", query: "sym:RetryPolicy" },
  { title: "Python files with “retry” in the name", query: "type:file lang:python retry" },
  { title: "Flaky-test fixes in the checkout repo", query: 'msg:"fix flaky" repo:web' },
  { title: "Timeouts outside vendored code, either spelling", query: "(timeout OR time_out) -f:vendor/" },
];

export const KEYS: [what: string, keys: string][] = [
  ["Open search", "⌘P or the key you picked"],
  ["Preview a result", "Single click or ↑ ↓"],
  ["Open the file at that line", "Double-click or ↵"],
  ["Open beside the current editor", "⌘↵"],
  ["Jump to the next result without reopening search", "F4"],
  ["Complete an operator or value", "Tab"],
  ["Apply the first suggested fix", "⌘."],
  ["Show the operator sheet", "? in an empty box"],
];

/** Every example on the page: the operator table's and the worked examples'. */
export function allExamples(): string[] {
  return [...OPERATOR_GROUPS.flatMap((group) => group.rows.map((row) => row.example)), ...EXAMPLES.map((e) => e.query)];
}
