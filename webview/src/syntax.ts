// Code in the theme's syntax colors, and whitespace marked as the editor
// marks it. The host colors lines with VS Code's grammars and sends each
// line's tokens by its file and text (syntax.lines); code drawn before its
// colors arrive is colored when they do, and code drawn again (a row whose
// indentation or replace preview changes) is colored again. Coloring wraps
// the text already drawn, matches and whitespace marks included, so it
// never changes what the text says.
import { lastLineOf } from "./format";
import type { ResultItem, SyntaxLinesMessage, UiSettings } from "./protocol.gen";

/** A token's style is a color index plus this times its font style (see SyntaxLine in the protocol). */
const FONT_STYLE_STEP = 512;
/** The font style bits, and the class each one adds. */
const FONT_STYLE_CLASSES = [
  [1, "syn-italic"],
  [2, "syn-bold"],
  [4, "syn-underline"],
  [8, "syn-strike"],
] as const;
/** Lines whose colors are kept; past this, the oldest are dropped (and drawn plain if they show again). */
const MAX_COLORED_LINES = 20_000;

/** Identifies a line by its file and text: lines with both the same color the same. */
const lineKey = (path: string, text: string): string => `${path}\n${text}`;

/** What a colored element shows: `text` of `path`, or the end of it (after the indentation a row drops). */
interface Painted {
  path: string;
  text: string;
}

/** Draws code with the editor's tab size and ligatures, through CSS properties main.css reads. */
export function applyCodeSettings({ tabSize, fontLigatures }: UiSettings): void {
  const root = document.documentElement.style;
  root.setProperty("--code-tab-size", String(tabSize));
  root.setProperty("--code-font-features", fontLigatures);
}

/** The colors the host sent, and the code elements they color. */
export class SyntaxPainter {
  private readonly tokensByLine = new Map<string, number[]>();
  /** The code drawn for each line, colored again when its colors (re)arrive. */
  private readonly painted = new Map<string, Set<HTMLElement>>();
  private readonly shown = new WeakMap<HTMLElement, Painted>();
  private readonly sheet = new CSSStyleSheet();
  /** Sees code drawn again, to color it again. */
  private readonly redrawn: MutationObserver;

  /** Adds the sheet of the theme's colors to the page. */
  constructor() {
    // A constructed sheet, through the CSSOM: the content security policy blocks <style> and style attributes.
    document.adoptedStyleSheets = [...document.adoptedStyleSheets, this.sheet];
    this.redrawn = new MutationObserver((records) => {
      this.onRedrawn(records);
    });
  }

  /** Colors code drawn again; code with token spans in it is what coloring itself changed. */
  private onRedrawn(records: MutationRecord[]): void {
    for (const code of new Set(records.map((record) => record.target as HTMLElement))) {
      const shown = this.shown.get(code);
      const tokens = shown && this.tokensByLine.get(lineKey(shown.path, shown.text));
      if (tokens && code.isConnected && !code.querySelector("span.syn")) this.color(code, tokens);
    }
  }

  /** Takes a syntax.lines message: the theme's colors, and the tokens of lines, which color their code on screen. */
  receive({ colors, lines, reset }: SyntaxLinesMessage): void {
    if (reset) {
      this.tokensByLine.clear();
      for (const elements of this.painted.values()) for (const code of elements) removeTokenSpans(code);
    }
    if (colors.length > 0 || reset) this.setColors(colors);
    for (const { path, text, tokens } of lines) {
      const key = lineKey(path, text);
      this.tokensByLine.delete(key); // so it counts as the newest
      this.tokensByLine.set(key, tokens);
      for (const code of this.drawn(key)) this.color(code, tokens);
    }
    for (const key of this.tokensByLine.keys()) {
      if (this.tokensByLine.size <= MAX_COLORED_LINES) break;
      this.tokensByLine.delete(key);
    }
  }

  /**
   * Colors `code`, which shows `text` of `path`, or the end of it: now if
   * its colors are here, or else when they arrive. Code showing anything
   * else (a replace's preview) stays plain.
   */
  paint(code: HTMLElement, path: string, text: string): void {
    const key = lineKey(path, text);
    const previous = this.shown.get(code);
    if (previous) this.painted.get(lineKey(previous.path, previous.text))?.delete(code);
    this.shown.set(code, { path, text });
    let elements = this.painted.get(key);
    if (!elements) {
      elements = new Set();
      this.painted.set(key, elements);
    }
    elements.add(code);
    this.redrawn.observe(code, { childList: true });
    const tokens = this.tokensByLine.get(key);
    if (tokens) this.color(code, tokens);
  }

  /** Colors the code of a code result's `node`: its line, each later line of a match over lines, and its context. */
  paintCode(node: ParentNode, item: Extract<ResultItem, { kind: "line" }>): void {
    const matchLines = [item.text, ...(item.spanLines ?? []).map((line) => line.text)];
    const codes = [...node.querySelectorAll<HTMLElement>(".row.line code.text")];
    for (const [index, code] of codes.entries()) {
      const text = matchLines[index];
      if (text !== undefined) this.paint(code, item.path, text);
    }
    const before = item.contextBefore ?? [];
    const textAt = new Map<number, string>([
      ...before.map((text, index): [number, string] => [item.line - before.length + index, text]),
      ...(item.contextAfter ?? []).map((text, index): [number, string] => [lastLineOf(item) + 1 + index, text]),
    ]);
    for (const row of node.querySelectorAll<HTMLElement>(".context-line[data-line]")) {
      const text = textAt.get(Number(row.dataset["line"]));
      const code = row.querySelector<HTMLElement>("code.text");
      if (text !== undefined && code) this.paint(code, item.path, text);
    }
  }

