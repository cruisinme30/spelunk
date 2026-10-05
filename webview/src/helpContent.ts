// The words of the help page. Kept as data, apart from the page that
// renders it, so tests can run every example against the real daemon. The
// operator table comes from operators.ts, which the cheat sheet shares.
import { OPERATOR_GROUPS } from "./operators";

/** The opening points of the Basics section; `code` spans become <code>. */
export const BASICS: string[] = [
  "Plain words match file names and file contents together.",
  "Words separated by spaces must all appear in the same file. Use quotes for an exact phrase.",
  "Results update as you type. Matching ignores case unless you add `case:yes` (or `case:smart`, which matches case only when the query has a capital letter), and finds parts of words unless you add `word:yes`.",
  "Adding `author:` or `msg:` switches the results to commits, and text then matches each commit’s diff or message.",
];

/** The points of the Combining section. */
export const COMBINING: string[] = [
  "AND binds tighter than OR. `a b OR c` means `(a b) OR c`.",
  "Use parentheses whenever you mix AND and OR, so the query reads the way you mean it.",
  "The `-` sign works on words, phrases and operators alike.",
  "The parsed query under the search box shows exactly how your query was read.",
];

/** A worked example: what it finds, and the query. */
export interface Example {
  title: string;
  query: string;
}

/** The worked examples, each with a Try button. */
export const EXAMPLES: Example[] = [
  {
    title: "Tests Jane changed this month that mention a timeout",
    query: String.raw`author:jane since:30d f:_test\.py$ timeout`,
  },
  { title: "Where is RetryPolicy defined?", query: "sym:RetryPolicy" },
  { title: "Python files with “retry” in the name", query: "type:file lang:python retry" },
  { title: "Flaky-test fixes in the checkout repo", query: 'msg:"fix flaky" repo:web' },
  { title: "Timeouts outside vendored code, either spelling", query: "(timeout OR time_out) -f:vendor/" },
];

/** The keyboard and mouse table. */
export const KEYS: [what: string, keys: string][] = [
  ["Open search", "⌘P, or the key set by spelunk.shortcut.preset"],
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
  const operatorExamples = OPERATOR_GROUPS.flatMap((group) => group.entries.map((entry) => entry.example));
  return [...operatorExamples, ...EXAMPLES.map((example) => example.query)];
}
