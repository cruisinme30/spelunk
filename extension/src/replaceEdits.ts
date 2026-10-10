// Spelunk's one write path: turns the daemon's replace/plan into the edits
// to apply, keeping only the files whose text still matches what the daemon
// read. It has no vscode import, so tests run it in plain Node; extension.ts
// reads the documents and applies the edits as one WorkspaceEdit.
import { resolve } from "node:path";
import type { ReplaceFile, ReplacePlan } from "./protocol.gen";

/** A document's lines as the editor has them, unsaved changes included. */
export interface DocumentLines {
  lineCount: number;
  /** Line `index` (0-based) without its line end. */
  lineText(index: number): string;
}

/** One match to replace: a range in a 0-based line of `file`, in UTF-16 units. */
export interface LineEdit {
  file: string;
  line: number;
  start: number;
  end: number;
  newText: string;
}

/** What a replace will change, and the files it leaves alone. */
interface CheckedReplace {
  edits: LineEdit[];
  /** The absolute paths of the files the edits change. */
  files: string[];
  /** The repo-relative paths of the files left alone. */
  skipped: string[];
}

/**
 * How many documents a replace reads at once: enough that reading hundreds
 * of files isn't one after another, few enough not to swamp the extension
 * host, which loads each document it opens.
 */
export const OPEN_AT_ONCE = 16;

/**
 * Keeps the files of `plan` whose every planned line still reads as the
 * daemon read it. A file edited since it was indexed (unsaved, or saved
 * but not yet re-indexed) is skipped whole: its ranges might cut the wrong
 * characters, and half a file's matches replaced would be worse than none.
 * A file the plan lists more than once (under two workspace folders, one
 * inside the other) is edited once: its entries' edits are merged, the same
 * edit kept once, and the file is skipped whole if they would overlap.
 * `open` reads a document, or returns undefined when it can't be read; up
 * to OPEN_AT_ONCE are read at a time, and the result keeps the plan's order.
 */
export async function checkReplace(
  plan: ReplacePlan,
  open: (file: string) => Promise<DocumentLines | undefined>,
): Promise<CheckedReplace> {
  const checked: CheckedReplace = { edits: [], files: [], skipped: [] };
  const groups = groupBy(plan.files, (file) => fileIdentity(file.file));
  const documents = await mapAtMost(groups, OPEN_AT_ONCE, ({ first }) => open(first.file));
  for (const [index, { first, all }] of groups.entries()) {
    const document = documents[index];
    const edits =
      document && all.every((entry) => unchanged(entry, document)) ? mergedEdits(first.file, all) : undefined;
    if (!edits) {
      checked.skipped.push(first.path);
      continue;
    }
    checked.files.push(first.file);
    checked.edits.push(...edits);
  }
  return checked;
}

/**
 * A file as VS Code's URIs name it, so two spellings of one file are one:
 * absolute, with "/" separators and a Windows drive letter in lower case.
 */
export function fileIdentity(file: string): string {
  const path = resolve(file).replaceAll("\\", "/");
  return /^[A-Za-z]:/.test(path) ? path.charAt(0).toLowerCase() + path.slice(1) : path;
}

/**
 * The edits of one file's plan entries, each distinct edit once, in line and
 * column order; undefined when two of them overlap, which no single edit of
 * the file could apply.
 */
function mergedEdits(file: string, entries: ReplaceFile[]): LineEdit[] | undefined {
  const byKey = new Map<string, LineEdit>();
  for (const entry of entries) {
    for (const { line, edits } of entry.lines) {
      for (const { start, end, newText } of edits) {
        const edit = { file, line: line - 1, start, end, newText };
        byKey.set(JSON.stringify([edit.line, start, end, newText]), edit);
      }
    }
  }
  const edits = [...byKey.values()].sort((a, b) => a.line - b.line || a.start - b.start || a.end - b.end);
  const overlaps = edits.some((edit, index) => {
    const previous = edits[index - 1];
    // Two edits at one point would insert in an undefined order, so they count as overlapping too.
    return previous?.line === edit.line && (edit.start < previous.end || edit.start === previous.start);
  });
  return overlaps ? undefined : edits;
}

/** Whether each line `file` plans to edit reads in `document` exactly as the daemon read it. */
function unchanged(file: ReplaceFile, document: DocumentLines): boolean {
  return file.lines.every(
    ({ line, text }) => line >= 1 && line <= document.lineCount && document.lineText(line - 1) === text,
  );
}

/** `items` in groups of equal `key`, each with its first item, in the order the groups first appear. */
function groupBy<T>(items: readonly T[], key: (item: T) => string): { first: T; all: T[] }[] {
  const groups = new Map<string, { first: T; all: T[] }>();
  for (const item of items) {
    const group = groups.get(key(item));
    if (group) group.all.push(item);
    else groups.set(key(item), { first: item, all: [item] });
  }
  return [...groups.values()];
}

/** Maps `items` through `map`, running at most `limit` at a time, keeping their order. */
async function mapAtMost<T, R>(items: readonly T[], limit: number, map: (item: T) => Promise<R>): Promise<R[]> {
  const results: R[] = Array.from({ length: items.length });
  let next = 0;
  const worker = async () => {
    while (next < items.length) {
      const index = next++;
      results[index] = await map(items[index] as T);
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
  return results;
}