  /** The code on screen that shows the line `key`; code no longer on screen is forgotten. */
  private drawn(key: string): HTMLElement[] {
    const elements = this.painted.get(key);
    if (!elements) return [];
    for (const code of elements) if (!code.isConnected) elements.delete(code);
    if (elements.size === 0) this.painted.delete(key);
    return [...elements];
  }

  private color(code: HTMLElement, tokens: readonly number[]): void {
    const shown = this.shown.get(code);
    if (!shown) return;
    removeTokenSpans(code);
    const visible = code.textContent;
    if (shown.text.endsWith(visible) && !code.querySelector("ins, del")) {
      paintTokens(code, tokens, shown.text.length - visible.length);
    }
  }

  /** One class per theme color: syn-N colors a token of color index N. */
  private setColors(colors: readonly string[]): void {
    const rules = colors.flatMap((color, index) =>
      index > 0 && /^#[\da-f]{3,8}$/i.test(color) ? [`.syn-${String(index)} { color: ${color}; }`] : [],
    );
    this.sheet.replaceSync(rules.join("\n"));
  }
}

/** Wraps the text of `code`, which starts at offset `from` of its line, in spans of its tokens' styles. */
function paintTokens(code: HTMLElement, tokens: readonly number[], from: number): void {
  const walker = document.createTreeWalker(code, NodeFilter.SHOW_TEXT);
  const nodes: Text[] = [];
  for (let node = walker.nextNode(); node; node = walker.nextNode()) nodes.push(node as Text);
  let offset = from;
  let token = 0;
  for (const node of nodes) {
    const value = node.data;
    const end = offset + value.length;
    const pieces: (Node | string)[] = [];
    let position = offset;
    while (position < end) {
      // The token that covers `position`: the last one starting at or before it.
      while (token + 2 < tokens.length && (tokens[token + 2] ?? 0) <= position) token += 2;
      const nextStart = token + 2 < tokens.length ? (tokens[token + 2] ?? end) : end;
      const stop = Math.min(end, Math.max(nextStart, position + 1));
      const text = value.slice(position - offset, stop - offset);
      const style = (tokens[token] ?? 0) <= position ? (tokens[token + 1] ?? 0) : 0;
      pieces.push(style > 0 ? styled(text, style) : text);
      position = stop;
    }
    if (pieces.length !== 1 || typeof pieces[0] !== "string") node.replaceWith(...pieces);
    offset = end;
  }
}

function styled(text: string, style: number): HTMLElement {
  const span = document.createElement("span");
  const color = style % FONT_STYLE_STEP;
  const fontStyle = Math.floor(style / FONT_STYLE_STEP);
  span.className = [
    "syn",
    color > 0 ? `syn-${String(color)}` : "",
    ...FONT_STYLE_CLASSES.filter(([bit]) => (fontStyle & bit) !== 0).map(([, name]) => name),
  ]
    .filter(Boolean)
    .join(" ");
  span.textContent = text;
  return span;
}

/** Takes the token spans back out of `code`, leaving its text, matches and whitespace marks. */
function removeTokenSpans(code: HTMLElement): void {
  const spans = code.querySelectorAll("span.syn");
  if (spans.length === 0) return;
  for (const span of spans) span.replaceWith(...span.childNodes);
  code.normalize();
}

/**
 * Marks the whitespace of `code`, which shows `text` from its start, as
 * editor.renderWhitespace `mode` would: a faint dot for each space, an
 * arrow for each tab. "selection" marks it on `selected` lines, where the
 * match is, the way the editor marks it in a selection.
 */
export function markWhitespace(
  code: HTMLElement,
  text: string,
  mode: UiSettings["renderWhitespace"],
  selected: boolean,
): void {
  const marked = markedWhitespace(text, mode, selected);
  if (marked.size === 0 || code.textContent !== text) return;
  const walker = document.createTreeWalker(code, NodeFilter.SHOW_TEXT);
  const nodes: Text[] = [];
  for (let node = walker.nextNode(); node; node = walker.nextNode()) nodes.push(node as Text);
  let offset = 0;
  for (const node of nodes) {
    const value = node.data;
    const pieces: (Node | string)[] = [];
    let plain = "";
    for (let index = 0; index < value.length; index++) {
      const char = value.charAt(index);
      if (!marked.has(offset + index)) {
        plain += char;
        continue;
      }
      if (plain) pieces.push(plain);
      plain = "";
      const mark = document.createElement("span");
      mark.className = char === "\t" ? "ws ws-tab" : "ws ws-space";
      mark.textContent = char;
      pieces.push(mark);
    }
    if (plain) pieces.push(plain);
    if (pieces.length !== 1 || typeof pieces[0] !== "string") node.replaceWith(...pieces);
    offset += value.length;
  }
}

/** The offsets of the spaces and tabs of `text` that `mode` marks. */
function markedWhitespace(text: string, mode: UiSettings["renderWhitespace"], selected: boolean): Set<number> {
  const marked = new Set<number>();
  if (mode === "none" || (mode === "selection" && !selected)) return marked;
  for (const run of text.matchAll(/[ \t]+/g)) {
    const start = run.index;
    const end = start + run[0].length;
    const leading = start === 0;
    const trailing = end === text.length;
    const keep =
      mode === "all" ||
      mode === "selection" ||
      (mode === "trailing" && trailing) ||
      (mode === "boundary" && (leading || trailing || run[0].length > 1 || run[0].includes("\t")));
    if (keep) for (let offset = start; offset < end; offset++) marked.add(offset);
  }
  return marked;
}
