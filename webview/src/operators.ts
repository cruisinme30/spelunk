// The operator reference: every operator and piece of syntax, in five
// groups. The cheat sheet, the operator suggestions and the help page all
// read it, so each operator is described in one place. Also the labels and
// color tones the parsed-query chips use for each operator.
import type { OpName } from "./protocol.gen";

/** One operator or piece of syntax. */
export interface OperatorEntry {
  /** The operator the entry describes; absent for syntax such as OR or quotes. */
  operator?: OpName;
  /** How the entry reads, with example values after the colon: "since:today|2h|30d|6m". */
  label: string;
  /** What clicking the entry on the cheat sheet inserts into the query box. */
  insert: string;
  /** One short line for the cheat sheet and the operator suggestions. */
  summary: string;
  /** The help page's fuller explanation. */
  description: string;
  /** A query to try from the help page; a test runs each one against the real daemon. */
  example: string;
}

/** The color family of an operator group. */
type Tone = "match" | "logic" | "scope" | "history" | "output";

/** A group of entries that share a color, such as Scope or History. */
export interface OperatorGroup {
  name: string;
  tone: Tone;
  entries: OperatorEntry[];
}

/** Every operator and piece of syntax: 19 entries in five groups. */
export const OPERATOR_GROUPS: OperatorGroup[] = [
  {
    name: "Matching",
    tone: "match",
    entries: [
      {
        operator: "case",
        label: "case:yes|no|smart",
        insert: "case:yes ",
        summary: "Case-sensitive, or not, or only with a capital. Insensitive by default.",
        description:
          "Match capital letters exactly, or not. smart matches them only when the query has a capital letter, so retry finds Retry but RetryPolicy doesn't find retrypolicy. Default is no, unless the Case Sensitive setting says otherwise.",
        example: "case:yes RetryPolicy",
      },
      {
        operator: "word",
        label: "word:yes|no",
        insert: "word:yes ",
        summary: "Whole words only, or parts of words too. Parts by default.",
        description:
          "Match only whole words, so retry finds retry() but not retryCount or autoretry. Letters, digits and _ make up words. Default is no, unless the Whole Word setting is on.",
        example: "word:yes retry",
      },
      {
        label: '"exact phrase"',
        insert: '""',
        summary: "Keeps the spaces together.",
        description: "Exact phrase, spaces included.",
        example: '"read timeout"',
      },
      {
        label: "/regex/",
        insert: "//",
        summary: "Text term as a regular expression.",
        description: "Treat the text as a regular expression.",
        example: "/Retry(Policy|Config)/",
      },
    ],
  },
  {
    name: "Logic",
    tone: "logic",
    entries: [
      {
        label: "a b · a AND b",
        insert: " AND ",
        summary: "Both must match.",
        description: "Both must match. A space means AND.",
        example: "retry timeout",
      },
      {
        label: "a OR b",
        insert: " OR ",
        summary: "Either one matches.",
        description: "Either one matches.",
        example: "timeout OR deadline",
      },
      {
        label: "( … )",
        insert: "()",
        summary: "Group terms.",
        description: "Group terms.",
        example: "(timeout OR retry) f:tests/",
      },
      {
        label: "-term · -f:vendor/",
        insert: "-",
        summary: "Exclude a term or any operator.",
        description: "Exclude a word or anything an operator matches.",
        example: "timeout -f:vendor/",
      },
    ],
  },
  {
    name: "Scope",
    tone: "scope",
    entries: [
      {
        operator: "f",
        label: "file:",
        insert: "file:",
        summary: "Regex or glob (*.go) on the file path.",
        description: String.raw`A regex on the full file path (\.go$), or a glob (*.go, src/**/*.ts).`,
        example: String.raw`file:.*test\.py$ timeout`,
      },
      {
        operator: "repo",
        label: "repo:",
        insert: "repo:",
        summary: "Regex or glob on the repo name.",
        description: "A regex (^web) or a glob (web-*) on the repo name.",
        example: "repo:payments retry",
      },
      {
        operator: "lang",
        label: "language:",
        insert: "language:",
        summary: "python, ts, go and so on.",
        description: "Only files in this language.",
        example: "language:python retry",
      },
      {
        operator: "type",
        label: "type:file|code|commit|added|removed",
        insert: "type:",
        summary: "Return one kind of result.",
        description:
          "Return only file names, code lines, or commits. type:added and type:removed search commits too, but only the lines they added or removed, not their messages.",
        example: "type:file retry",
      },
      {
        operator: "sym",
        label: "symbol:",
        insert: "symbol:",
        summary: "Class, function and method definitions.",
        description: "Where a class, function or method is defined. Current files only.",
        example: "symbol:RetryPolicy",
      },
      {
        operator: "is",
        label: "is:open|changed|test",
        insert: "is:",
        summary: "Files open in the editor, uncommitted, or tests.",
        description:
          "Files open in the editor (open), with uncommitted changes (changed), or holding tests (test). Negate it to leave them out: -is:test. In a commit search, is:test keeps the test files' changes; open and changed apply to current files only.",
        example: "is:test retry",
      },
    ],
  },
  {
    name: "History",
    tone: "history",
    entries: [
      {
        operator: "author",
        label: "author:",
        insert: "author:",
        summary: "Commits by this person. Searches diffs and messages.",
        description: "Commits by this person. Text matches their diffs or messages.",
        example: "author:jane timeout",
      },
      {
        operator: "msg",
        label: "message:",
        insert: "message:",
        summary: "Words in the commit message.",
        description: "Words in the commit message.",
        example: 'message:"fix flaky"',
      },
      {
        operator: "since",
        label: "since:today|2h|30d|6m",
        insert: "since:",
        summary: "Commits in the window, or files changed in it.",
        description:
          "Commits in the window, or files changed in it. today, yesterday, or a number with min, h, d, w, m (months) or y.",
        example: "since:2w timeout",
      },
    ],
  },
  {
    name: "Output",
    tone: "output",
    entries: [
      {
        operator: "count",
        label: "count:50|all",
        insert: "count:",
        summary: "Results per page, 500 by default; Load more shows the next page.",
        description:
          "How many results to show per page, or all. Without it, the Default Count setting applies (500 unless you change it), and Load more fetches the next page.",
        example: "count:all retry",
      },
      {
        operator: "order",
        label: "order:best|path",
        insert: "order:",
        summary: "Best match first, or by path. Best by default.",
        description:
          "How results from current files are sorted. best puts definitions and file-name matches first and tests, vendored and generated files last; path sorts by repo, then path, then line. Without it, the Order setting applies (best unless you change it). Commits are always newest first.",
        example: "order:path retry",
      },
    ],
  },
];

