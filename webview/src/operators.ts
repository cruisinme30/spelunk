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

/** Every operator and piece of syntax: 26 entries in five groups. */
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
        summary: String.raw`Text term as a regular expression; one with \n spans lines.`,
        description: String.raw`Treat the text as a regular expression. One with \n in it matches across lines of current files, so /except\n\s*raise/ finds an except followed by a raise on the next line.`,
        example: "/Retry(Policy|Config)/",
      },
      {
        operator: "struct",
        label: 'struct:"f(:[x])"',
        insert: 'struct:""',
        summary: "Code shaped like a template; each :[hole] takes any code.",
        description:
          "Find code by its shape. The template is code with holes: backoff(:[call], tries=:[n]) finds every call of backoff with a tries= argument last, however it is laid out over lines, and a hole takes whole arguments, brackets and all. Spaces don't matter except between words, and code in comments and strings, read by each file's language, never matches. A name used twice must take the same code, so if :[x] == :[x] { finds a value compared with itself; :[_] takes anything. Current files only, and replace can't change it.",
        example: 'struct:"backoff(:[call], tries=:[n])"',
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
        description: "Exclude a word or anything an operator matches. NOT works too: NOT f:vendor/ is -f:vendor/.",
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
        description: String.raw`A regex on the full file path (\.go$), or a glob (*.go, src/**/*.ts). path: works too.`,
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
        operator: "kind",
        label: "kind:function|class",
        insert: "kind:",
        summary: "Only definitions of one kind, such as class.",
        description:
          "Only definitions of one kind: function, method, class, interface, type or other. Use it with symbol: to narrow a name, or alone with file: to list them: kind:class file:models/. Current files only.",
        example: "kind:class symbol:Retry",
      },
      {
        operator: "ref",
        label: "ref:",
        insert: "ref:",
        summary: "Uses of a name, without its definitions.",
        description:
          "Lines that use a name as a whole word, leaving out the lines that define it. It matches by name, not by meaning, so a different RetryPolicy in another repo counts too. Current files only.",
        example: "ref:RetryPolicy",
      },
      {
        operator: "content",
        label: "content:",
        insert: "content:",
        summary: "Text inside files only, never file names.",
        description:
          'Match the text inside files but never their names; in a commit search, the changed lines but never the message. Its value is searched as written, so content:"type:file" finds that text.',
        example: "content:retry",
      },
      {
        operator: "is",
        label: "is:open|test|generated|vendored",
        insert: "is:",
        summary: "Files open in the editor, uncommitted, tests, generated or vendored.",
        description:
          "Files open in the editor (open), with uncommitted changes (changed), holding tests (test), written by a tool (generated: *.pb.go, *_pb2.py, *.min.js, or a DO NOT EDIT or @generated header), or someone else's code or a build's (vendored: vendor/, node_modules/, third_party/, dist/). Negate it to leave them out: -is:test, or -is:generated -is:vendored before a replace. In a commit search, test, vendored and generated keep those files' changes, generated by file name only; open and changed apply to current files only.",
        example: "is:test retry",
      },
      {
        operator: "owner",
        label: "owner:@team|@user|none",
        insert: "owner:",
        summary: "Files a team or person owns in CODEOWNERS.",
        description:
          "Files the repo's CODEOWNERS file gives this owner: a team (@acme/payments-core, or payments-core alone), a person (@jane or jane) or an email, matched whole and ignoring case. As on GitHub, the file in .github/, the root or docs/ counts, and the last line that matches a file decides. owner:none keeps files no line gives an owner, and -owner:@acme/platform leaves a team's files out. A repo without a CODEOWNERS file matches no owner. Current files only.",
        example: "owner:payments-core retry",
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
        label: "since:today|2h|30d|2026-09",
        insert: "since:",
        summary: "Commits in the window or from a date, or files changed then.",
        description:
          "Commits in the window, or files changed in it. today, yesterday, a number with min, h, d, w, m (months) or y, or a date: 2026-09-30 starts at that day's midnight, 2026-09 at the month's first.",
        example: "since:2w timeout",
      },
      {
        operator: "until",
        label: "until:yesterday|2w|2026-09",
        insert: "until:",
        summary: "Commits up to a date or older than a window, or files last changed then.",
        description:
          "The other end of since: and the same values. A date or a month counts in full, so until:2026-09 keeps everything before October; until:2w keeps what is older than two weeks. since:2026-09 until:2026-09 is September.",
        example: "until:2w timeout",
      },
      {
        operator: "at",
        label: "at:v1.0.0|main|6m",
        insert: "at:",
        summary: "Files as they were at a tag, branch, commit or time.",
        description:
          "Search the files as they were at a tag, a branch, a commit (3f9c2e1, HEAD~10) or a time (6m, 2026-01-31: the newest commit by then), read from Git in each repo. Results say which files changed or were deleted since, open read-only, and Compare with now shows what changed. is:open, is:changed, since:, until: and owner: describe files as they are now, so they can't go with it.",
        example: "at:v1.0.0 retry",
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
  kind: "scope",
  ref: "scope",
  is: "scope",
  owner: "scope",
  content: "scope",
  struct: "match",
  author: "history",
  msg: "history",
  since: "history",
  until: "history",
  at: "history",
  count: "output",
  order: "output",
};

/**
 * The two spellings of each operator: file: and f:, and any alias brought from other tools (path:). The daemon also
 * accepts the OpName itself (lang:, sym:, msg:).
 */
const OPERATOR_NAMES: Record<OpName, { full: string; short: string; aliases?: string[] }> = {
  f: { full: "file", short: "f", aliases: ["path"] },
  repo: { full: "repo", short: "r" },
  lang: { full: "language", short: "l" },
  type: { full: "type", short: "t" },
  sym: { full: "symbol", short: "s" },
  kind: { full: "kind", short: "k" },
  ref: { full: "ref", short: "x" },
  is: { full: "is", short: "i" },
  owner: { full: "owner", short: "" },
  content: { full: "content", short: "" },
  struct: { full: "struct", short: "" },
  author: { full: "author", short: "a" },
  msg: { full: "message", short: "m" },
  since: { full: "since", short: "d" },
  until: { full: "until", short: "u" },
  at: { full: "at", short: "" },
  case: { full: "case", short: "c" },
  word: { full: "word", short: "w" },
  count: { full: "count", short: "n" },
  order: { full: "order", short: "o" },
};

/** The operator a typed name stands for, in any spelling: "f", "file", "path" → "f". */
export function opNameFor(spelling: string): OpName | undefined {
  return (Object.keys(OPERATOR_NAMES) as OpName[]).find((op) => {
    const names = OPERATOR_NAMES[op];
    return (
      op === spelling ||
      names.full === spelling ||
      (names.short !== "" && names.short === spelling) ||
      names.aliases?.includes(spelling)
    );
  });
}

/** How an operator reads in full, as in the parsed-query chips: "f" → "file". */
export function fullName(op: OpName): string {
  return OPERATOR_NAMES[op].full;
}

/** An entry's short name, "f:", shown beside its full name; absent for syntax and for owner:, content:, struct: and at:, which have none. */
export function shortName(entry: OperatorEntry): string | undefined {
  const short = entry.operator ? OPERATOR_NAMES[entry.operator].short : "";
  return short ? `${short}:` : undefined;
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
