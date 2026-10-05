// DOM and text helpers shared by the renderers: an element builder, match
// highlighting, progress bars, relative times, plurals and small numeric
// helpers.
import type { Hit, IndexState, Range } from "./protocol.gen";

/** Distinct highlight colors for text terms (main.css defines term-color-0 to -3); term N uses color N mod this. */
const TERM_COLOR_COUNT = 4;

/** The CSS class that colors text term `termIndex`: each term, and so each OR branch, has its own color. */
export function termClass(termIndex: number): string {
  // An index that isn't a whole number >= 0 still gets a color class that exists.
  const index = Number.isSafeInteger(termIndex) ? wrapIndex(termIndex, 0, TERM_COLOR_COUNT) : 0;
  return `term-color-${index}`;
}

type AttributeValue = string | number | boolean | undefined;
type Child = Node | string | null | undefined | false;

/**
 * Creates an element. `class` and `text` are shortcuts; `false` and
 * `undefined` attributes are skipped, `true` sets an empty attribute.
 * Falsy children are skipped, so `condition ? element(...) : null` reads naturally.
 */
export function element<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attributes: Record<string, AttributeValue> = {},
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const created = document.createElement(tag);
  for (const [name, value] of Object.entries(attributes)) {
    if (value === undefined || value === false) continue;
    switch (name) {
      case "class": {
        created.className = String(value);
        break;
      }
      case "text": {
        created.textContent = String(value);
        break;
      }
      case "style": {
        // Through the CSSOM: the webviews' content security policy blocks style attributes.
        created.style.cssText = String(value);
        break;
      }
      default: {
        created.setAttribute(name, value === true ? "" : String(value));
      }
    }
  }
  for (const child of children) {
    if (child !== null && child !== undefined && child !== false) created.append(child);
  }
  return created;
}

/** A decorative icon: `svg`, a constant of the page's own markup, in a span screen readers skip. */
export function icon(svg: string): HTMLElement {
  const span = element("span", { class: "icon", "aria-hidden": "true" });
  span.innerHTML = svg;
  return span;
}

/** A `<button type="button">` that calls `onClick` when clicked. */
export function button(
  attributes: Record<string, AttributeValue>,
  onClick: () => void,
  ...children: Child[]
): HTMLButtonElement {
  const created = element("button", { type: "button", ...attributes }, ...children);
  created.addEventListener("click", () => {
    onClick();
  });
  return created;
}

/** Whether UTF-16 offset `index` falls between the two halves of a surrogate pair. */
function splitsSurrogatePair(text: string, index: number): boolean {
  const before = text.codePointAt(index - 1);
  return before !== undefined && before > 0xff_ff;
}

/**
 * Splits text into plain runs and <mark> runs. Hits are UTF-16 ranges, which
 * JS strings index directly. Hits that overlap, are out of order or out of
 * range, or aren't numbers are clipped or skipped, and a hit that would cut
 * an astral character (an emoji) in half grows to cover all of it.
 */
export function highlight(text: string, hits: (Hit | Range)[]): DocumentFragment {
  const fragment = document.createDocumentFragment();
  const sorted = hits
    .filter((hit) => Number.isFinite(hit.start) && Number.isFinite(hit.end))
    .sort((first, second) => first.start - second.start);
  let position = 0;
  for (const hit of sorted) {
    let start = Math.max(hit.start, position);
    let end = Math.min(hit.end, text.length);
    if (end <= start) continue;
    if (splitsSurrogatePair(text, start)) start--;
    if (splitsSurrogatePair(text, end)) end++;
    if (start > position) fragment.append(text.slice(position, start));
    const termIndex = "termIndex" in hit ? hit.termIndex : 0;
    fragment.append(element("mark", { class: termClass(termIndex) }, text.slice(start, end)));
    position = end;
  }
  if (position < text.length) fragment.append(text.slice(position));
  return fragment;
}

/** Drops a code line's indentation, shifting its hits to match, so result rows line up. */
function trimIndent<H extends Range>(text: string, hits: H[]): { text: string; hits: H[] } {
  const indent = text.length - text.trimStart().length;
  if (indent === 0) return { text, hits };
  return {
    text: text.slice(indent),
    hits: hits
      .filter((hit) => hit.end > indent)
      .map((hit) => ({ ...hit, start: Math.max(hit.start, indent) - indent, end: hit.end - indent })),
  };
}

