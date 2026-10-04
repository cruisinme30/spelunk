// Pure edits of the query text. Fix-its, completions and the Aa / .*
// toggles are all text edits followed by an ordinary query.changed, so the
// daemon stays the only parser. Spans come from the parsed
// query; nothing here scans the raw text.
import type { Node, ParsedQuery, Span, TextEdit } from "./protocol.gen";

type TextNode = Extract<Node, { kind: "text" }>;
type OpNode = Extract<Node, { kind: "op" }>;

/** The query text and cursor after an edit. */
export interface Edited {
  text: string;
  cursor: number;
}

/** RE2 metacharacters. */
const REGEX_SPECIAL = /[\\^$.|?*+()[\]{}]/;
/** RE2 metacharacters plus the / that delimits a /regex/ term. */
const REGEX_SPECIAL_OR_SLASH = /[\\^$.|?*+()[\]{}/]/g;

/** Applies non-overlapping edits; the cursor lands after the last one. */
export function applyEdits(text: string, edits: TextEdit[]): Edited {
  const sorted = [...edits].sort((a, b) => a.span.start - b.span.start);
  let result = "";
  let position = 0;
  let cursor = text.length;
  for (const edit of sorted) {
    result += text.slice(position, edit.span.start) + edit.newText;
    cursor = result.length;
    position = edit.span.end;
  }
  result += text.slice(position);
  return { text: result, cursor: Math.min(cursor, result.length) };
}

/** Visits every node depth-first, telling the visitor whether it sits under a NOT. */
export function walk(node: Node | null, visit: (node: Node, negated: boolean) => void, negated = false): void {
  if (!node) return;
  visit(node, negated);
  if (node.kind === "and" || node.kind === "or") node.children.forEach((child) => walk(child, visit, negated));
  else if (node.kind === "not") walk(node.child, visit, !negated);
}

/** Every `op:` node with the given operator name. */
export function opNodes(query: ParsedQuery | undefined, op: string): OpNode[] {
  const found: OpNode[] = [];
  walk(query?.root ?? null, (node) => {
    if (node.kind === "op" && node.op === op) found.push(node);
  });
  return found;
}

/** Every text term, in query order. */
export function textNodes(query: ParsedQuery | undefined): TextNode[] {
  const found: TextNode[] = [];
  walk(query?.root ?? null, (node) => {
    if (node.kind === "text") found.push(node);
  });
  return found;
}

/** Removes a span plus one neighbouring space, so no double space is left. */
export function removeSpan(text: string, span: Span): Edited {
  let { start, end } = span;
  if (text[end] === " ") end++;
  else if (start > 0 && text[start - 1] === " ") start--;
  return { text: text.slice(0, start) + text.slice(end), cursor: start };
}

/** Whether Aa shows as pressed: an explicit case: wins over the setting. */
export function isCasePressed(query: ParsedQuery | undefined, caseSensitiveByDefault: boolean): boolean {
  const caseValue = query?.globals.case;
  return caseValue ? caseValue === "yes" : caseSensitiveByDefault;
}

/** Aa: drops an explicit case: when that flips the state, otherwise writes the opposite one. */
export function toggleCase(text: string, query: ParsedQuery | undefined, caseSensitiveByDefault: boolean): Edited {
  const existing = opNodes(query, "case")[0];
  if (!existing) {
    const operator = caseSensitiveByDefault ? "case:no" : "case:yes";
    return { text: `${operator} ${text}`.trimEnd(), cursor: operator.length + 1 };
  }
  const pressed = isCasePressed(query, caseSensitiveByDefault);
  if (pressed !== caseSensitiveByDefault) return removeSpan(text, existing.span);
  return applyEdits(text, [{ span: existing.span, newText: `case:${pressed ? "no" : "yes"}` }]);
}

/** Whether .* shows as pressed: every text term is a regex. */
export function isRegexPressed(query: ParsedQuery | undefined): boolean {
  const terms = textNodes(query);
  return terms.length > 0 && terms.every((term) => term.match === "regex");
}

/** .*: wraps each literal term in /…/ (escaped), or turns each regex term back into literal text. */
export function toggleRegex(text: string, query: ParsedQuery | undefined): Edited {
  const terms = textNodes(query);
  if (!terms.length) return { text, cursor: text.length };
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
      .map((term) => ({ span: term.span, newText: `/${term.value.replace(REGEX_SPECIAL_OR_SLASH, "\\$&")}/` })),
  );
}

/**
 * The literal a regex term stands for. A plain word loses its escapes
 * (`retry\_x` → `retry_x`); a real pattern is kept as written, so turning
 * .* off searches for that exact text.
 */
function unescapeRegexTerm(value: string): string {
  const isRealPattern = REGEX_SPECIAL.test(value.replace(/\\./g, ""));
  return isRealPattern ? value : value.replace(/\\(.)/g, "$1");
}

/** Quotes a literal that would otherwise split into several terms. */
function quoteIfNeeded(literal: string): string {
  return /[\s"()]/.test(literal) ? `"${literal.replace(/"/g, '\\"')}"` : literal;
}

/** The repo a top-level repo: picks, as the daemon resolved it; undefined for all repos. */
export function scopedRepo(query: ParsedQuery | undefined): string | undefined {
  const found = topLevelRepoNodes(query);
  const only = found.length === 1 ? found[0] : undefined;
  return only ? (only.resolved?.label ?? only.value) : undefined;
}

/**
 * The repo menu: replaces the query's repo: scope with one repo, or removes
 * it for all repos. Negated repo: filters (-repo:legacy) are left alone.
 */
export function scopeToRepo(text: string, query: ParsedQuery | undefined, repoName: string | undefined): Edited {
  let edited: Edited = { text, cursor: text.length };
  const spans = topLevelRepoNodes(query)
    .map((node) => node.span)
    .sort((a, b) => b.start - a.start); // right to left, so earlier spans stay valid
  for (const span of spans) edited = removeSpan(edited.text, span);
  if (repoName === undefined) return { text: edited.text.trim(), cursor: edited.text.trim().length };
  const operator = `repo:${repoName.replace(new RegExp(REGEX_SPECIAL.source, "g"), "\\$&")}`;
  const rest = edited.text.trim();
  const result = rest ? `${operator} ${rest}` : operator;
  return { text: result, cursor: result.length };
}

/** repo: operators that scope the whole query: not negated, not inside an OR. */
function topLevelRepoNodes(query: ParsedQuery | undefined): OpNode[] {
  const root = query?.root;
  if (!root) return [];
  const conjuncts = root.kind === "and" ? root.children : [root];
  return conjuncts.filter((node): node is OpNode => node.kind === "op" && node.op === "repo");
}
