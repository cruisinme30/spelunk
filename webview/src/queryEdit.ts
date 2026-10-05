// Pure edits of the query text. Fix-its, completions, the Aa / .* toggles
// and the repo menu are all text edits followed by an ordinary
// query.changed, so the daemon stays the only parser. Spans come from the
// parsed query; nothing here scans the raw text.
import { clamp } from "./format";
import { operatorNodes, textNodes, topLevelOperators } from "./parsedQuery";
import type { ParsedQuery, Span, TextEdit } from "./protocol.gen";

/** The query text and cursor after an edit. */
export interface Edited {
  text: string;
  cursor: number;
}

/** RE2 metacharacters. */
const REGEX_SPECIAL = /[\\^$.|?*+()[\]{}]/;
/** RE2 metacharacters plus the / that delimits a /regex/ term. */
const REGEX_SPECIAL_OR_SLASH = /[\\^$.|?*+()[\]{}/]/g;

/**
 * Applies non-overlapping edits; the cursor lands after the last one. Spans
 * are clipped to the text, and a span that overlaps an earlier edit starts
 * where that edit ended, so no character is ever copied twice.
 */
export function applyEdits(text: string, edits: TextEdit[]): Edited {
  const sorted = edits
    .filter(({ span }) => Number.isFinite(span.start) && Number.isFinite(span.end))
    .sort((first, second) => first.span.start - second.span.start);
  let result = "";
  let position = 0;
  let cursor = text.length;
  for (const edit of sorted) {
    const start = clamp(edit.span.start, position, text.length);
    result += text.slice(position, start) + edit.newText;
    cursor = result.length;
    position = clamp(edit.span.end, start, text.length);
  }
  result += text.slice(position);
  return { text: result, cursor: Math.min(cursor, result.length) };
}

/** Removes a span plus one neighbouring space, so no double space is left. */
function removeSpan(text: string, span: Span): Edited {
  let start = clamp(span.start, 0, text.length);
  let end = clamp(span.end, start, text.length);
  if (text[end] === " ") end++;
  else if (start > 0 && text[start - 1] === " ") start--;
  return { text: text.slice(0, start) + text.slice(end), cursor: start };
}

/** A global that is yes or no, with a setting for its default: case: behind Aa, word: behind ab. */
type SwitchOperator = "case" | "word";

/** Whether a switch's button shows as pressed: an explicit value in the query wins over the setting. */
function isSwitchOn(query: ParsedQuery | undefined, op: SwitchOperator, onByDefault: boolean): boolean {
  const value = op === "case" ? query?.globals.case : query?.globals.word;
  return value ? value === "yes" : onByDefault;
}

/** A switch's button: drops an explicit value when that flips the state, otherwise writes the opposite one. */
function toggleSwitch(text: string, query: ParsedQuery | undefined, op: SwitchOperator, onByDefault: boolean): Edited {
  const [existing] = operatorNodes(query, op);
  if (!existing) {
    const operator = `${op}:${onByDefault ? "no" : "yes"}`;
    const edited = `${operator} ${text}`.trimEnd();
    return { text: edited, cursor: Math.min(operator.length + 1, edited.length) };
  }
  const pressed = isSwitchOn(query, op, onByDefault);
  if (pressed !== onByDefault) return removeSpan(text, existing.span);
  return applyEdits(text, [{ span: existing.span, newText: `${op}:${pressed ? "no" : "yes"}` }]);
}

/** Whether Aa shows as pressed: an explicit case: wins over the setting. */
export function isCasePressed(query: ParsedQuery | undefined, caseSensitiveByDefault: boolean): boolean {
  return isSwitchOn(query, "case", caseSensitiveByDefault);
}

/** Aa: drops an explicit case: when that flips the state, otherwise writes the opposite one. */
export function toggleCase(text: string, query: ParsedQuery | undefined, caseSensitiveByDefault: boolean): Edited {
  return toggleSwitch(text, query, "case", caseSensitiveByDefault);
}

/** Whether ab shows as pressed: an explicit word: wins over the setting. */
export function isWordPressed(query: ParsedQuery | undefined, wholeWordByDefault: boolean): boolean {
  return isSwitchOn(query, "word", wholeWordByDefault);
}

/** ab: drops an explicit word: when that flips the state, otherwise writes the opposite one. */
export function toggleWord(text: string, query: ParsedQuery | undefined, wholeWordByDefault: boolean): Edited {
  return toggleSwitch(text, query, "word", wholeWordByDefault);
}

/** Whether .* shows as pressed: every text term is a regex. */
export function isRegexPressed(query: ParsedQuery | undefined): boolean {
  const terms = textNodes(query);
  return terms.length > 0 && terms.every((term) => term.match === "regex");
}

/** .*: wraps each literal term in /…/ (escaped), or turns each regex term back into literal text. */
export function toggleRegex(text: string, query: ParsedQuery | undefined): Edited {
  const terms = textNodes(query);
  if (terms.length === 0) return { text, cursor: text.length };
  if (isRegexPressed(query)) {
    return applyEdits(
      text,
      terms.map((term) => ({ span: term.span, newText: quoteIfNeeded(unescapeRegexTerm(term.value)) })),
    );
  }
  return applyEdits(
    text,
    terms
      .filter((term) => term.match !== "regex")
      .map((term) => ({ span: term.span, newText: `/${escapeRegex(term.value, REGEX_SPECIAL_OR_SLASH)}/` })),
  );
}

/**
 * The literal a regex term stands for. A plain word loses its escapes
 * (`retry\_x` → `retry_x`); a real pattern is kept as written, so turning
 * .* off searches for that exact text.
 */
function unescapeRegexTerm(value: string): string {
  const isRealPattern = REGEX_SPECIAL.test(value.replaceAll(/\\./g, ""));
  return isRealPattern ? value : value.replaceAll(/\\(.)/g, "$1");
}

/** Backslash-escapes every character `special` (a global regex) matches. */
function escapeRegex(text: string, special: RegExp): string {
  return text.replace(special, String.raw`\$&`);
}

/** Quotes a literal that would otherwise split into several terms. */
function quoteIfNeeded(literal: string): string {
  return /[\s"()]/.test(literal) ? `"${literal.replaceAll('"', String.raw`\"`)}"` : literal;
}

/**
 * The repo menu: replaces the query's repo: scope with one repo, or removes
 * it for all repos. Negated repo: filters (-repo:legacy) are left alone.
 */
export function scopeToRepo(text: string, query: ParsedQuery | undefined, repoName: string | undefined): Edited {
  let edited: Edited = { text, cursor: text.length };
  const spans = topLevelOperators(query, "repo")
    .map((node) => node.span)
    .sort((first, second) => second.start - first.start); // right to left, so earlier spans stay valid
  for (const span of spans) edited = removeSpan(edited.text, span);
  if (repoName === undefined) return { text: edited.text.trim(), cursor: edited.text.trim().length };
  const operator = `repo:${escapeRegex(repoName, new RegExp(REGEX_SPECIAL.source, "g"))}`;
  const rest = edited.text.trim();
  const result = rest ? `${operator} ${rest}` : operator;
  return { text: result, cursor: result.length };
}
