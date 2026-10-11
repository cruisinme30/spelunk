// Turns text taken from the editor into a query: the selection for ⌘P, or a
// name for Find Definitions and Find Uses. No vscode import, so tests run it
// under plain Node.

/** A selection with more lines than this is not searched. */
export const MAX_SEED_LINES = 10;
/** A selection with more characters than this is not searched. */
export const MAX_SEED_CHARS = 1000;

/** What a selection seeds: a query, or why the last query stays. */
export type Seed = { query: string } | { skipped: "empty" | "tooLong" };

/** A word the query language reads as itself: letters, digits, _ and inner dots. */
const PLAIN_WORD = /^[\p{L}\p{N}_](?:[\p{L}\p{N}_.]*[\p{L}\p{N}_])?$/u;
/** Words the query language reads as logic, not text, when bare. */
const KEYWORDS = new Set(["OR", "AND", "NOT"]);
const CASE_TOGGLE = /^case:(?:yes|no|smart)$/;
const WORD_TOGGLE = /^word:(?:yes|no)$/;

/**
 * The query for a selection. A line is searched as a "phrase", even a single
 * word: a phrase matches only that exact text, in file names too, where a bare
 * word would also find names with its letters in order; and quotes, colons,
 * dashes and OR are text, not syntax. With .* pressed in the last query, it is
 * an escaped /regex/; several lines become one regex across them, indentation
 * ignored. Aa and ab carry over.
 */
export function seedFromSelection(selection: string, lastQuery: string): Seed {
  const lines = selection.split(/\r?\n/);
  if (selection.length > MAX_SEED_CHARS || lines.length > MAX_SEED_LINES) return { skipped: "tooLong" };
  const kept = lines.map((line) => line.trim()).filter((line) => line !== "");
  const [first] = kept;
  if (first === undefined) return { skipped: "empty" };
  const { toggles, regex } = lastToggles(lastQuery);
  let term: string;
  if (kept.length > 1) term = `/${kept.map((line) => escapeRegex(line)).join(String.raw`\n\s*`)}/`;
  else term = regex ? `/${escapeRegex(first)}/` : phrase(first);
  return { query: [...toggles, term].join(" ") };
}

/** Find Definitions: the definitions named exactly `name`. */
export function definitionsQuery(name: string): string {
  return `sym:/^${escapeRegex(name)}$/`;
}

/** Find Uses: the lines that use `name` as a whole word. */
export function usesQuery(name: string): string {
  return `ref:${textTerm(name)}`;
}

/** A word bare, anything else as a "phrase". */
export function textTerm(text: string): string {
  return PLAIN_WORD.test(text) && !KEYWORDS.has(text) ? text : phrase(text);
}

/** The text as a "phrase", its quotes and backslashes escaped. */
function phrase(text: string): string {
  return `"${text.replaceAll("\\", "\\\\").replaceAll('"', String.raw`\"`)}"`;
}

/** Escapes RE2's special characters, and the / that closes a /regex/. */
function escapeRegex(text: string): string {
  return text.replaceAll(/[\\^$.*+?()[\]{}|/]/g, String.raw`\$&`);
}

/** The last query's case: and word: toggles, and whether its text was a /regex/ (.* pressed). */
function lastToggles(query: string): { toggles: string[]; regex: boolean } {
  const toggles: string[] = [];
  let regex = false;
  for (const token of topLevelTokens(query)) {
    if (CASE_TOGGLE.test(token) || WORD_TOGGLE.test(token)) toggles.push(token);
    else if (token.length > 1 && token.startsWith("/") && token.endsWith("/")) regex = true;
  }
  return { toggles, regex };
}

/** The query's space-separated tokens, keeping "phrases" and /regexes/ whole. */
function topLevelTokens(query: string): string[] {
  const tokens: string[] = [];
  let token = "";
  let delimiter: string | undefined;
  let escaped = false;
  for (const char of query) {
    if (delimiter) {
      token += char;
      if (escaped) escaped = false;
      else if (char === "\\") escaped = true;
      else if (char === delimiter) delimiter = undefined;
    } else if (/\s/.test(char)) {
      if (token) tokens.push(token);
      token = "";
    } else {
      if (char === '"' || (char === "/" && token === "")) delimiter = char;
      token += char;
    }
  }
  if (token) tokens.push(token);
  return tokens;
}
