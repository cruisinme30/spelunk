// The operator cheat sheet: 16 operators in five groups, plus the
// labels and color tones the chips and completions use for each operator.
import type { OpName } from "./protocol.gen";

/** One entry of the sheet. */
export interface SheetItem {
  /** What clicking the entry inserts into the query box. */
  insert: string;
  /** How the entry reads, with example values after the colon: "since:30d|2w|6m|1y". */
  label: string;
  detail: string;
  /** The operator the entry describes; absent for syntax such as OR or quotes. */
  operator?: OpName;
}
/** The color family of an operator group. */
export type Tone = "match" | "logic" | "scope" | "history" | "output";

export interface SheetGroup {
  name: string;
  tone: Tone;
  items: SheetItem[];
}

export const SHEET: SheetGroup[] = [
  {
    name: "Matching",
    tone: "match",
    items: [
      {
        operator: "case",
        insert: "case:yes ",
        label: "case:yes|no",
        detail: "Case-sensitive or not. Insensitive by default.",
      },
      { insert: '""', label: '"exact phrase"', detail: "Keeps the spaces together." },
      { insert: "//", label: "/regex/", detail: "Text term as a regular expression." },
    ],
  },
  {
    name: "Logic",
    tone: "logic",
    items: [
      { insert: " AND ", label: "a b · a AND b", detail: "Both must match." },
      { insert: " OR ", label: "a OR b", detail: "Either one matches." },
      { insert: "()", label: "( … )", detail: "Group terms." },
      { insert: "-", label: "-term · -f:vendor/", detail: "Exclude a term or any operator." },
    ],
  },
  {
    name: "Scope",
    tone: "scope",
    items: [
      { operator: "f", insert: "f:", label: "f:", detail: "Regex on the full file path." },
      { operator: "repo", insert: "repo:", label: "repo:", detail: "Regex on the repo name." },
      { operator: "lang", insert: "lang:", label: "lang:", detail: "python, ts, go and so on." },
      { operator: "type", insert: "type:", label: "type:file|code|commit", detail: "Return one kind of result." },
      { operator: "sym", insert: "sym:", label: "sym:", detail: "Class, function and method definitions." },
    ],
  },
  {
    name: "History",
    tone: "history",
    items: [
      { operator: "author", insert: "author:", label: "author:", detail: "Commits by this person. Searches diffs." },
      { operator: "msg", insert: "msg:", label: "msg:", detail: "Words in the commit message." },
      {
        operator: "since",
        insert: "since:",
        label: "since:30d|2w|6m|1y",
        detail: "Commits in the window, or files changed in it.",
      },
    ],
  },
  {
    name: "Output",
    tone: "output",
    items: [
      {
        operator: "count",
        insert: "count:",
        label: "count:50|all",
        detail: "How many results. Default is 500 with Load more.",
      },
    ],
  },
];

/** Typed by OpName, so adding an operator to the schema fails to compile until it has a tone. */
export const OP_TONE: Record<OpName, Tone> = {
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
export const OP_LABEL: Record<OpName, string> = {
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

/** The tone for a completion label such as "since:" (non-operators read as logic). */
export function toneForLabel(label: string): Tone {
  const name = label.replace(/:.*$/, "");
  return name in OP_TONE ? OP_TONE[name as OpName] : "logic";
}
