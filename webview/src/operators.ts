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
        label: "f:",
        insert: "f:",
        summary: "Regex or glob (*.go) on the file path.",
        description: String.raw`A regex on the full file path (\.go$), or a glob (*.go, src/**/*.ts).`,
        example: String.raw`f:.*test\.py$ timeout`,
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
        label: "lang:",
        insert: "lang:",
        summary: "python, ts, go and so on.",
        description: "Only files in this language.",
        example: "lang:python retry",
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
        label: "sym:",
        insert: "sym:",
        summary: "Class, function and method definitions.",
        description: "Where a class, function or method is defined. Current files only.",
        example: "sym:RetryPolicy",
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
        summary: "Commits by this person. Searches diffs.",
        description: "Commits by this person. Text matches inside their diffs.",
        example: "author:jane timeout",
      },
      {
        operator: "msg",
        label: "msg:",
        insert: "msg:",
        summary: "Words in the commit message.",
        description: "Words in the commit message.",
        example: 'msg:"fix flaky"',
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

/** How each operator reads in the parsed-query chips. */
export const OPERATOR_CHIP_LABEL: Record<OpName, string> = {
  f: "path",
  repo: "repo",
  lang: "lang",
  type: "type",
  sym: "symbol",
  author: "author",
  msg: "message",
  since: "since",
  case: "case",
  count: "count",
};

/** The reference entry for an operator name such as "since". */
export function operatorEntry(operator: string): OperatorEntry | undefined {
  return OPERATOR_GROUPS.flatMap((group) => group.entries).find((entry) => entry.operator === operator);
}

/** The tone for a completion label such as "since:" (anything that isn't an operator reads as logic). */
export function toneForLabel(label: string): Tone {
  const name = label.replace(/:.*$/, "");
  return name in OPERATOR_TONE ? OPERATOR_TONE[name as OpName] : "logic";
}
