// Pure edits of the query text. Fix-its, completions, the Aa / .* toggles
// and the repo menu are all text edits followed by an ordinary
// query.changed, so the daemon stays the only parser. Spans come from the
// parsed query; nothing here scans the raw text.
import { clamp } from "./format";
import { operatorNodes, textNodes, topLevelConjuncts, topLevelOperators } from "./parsedQuery";
import type { CaseSetting, ParsedQuery, Span, TextEdit } from "./protocol.gen";

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

/** A switch's state: pressed now, and whether it would be without the query's value. */
interface SwitchState {
  pressed: boolean;
  onByDefault: boolean;
}

/** A switch's button: drops an explicit value when that flips the state, otherwise writes the opposite one. */
function toggleSwitch(
  text: string,
  query: ParsedQuery | undefined,
  op: SwitchOperator,
  { pressed, onByDefault }: SwitchState,
): Edited {
  const [existing] = operatorNodes(query, op);
  if (!existing) {
    const operator = `${op}:${onByDefault ? "no" : "yes"}`;
    const edited = `${operator} ${text}`.trimEnd();
    return { text: edited, cursor: Math.min(operator.length + 1, edited.length) };
  }
  if (pressed !== onByDefault) return removeSpan(text, existing.span);
  return applyEdits(text, [{ span: existing.span, newText: `${op}:${pressed ? "no" : "yes"}` }]);
}

/** Whether smart case decides Aa: the query says case:smart, or says nothing and the setting is smart. */
export function isCaseSmart(query: ParsedQuery | undefined, setting: CaseSetting): boolean {
  const value = query?.globals.case;
  return value ? value === "smart" : setting === "smart";
}

/** Whether case matches by `setting` alone: smart case matches it when the query has a capital letter. */
function matchesCaseByDefault(query: ParsedQuery | undefined, setting: CaseSetting): boolean {
  return setting === "smart" ? !!query?.hasCapital : setting === "on";
}

/** Whether Aa shows as pressed: case matches, by an explicit case: or else by the setting. */
export function isCasePressed(query: ParsedQuery | undefined, setting: CaseSetting): boolean {
  const value = query?.globals.case;
  if (value === "smart") return !!query?.hasCapital;
  return value ? value === "yes" : matchesCaseByDefault(query, setting);
}

/** Aa: drops an explicit case: when that flips the state, otherwise writes the opposite one. */
export function toggleCase(text: string, query: ParsedQuery | undefined, setting: CaseSetting): Edited {
  return toggleSwitch(text, query, "case", {
    pressed: isCasePressed(query, setting),
    onByDefault: matchesCaseByDefault(query, setting),
  });
}

/** Whether ab shows as pressed: an explicit word: wins over the setting. */
export function isWordPressed(query: ParsedQuery | undefined, wholeWordByDefault: boolean): boolean {
  const value = query?.globals.word;
  return value ? value === "yes" : wholeWordByDefault;
}

/** ab: drops an explicit word: when that flips the state, otherwise writes the opposite one. */
export function toggleWord(text: string, query: ParsedQuery | undefined, wholeWordByDefault: boolean): Edited {
  return toggleSwitch(text, query, "word", {
    pressed: isWordPressed(query, wholeWordByDefault),
    onByDefault: wholeWordByDefault,
  });
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

/** Whether a facet bucket's filter is in the query: kept (repo:x), left out (-repo:x), or neither. */
export type FacetState = "kept" | "left-out" | "none";

/**
 * The filters a facet bucket's filter is made of: "since:2026-09 until:2026-09"
 * is two. The daemon writes these, quoting any value with a space.
 */
function filterParts(filter: string): string[] {
  return filter.match(/(?:[^\s"]|"(?:\\.|[^"\\])*")+/g) ?? [];
}

/** The filter that leaves a bucket out: -repo:x, or -(since:… until:…) for one of several parts. */
export function leaveOut(filter: string): string {
  return filterParts(filter).length > 1 ? `-(${filter})` : `-${filter}`;
}

/** The top-level conjuncts that hold a bucket's filter, and whether they keep or leave it out. */
function facetSpans(query: ParsedQuery | undefined, filter: string): { state: FacetState; spans: Span[] } {
  const raw = query?.raw ?? "";
  const conjuncts = topLevelConjuncts(query).map((node) => ({
    span: node.span,
    text: raw.slice(node.span.start, node.span.end),
  }));
  const excluded = conjuncts.find((conjunct) => conjunct.text === leaveOut(filter));
  if (excluded) return { state: "left-out", spans: [excluded.span] };
  const kept = filterParts(filter).map((part) => conjuncts.find((conjunct) => conjunct.text === part)?.span);
  const spans = kept.filter((span) => span !== undefined);
  return spans.length > 0 && spans.length === kept.length ? { state: "kept", spans } : { state: "none", spans: [] };
}

/** Whether the query keeps or leaves out a facet bucket. */
export function facetState(query: ParsedQuery | undefined, filter: string): FacetState {
  return facetSpans(query, filter).state;
}

/**
 * A facet bucket's click: takes its filter back out of the query when it is
 * there, kept or left out; otherwise adds it at the end, or with `exclude`
 * (Alt-click) the filter that leaves it out.
 */
export function toggleFacet(text: string, query: ParsedQuery | undefined, filter: string, exclude: boolean): Edited {
  const { state, spans } = facetSpans(query, filter);
  if (state !== "none") {
    let edited: Edited = { text, cursor: text.length };
    for (const span of [...spans].sort((first, second) => second.start - first.start)) {
      edited = removeSpan(edited.text, span);
    }
    const trimmed = edited.text.trim();
    return { text: trimmed, cursor: trimmed.length };
  }
  const added = `${text.trimEnd()} ${exclude ? leaveOut(filter) : filter}`.trimStart();
  return { text: added, cursor: added.length };
}
