// Spelunk's one write path: turns the daemon's replace/plan into the edits
// to apply, keeping only the files whose text still matches what the daemon
// read. It has no vscode import, so tests run it in plain Node; extension.ts
// reads the documents and applies the edits as one WorkspaceEdit.
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
export interface CheckedReplace {
  edits: LineEdit[];
  /** The absolute paths of the files the edits change. */
  files: string[];
  /** The repo-relative paths of the files left alone. */
  skipped: string[];
}

/**
 * Keeps the files of `plan` whose every planned line still reads as the
 * daemon read it. A file edited since it was indexed (unsaved, or saved
 * but not yet re-indexed) is skipped whole: its ranges might cut the wrong
 * characters, and half a file's matches replaced would be worse than none.
 * `open` reads a document, or returns undefined when it can't be read.
 */
export async function checkReplace(
  plan: ReplacePlan,
  open: (file: string) => Promise<DocumentLines | undefined>,
): Promise<CheckedReplace> {
  const checked: CheckedReplace = { edits: [], files: [], skipped: [] };
  for (const file of plan.files) {
    const document = await open(file.file);
    if (!document || !unchanged(file, document)) {
      checked.skipped.push(file.path);
      continue;
    }
    checked.files.push(file.file);
    for (const { line, edits } of file.lines) {
      for (const { start, end, newText } of edits) {
        checked.edits.push({ file: file.file, line: line - 1, start, end, newText });
      }
    }
  }
  return checked;
}

/** Whether each line `file` plans to edit reads in `document` exactly as the daemon read it. */
function unchanged(file: ReplaceFile, document: DocumentLines): boolean {
  return file.lines.every(
    ({ line, text }) => line >= 1 && line <= document.lineCount && document.lineText(line - 1) === text,
  );
}
