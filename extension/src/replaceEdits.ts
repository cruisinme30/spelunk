// Spelunk's one write path: turns the daemon's replace/plan into the edits
// to apply, keeping only the files whose text still matches what the daemon
// read. It has no vscode import, so tests run it in plain Node; extension.ts
// reads the documents and applies the edits as one WorkspaceEdit.
import { createHash } from "node:crypto";
import { resolve, sep } from "node:path";
import { editorLanguage } from "./languageIds";
import type { ReplaceCell, ReplaceFile, ReplacePlan } from "./protocol.gen";

/** A document's lines as the editor has them, unsaved changes included. */
export interface DocumentLines {
  lineCount: number;
  /** Line `index` (0-based) without its line end. */
  lineText(index: number): string;
  /** Source cells with editor language IDs, when this is a notebook. */
  cells?: readonly ReplaceCell[];
  /** Searchable notebook outputs, independently fingerprinted from editable source. */
  outputHash?: string;
  /** Whether the live document still has the version this view captured. */
  isCurrent?(): boolean;
  /** Whether the original confined path is still safe to save, after our edit changes its version. */
  pathIsCurrent?(): boolean;
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
  /** Recheck live document versions immediately before submitting the edit. */
  isCurrent: () => boolean;
  /** Recheck only path identities when saving the edited documents. */
  pathsCurrent: () => boolean;
}

/**
 * How many documents a replace reads at once: enough that reading hundreds
 * of files isn't one after another, few enough not to swamp the extension
 * host, which loads each document it opens.
 */
export const OPEN_AT_ONCE = 16;

/**
 * Keeps the files of `plan` whose complete source and notebook structure
 * still match the indexed generation. A file edited since it was indexed (unsaved, or saved
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
  open: (file: string, planned: readonly ReplaceFile[]) => Promise<DocumentLines | undefined>,
): Promise<CheckedReplace> {
  const accepted: DocumentLines[] = [];
  const checked: CheckedReplace = {
    edits: [],
    files: [],
    skipped: [],
    isCurrent: () => accepted.every((document) => document.isCurrent?.() ?? true),
    pathsCurrent: () => accepted.every((document) => document.pathIsCurrent?.() ?? true),
  };
  const groups = groupBy(plan.files, (file) => fileIdentity(file.file));
  const documents = await mapAtMost(groups, OPEN_AT_ONCE, ({ first, all }) => open(first.file, all));
  for (const [index, { first, all }] of groups.entries()) {
    const document = documents[index];
    const sourceHash = document ? documentHash(document) : undefined;
    const edits =
      document && all.every((entry) => unchanged(entry, document, sourceHash))
        ? mergedEdits(first.file, all)
        : undefined;
    if (!document || !edits) {
      checked.skipped.push(first.path);
      continue;
    }
    accepted.push(document);
    checked.files.push(first.file);
    checked.edits.push(...edits);
  }
  return checked;
}

/**
 * A file as VS Code's URIs name it, so two spellings of one file are one:
 * absolute, with a Windows drive letter in lower case and its separators
 * normalized. On POSIX a backslash is a filename character.
 */
export function fileIdentity(file: string): string {
  const resolved = resolve(file);
  if (sep !== "\\") return resolved;
  const path = resolved.replaceAll("\\", "/");
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

/** The indexed source digest: UTF-8 source lines, each followed by LF. */
export function documentHash(document: DocumentLines): string {
  const digest = createHash("sha256");
  for (let index = 0; index < document.lineCount; index++) {
    digest.update(document.lineText(index), "utf8");
    digest.update("\n");
  }
  return digest.digest("hex");
}

/** Whether the complete source, cell boundaries and every planned line still match. */
function unchanged(file: ReplaceFile, document: DocumentLines, sourceHash: string | undefined): boolean {
  return (
    file.sourceHash === sourceHash &&
    (file.outputHash === undefined || file.outputHash === document.outputHash) &&
    sameCells(file.cells, document.cells) &&
    (document.isCurrent?.() ?? true) &&
    file.lines.every(
      ({ line, text, edits }) =>
        Number.isSafeInteger(line) &&
        line >= 1 &&
        line <= document.lineCount &&
        document.lineText(line - 1) === text &&
        edits.every(({ start, end }) => validRange(text, start, end)),
    )
  );
}

/** A plan cannot reuse offsets across notebook cell or language changes. */
function sameCells(expected: ReplaceCell[] | undefined, actual: readonly ReplaceCell[] | undefined): boolean {
  if (!expected || !actual) return expected === actual;
  return (
    expected.length === actual.length &&
    expected.every((cell, index) => {
      const live = actual[index];
      return (
        live?.lineCount === cell.lineCount &&
        live.kind === cell.kind &&
        (cell.kind === "raw" || editorLanguage(cell.language) === live.language)
      );
    })
  );
}

/** Reject malformed ranges instead of letting the editor clamp them onto unrelated text. */
function validRange(text: string, start: number, end: number): boolean {
  return (
    Number.isSafeInteger(start) &&
    Number.isSafeInteger(end) &&
    start >= 0 &&
    start < end &&
    end <= text.length &&
    !splitsSurrogate(text, start) &&
    !splitsSurrogate(text, end)
  );
}

/** Whether a UTF-16 offset cuts a supplementary character in half. */
function splitsSurrogate(text: string, offset: number): boolean {
  return (text.codePointAt(offset - 1) ?? 0) > 65_535;
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

/** What a replace plan would change, as one string: every file, line, text and edit, in order. */
export function replacementPlanKey(plan: ReplacePlan): string {
  return JSON.stringify(
    plan.files.map((file) => [
      file.repoId,
      file.path,
      file.file,
      file.sourceHash,
      file.outputHash,
      file.cells,
      file.lines,
    ]),
  );
}
