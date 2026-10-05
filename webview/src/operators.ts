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
export type Tone = "match" | "logic" | "scope" | "history" | "output";

/** A group of entries that share a color, such as Scope or History. */
export interface OperatorGroup {
  name: string;
  tone: Tone;
  entries: OperatorEntry[];
}

/** Every operator and piece of syntax: 16 entries in five groups. */
export const OPERATOR_GROUPS: OperatorGroup[] = [
  {
    name: "Matching",
    tone: "match",
    entries: [
      {
        operator: "case",
        label: "case:yes|no",
        insert: "case:yes ",
        summary: "Case-sensitive or not. Insensitive by default.",
        description: "Match capital letters exactly, or not. Default is no.",
        example: "case:yes RetryPolicy",
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
        label: "type:file|code|commit",
        insert: "type:",
        summary: "Return one kind of result.",
        description: "Return only file names, code lines, or commits.",
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
    ],
  },
];

/** Typed by OpName, so adding an operator to the schema fails to compile until it has a tone. */
export const OPERATOR_TONE: Record<OpName, Tone> = {
  case: "match",
  f: "scope",
  repo: "scope",
  lang: "scope",
  type: "scope",
  sym: "scope",
  author: "history",
  msg: "history",
  since: "history",
  count: "output",
};

/** The two spellings of each operator: file: and f:. The daemon also accepts the OpName itself (lang:, sym:, msg:). */
const OPERATOR_NAMES: Record<OpName, { full: string; short: string }> = {
  f: { full: "file", short: "f" },
  repo: { full: "repo", short: "r" },
  lang: { full: "language", short: "l" },
  type: { full: "type", short: "t" },
  sym: { full: "symbol", short: "s" },
  author: { full: "author", short: "a" },
  msg: { full: "message", short: "m" },
  since: { full: "since", short: "d" },
  case: { full: "case", short: "c" },
  count: { full: "count", short: "n" },
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
