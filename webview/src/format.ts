// DOM and text helpers shared by the renderers: an element builder, match
// highlighting, relative times, plurals and small numeric helpers.
import type { Hit, Range } from "./protocol.gen";

/** Distinct highlight colors for text terms; term N uses color N mod this. */
export const TERM_COLOR_COUNT = 4;

/** The CSS class that colors text term `termIndex` (mock 7: each OR branch its own color). */
export function termClass(termIndex: number): string {
  return `t${termIndex % TERM_COLOR_COUNT}`;
}

type AttributeValue = string | number | boolean | undefined;
type Child = Node | string | null | undefined | false;

/**
 * Creates an element. `class` and `text` are shortcuts; `false` and
 * `undefined` attributes are skipped, `true` sets an empty attribute.
 * Falsy children are skipped, so `cond ? el(...) : null` reads naturally.
 */
export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attributes: Record<string, AttributeValue> = {},
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const element = document.createElement(tag);
  for (const [name, value] of Object.entries(attributes)) {
    if (value === undefined || value === false) continue;
    if (name === "class") element.className = String(value);
    else if (name === "text") element.textContent = String(value);
    else element.setAttribute(name, value === true ? "" : String(value));
  }
  for (const child of children) {
    if (child !== null && child !== undefined && child !== false) element.append(child);
  }
  return element;
}

/** Splits text into plain runs and <mark> runs. Hits are UTF-16 ranges, which JS strings index directly. */
export function highlight(text: string, hits: (Hit | Range)[]): DocumentFragment {
  const fragment = document.createDocumentFragment();
  const sorted = [...hits].sort((a, b) => a.start - b.start);
  let position = 0;
  for (const hit of sorted) {
    const start = Math.max(hit.start, position);
    const end = Math.min(hit.end, text.length);
    if (end <= start) continue;
    if (start > position) fragment.append(text.slice(position, start));
    const termIndex = "termIndex" in hit ? hit.termIndex : 0;
    fragment.append(el("mark", { class: termClass(termIndex) }, text.slice(start, end)));
    position = end;
  }
  if (position < text.length) fragment.append(text.slice(position));
  return fragment;
}

const TIME_UNITS: [seconds: number, name: string][] = [
  [31_536_000, "year"],
  [2_592_000, "month"],
  [604_800, "week"],
  [86_400, "day"],
  [3_600, "hour"],
  [60, "minute"],
];

/** "3 days ago", from an ISO timestamp. */
export function timeAgo(iso: string, now = Date.now()): string {
  const time = Date.parse(iso);
  if (Number.isNaN(time)) return iso;
  const seconds = Math.max(0, Math.round((now - time) / 1000));
  for (const [unitSeconds, name] of TIME_UNITS) {
    const count = Math.floor(seconds / unitSeconds);
    if (count >= 1) return `${count} ${name}${count === 1 ? "" : "s"} ago`;
  }
  return "just now";
}

/** "1 file" / "3 files", with thousands separators. */
export function plural(count: number, one: string, many = one + "s"): string {
  return `${count.toLocaleString("en-US")} ${count === 1 ? one : many}`;
}

/** The first seven characters of a commit sha, as Git shows them. */
export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

/** Steps through `length` items, wrapping at both ends. */
export function wrapIndex(index: number, step: number, length: number): number {
  return (((index + step) % length) + length) % length;
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

/** "path/to/file.py +9 −1" for a changed file. */
export function fileStat(path: string, added: number, removed: number): HTMLElement {
  return el(
    "span",
    { class: "file" },
    path,
    " ",
    el("span", { class: "add" }, `+${added}`),
    " ",
    el("span", { class: "del" }, `−${removed}`),
  );
}
