// Reads the mock tables: docs/dev/mocks.md lists every screen of the panel
// once, and the wiki's Screens page lists the same screens grouped as on the
// design canvas. Both are Markdown tables with a "Screen id" column.
import { readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * The rows of every Markdown table in `text` that has a "Screen id" column,
 * keyed by header ("#", "Screen id", "Milestone", ...). Backticks are stripped.
 * @param {string} text Markdown source.
 * @returns {Record<string, string>[]} One object per row.
 */
export function screenRows(text) {
  const rows = [];
  let header;
  for (const line of text.split("\n")) {
    if (!line.startsWith("|")) {
      header = undefined;
      continue;
    }
    const cells = line
      .slice(1, line.trimEnd().endsWith("|") ? line.trimEnd().length - 1 : undefined)
      .split("|")
      .map((cell) => cell.trim().replaceAll("`", ""));
    if (!header) header = cells;
    else if (!cells.every((cell) => /^:?-+:?$/.test(cell)) && header.includes("Screen id")) {
      rows.push(Object.fromEntries(header.map((name, index) => [name, cells[index] ?? ""])));
    }
  }
  return rows;
}

/**
 * The panel's screen ids, in mock order, from docs/dev/mocks.md.
 * @param {string} root The repository root.
 * @returns {string[]} Screen ids such as "plain-text-search".
 */
export function screenIds(root) {
  return screenRows(readFileSync(join(root, "docs/dev/mocks.md"), "utf8")).map((row) => row["Screen id"]);
}
