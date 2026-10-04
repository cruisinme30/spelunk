// Pure text edits on the query box: fix-its, completions and the Aa / .*
// toggles all become edits to the query text (Contract 2), never state.
import type { Node, ParsedQuery, Span, TextEdit } from "./protocol.gen";

export interface Edited {
  text: string;
  cursor: number;
}

/** Applies non-overlapping edits; the cursor lands after the last edit. */
export function applyEdits(text: string, edits: TextEdit[]): Edited {
  const sorted = [...edits].sort((a, b) => a.span.start - b.span.start);
  let out = "";
  let pos = 0;
  let cursor = text.length;
  for (const e of sorted) {
    out += text.slice(pos, e.span.start) + e.newText;
    cursor = out.length;
    pos = e.span.end;
  }
  out += text.slice(pos);
  return { text: out, cursor: Math.min(cursor, out.length) };
}

export function walk(n: Node | null, visit: (n: Node, negated: boolean) => void, negated = false): void {
  if (!n) return;
  visit(n, negated);
  if (n.kind === "and" || n.kind === "or") n.children.forEach((c) => walk(c, visit, negated));
  else if (n.kind === "not") walk(n.child, visit, !negated);
}

export function opNodes(q: ParsedQuery | undefined, op: string): Extract<Node, { kind: "op" }>[] {
  const out: Extract<Node, { kind: "op" }>[] = [];
  walk(q?.root ?? null, (n) => {
    if (n.kind === "op" && n.op === op) out.push(n);
  });
  return out;
}

export function textNodes(q: ParsedQuery | undefined): Extract<Node, { kind: "text" }>[] {
  const out: Extract<Node, { kind: "text" }>[] = [];
  walk(q?.root ?? null, (n) => {
    if (n.kind === "text") out.push(n);
  });
  return out;
}

/** Removes a span plus one neighbouring space, so no double spaces remain. */
export function removeSpan(text: string, span: Span): Edited {
  let { start, end } = span;
  if (text[end] === " ") end++;
  else if (start > 0 && text[start - 1] === " ") start--;
  return { text: text.slice(0, start) + text.slice(end), cursor: start };
}

export function isCasePressed(q: ParsedQuery | undefined, defaultSensitive: boolean): boolean {
  const c = q?.globals.case;
  return c ? c === "yes" : defaultSensitive;
}

/** Aa: removes an explicit case: operator, or adds one that flips the default. */
export function toggleCase(text: string, q: ParsedQuery | undefined, defaultSensitive: boolean): Edited {
  const ops = opNodes(q, "case");
  if (ops.length) {
    const pressed = isCasePressed(q, defaultSensitive);
    const removed = removeSpan(text, ops[0].span);
    // Removing it must actually flip the state; otherwise write the opposite.
    if (pressed !== defaultSensitive) return removed;
    return applyEdits(text, [{ span: ops[0].span, newText: `case:${pressed ? "no" : "yes"}` }]);
  }
  const want = defaultSensitive ? "case:no" : "case:yes";
  return { text: `${want} ${text}`.trimEnd(), cursor: want.length + 1 };
}

export function isRegexPressed(q: ParsedQuery | undefined): boolean {
  const terms = textNodes(q);
  return terms.length > 0 && terms.every((t) => t.match === "regex");
}

const reSpecial = /[\\^$.|?*+()[\]{}]/;

/** .*: wraps every literal text term in /…/, or unwraps regex terms. */
export function toggleRegex(text: string, q: ParsedQuery | undefined): Edited {
  const terms = textNodes(q);
  if (!terms.length) return { text, cursor: text.length };
  if (isRegexPressed(q)) {
    const edits: TextEdit[] = [];
    for (const t of terms) {
      // Plain words lose their escapes; a real pattern becomes its own text, searched literally.
      const literal = reSpecial.test(t.value.replace(/\\./g, "")) ? t.value : t.value.replace(/\\(.)/g, "$1");
      edits.push({ span: t.span, newText: /[\s"()]/.test(literal) ? `"${literal.replace(/"/g, '\\"')}"` : literal });
    }
    return applyEdits(text, edits);
  }
  return applyEdits(
    text,
    terms
      .filter((t) => t.match !== "regex")
      .map((t) => ({
        span: t.span,
        newText: `/${t.value.replace(/[\\^$.|?*+()[\]{}/]/g, "\\$&")}/`,
      })),
  );
}
