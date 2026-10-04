// Small display helpers shared by the renderers.
import type { Hit, Range } from "./protocol.gen";

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string | number | boolean | undefined> = {},
  ...children: (Node | string | null | undefined | false)[]
): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === false) continue;
    if (k === "class") e.className = String(v);
    else if (k === "text") e.textContent = String(v);
    else e.setAttribute(k, v === true ? "" : String(v));
  }
  for (const c of children) if (c !== null && c !== undefined && c !== false) e.append(c);
  return e;
}

/** Splits text into plain and <mark> parts; hits carry termIndex for colors. */
export function highlight(text: string, hits: (Hit | Range)[]): DocumentFragment {
  const frag = document.createDocumentFragment();
  const sorted = [...hits].sort((a, b) => a.start - b.start);
  let pos = 0;
  for (const h of sorted) {
    const start = Math.max(h.start, pos);
    const end = Math.min(h.end, text.length);
    if (end <= start) continue;
    if (start > pos) frag.append(text.slice(pos, start));
    const term = "termIndex" in h ? h.termIndex : 0;
    frag.append(el("mark", { class: `t${term % 4}` }, text.slice(start, end)));
    pos = end;
  }
  if (pos < text.length) frag.append(text.slice(pos));
  return frag;
}

export function timeAgo(iso: string, now = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const s = Math.max(0, Math.round((now - t) / 1000));
  const units: [number, string][] = [[31536000, "year"], [2592000, "month"], [604800, "week"], [86400, "day"], [3600, "hour"], [60, "minute"]];
  for (const [secs, name] of units) {
    const n = Math.floor(s / secs);
    if (n >= 1) return `${n} ${name}${n === 1 ? "" : "s"} ago`;
  }
  return "just now";
}

export function plural(n: number, one: string, many = one + "s"): string {
  return `${n.toLocaleString("en-US")} ${n === 1 ? one : many}`;
}

export function splitPath(path: string): [string, string] {
  const i = path.lastIndexOf("/");
  return i < 0 ? ["", path] : [path.slice(0, i + 1), path.slice(i + 1)];
}
