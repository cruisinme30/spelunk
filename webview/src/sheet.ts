// The operator cheat sheet (mock 4) and help-table data. 16 operators.
export interface SheetItem {
  insert: string;
  label: string;
  detail: string;
}
export interface SheetGroup {
  name: string;
  tone: "match" | "logic" | "scope" | "history" | "output";
  items: SheetItem[];
}

export const SHEET: SheetGroup[] = [
  {
    name: "Matching",
    tone: "match",
    items: [
      { insert: "case:yes ", label: "case:yes|no", detail: "Case-sensitive or not. Insensitive by default." },
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
      { insert: "f:", label: "f:", detail: "Regex on the full file path." },
      { insert: "repo:", label: "repo:", detail: "Regex on the repo name." },
      { insert: "lang:", label: "lang:", detail: "python, ts, go and so on." },
      { insert: "type:", label: "type:file|code|commit", detail: "Return one kind of result." },
      { insert: "sym:", label: "sym:", detail: "Class, function and method definitions." },
    ],
  },
  {
    name: "History",
    tone: "history",
    items: [
      { insert: "author:", label: "author:", detail: "Commits by this person. Searches diffs." },
      { insert: "msg:", label: "msg:", detail: "Words in the commit message." },
      { insert: "since:", label: "since:30d|2w|6m|1y", detail: "Commits in the window, or files changed in it." },
    ],
  },
  {
    name: "Output",
    tone: "output",
    items: [{ insert: "count:", label: "count:50|all", detail: "How many results. Default is 500 with Load more." }],
  },
];

export const OP_TONE: Record<string, SheetGroup["tone"]> = {
  case: "match", f: "scope", repo: "scope", lang: "scope", type: "scope", sym: "scope",
  author: "history", msg: "history", since: "history", count: "output",
};

export const OP_LABEL: Record<string, string> = {
  f: "path", repo: "repo", lang: "lang", type: "type", sym: "symbol",
  author: "author", msg: "message", since: "since", case: "case", count: "count",
};