/** Typed by OpName, so adding an operator to the schema fails to compile until it has a tone. */
export const OPERATOR_TONE: Record<OpName, Tone> = {
  case: "match",
  word: "match",
  f: "scope",
  repo: "scope",
  lang: "scope",
  type: "scope",
  sym: "scope",
  is: "scope",
  author: "history",
  msg: "history",
  since: "history",
  count: "output",
  order: "output",
};

/** The two spellings of each operator: file: and f:. The daemon also accepts the OpName itself (lang:, sym:, msg:). */
const OPERATOR_NAMES: Record<OpName, { full: string; short: string }> = {
  f: { full: "file", short: "f" },
  repo: { full: "repo", short: "r" },
  lang: { full: "language", short: "l" },
  type: { full: "type", short: "t" },
  sym: { full: "symbol", short: "s" },
  is: { full: "is", short: "i" },
  author: { full: "author", short: "a" },
  msg: { full: "message", short: "m" },
  since: { full: "since", short: "d" },
  case: { full: "case", short: "c" },
  word: { full: "word", short: "w" },
  count: { full: "count", short: "n" },
  order: { full: "order", short: "o" },
};

/** The operator a typed name stands for, in any spelling: "f", "file" → "f". */
export function opNameFor(spelling: string): OpName | undefined {
  return (Object.keys(OPERATOR_NAMES) as OpName[]).find(
    (op) => op === spelling || OPERATOR_NAMES[op].full === spelling || OPERATOR_NAMES[op].short === spelling,
  );
}

/** How an operator reads in full, as in the parsed-query chips: "f" → "file". */
export function fullName(op: OpName): string {
  return OPERATOR_NAMES[op].full;
}

/** An entry's short name, "f:", shown beside its full name; absent for syntax. */
export function shortName(entry: OperatorEntry): string | undefined {
  return entry.operator ? `${OPERATOR_NAMES[entry.operator].short}:` : undefined;
}

/** The reference entry for an operator name in any spelling, such as "since" or "d". */
export function operatorEntry(spelling: string): OperatorEntry | undefined {
  const operator = opNameFor(spelling);
  return OPERATOR_GROUPS.flatMap((group) => group.entries).find((entry) => entry.operator === operator);
}

/** The tone for a completion label such as "since:" (anything that isn't an operator reads as logic). */
export function toneForLabel(label: string): Tone {
  const operator = opNameFor(label.replace(/:.*$/, ""));
  return operator ? OPERATOR_TONE[operator] : "logic";
}
