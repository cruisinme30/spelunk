// Pure edits of the query text. Fix-its, completions and the Aa / .*
// toggles are all text edits followed by an ordinary query.changed, so the
// daemon stays the only parser (Contract 2). Spans come from the parsed
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