const TIME_UNITS: [seconds: number, name: string][] = [
  [31_536_000, "year"],
  [2_592_000, "month"],
  [604_800, "week"],
  [86_400, "day"],
  [3600, "hour"],
  [60, "minute"],
];

/** "3 days ago", from an ISO timestamp. */
export function timeAgo(iso: string, now = Date.now()): string {
  const time = Date.parse(iso);
  if (Number.isNaN(time)) return iso;
  const seconds = Math.max(0, Math.round((now - time) / 1000));
  for (const [unitSeconds, name] of TIME_UNITS) {
    const count = Math.floor(seconds / unitSeconds);
    if (count >= 1) return `${plural(count, name)} ago`;
  }
  return "just now";
}

/**
 * Scrolls `container` alone so `child` shows: just into view, or centred.
 * Unlike scrollIntoView, it never scrolls the panel around the container,
 * which in a short panel would push the query box out of sight.
 */
export function scrollIntoContainer(container: HTMLElement, child: HTMLElement, align: "nearest" | "center"): void {
  const top = child.getBoundingClientRect().top - container.getBoundingClientRect().top - container.clientTop;
  const { height } = child.getBoundingClientRect();
  const visible = container.clientHeight;
  if (align === "center") container.scrollTop += top + height / 2 - visible / 2;
  else if (top < 0) container.scrollTop += top;
  else if (top + height > visible) container.scrollTop += Math.min(top, top + height - visible);
}

/** "1 file" / "3 files", with thousands separators. */
export function plural(count: number, one: string, many = one + "s"): string {
  return `${count.toLocaleString("en-US")} ${count === 1 ? one : many}`;
}

/** A code line as results show it: its number, then its text without the indent, with `hits` marked. */
export function codeLineRow(
  line: { line: number; text: string; hits: Hit[] },
  attributes: Record<string, AttributeValue> = {},
): HTMLDivElement {
  const shown = trimIndent(line.text, line.hits);
  return element(
    "div",
    { ...attributes, class: "row line" },
    element("span", { class: "line-number" }, String(line.line)),
    element("code", { class: "text" }, highlight(shown.text, shown.hits)),
  );
}

/** The first seven characters of a commit sha, as Git shows them. */
export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

/** Steps through `length` items, wrapping at both ends. */
export function wrapIndex(index: number, step: number, length: number): number {
  return (((index + step) % length) + length) % length;
}

/** `value`, kept within `min`..`max`. */
export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

/** An indexing progress fraction (0..1) as a whole percentage, kept within 0..100. */
export function percent(progress: number | undefined): number {
  return Number.isFinite(progress) ? Math.round(clamp(progress ?? 0, 0, 1) * 100) : 0;
}

/** "path/to/file.py +9 −1" for a changed file. */
export function fileStat(path: string, added: number, removed: number): HTMLElement {
  return element(
    "span",
    { class: "file" },
    path,
    " ",
    element("span", { class: "add" }, `+${added}`),
    " ",
    element("span", { class: "del" }, `−${removed}`),
  );
}

/** Whether an index is being built or waits its turn. */
export function isIndexing(state: IndexState): boolean {
  return state === "indexing" || state === "queued";
}

/** A progress bar filled to `percentDone` (0..100). */
export function progressBar(percentDone: number, label?: string): HTMLElement {
  return element(
    "span",
    {
      class: "progress",
      role: "progressbar",
      "aria-valuenow": percentDone,
      "aria-valuemin": 0,
      "aria-valuemax": 100,
      ...(label === undefined ? {} : { "aria-label": label }),
    },
    element("span", { style: `width:${percentDone}%` }),
  );
}

/** Words as code, joined with "and": <code>a</code> and <code>b</code>. */
export function codeList(words: string[]): (Node | string)[] {
  return words.flatMap((word, index) =>
    index > 0 ? [" and ", element("code", {}, word)] : [element("code", {}, word)],
  );
}
