// Reading the daemon's ParsedQuery: its text terms, its operators, and the
// operators that scope the whole query. The webview never parses query text
// itself; everything it knows about a query comes from here.
import type { Node, OpName, ParsedQuery } from "./protocol.gen";

/** A text term of the query tree. */
export type TextNode = Extract<Node, { kind: "text" }>;
/** An operator (`f:`, `repo:`, …) of the query tree. */
export type OperatorNode = Extract<Node, { kind: "op" }>;

/** Visits every node depth-first. */
function walk(node: Node, visit: (node: Node) => void): void {
  visit(node);
  if (node.kind === "and" || node.kind === "or") {
    for (const child of node.children) walk(child, visit);
  } else if (node.kind === "not") {
    walk(node.child, visit);
  }
}

/** Every node of `query` for which `matches` holds, in query order. */
function findNodes<T extends Node>(query: ParsedQuery | undefined, matches: (node: Node) => node is T): T[] {
  const found: T[] = [];
  if (query?.root) {
    walk(query.root, (node) => {
      if (matches(node)) found.push(node);
    });
  }
  return found;
}

/** Every text term, in query order. */
export function textNodes(query: ParsedQuery | undefined): TextNode[] {
  return findNodes(query, (node): node is TextNode => node.kind === "text");
}

/** The plain words of a query, in order. */
export function textTerms(query: ParsedQuery | undefined): string[] {
  return textNodes(query).map((node) => node.value);
}

/** Every `operator:` node with this name, wherever it is in the query. */
export function operatorNodes(query: ParsedQuery | undefined, operator: OpName): OperatorNode[] {
  return findNodes(query, (node): node is OperatorNode => node.kind === "op" && node.op === operator);
}

/**
 * The `operator:` nodes that scope the whole query: those ANDed at the top
 * level, so not negated and not inside an OR.
 */
export function topLevelOperators(query: ParsedQuery | undefined, operator: OpName): OperatorNode[] {
  return topLevelConjuncts(query).filter((node): node is OperatorNode => node.kind === "op" && node.op === operator);
}

/** The nodes ANDed at the top level of the query: every node, when it is a single one. */
export function topLevelConjuncts(query: ParsedQuery | undefined): Node[] {
  const root = query?.root;
  if (!root) return [];
  return root.kind === "and" ? root.children : [root];
}

/** The path patterns that scope a search (top-level `f:` values); code results come only from matching paths. */
export function pathScope(query: ParsedQuery | undefined): string[] {
  return topLevelOperators(query, "f").map((node) => node.value);
}

/** The one repo a top-level `repo:` picks, as the daemon resolved it; undefined for all repos. */
export function scopedRepo(query: ParsedQuery | undefined): string | undefined {
  const [only, ...others] = topLevelOperators(query, "repo");
  if (!only || others.length > 0) return undefined;
  return only.resolved?.label ?? only.value;
}

/** The side of the diff type:added or type:removed limits commit search to; null for both. */
export function diffSide(query: ParsedQuery | undefined): "added" | "removed" | null {
  const type = query?.globals.type;
  return type === "added" || type === "removed" ? type : null;
}
