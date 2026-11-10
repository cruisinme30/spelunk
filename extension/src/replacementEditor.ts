// The replacement editor boundary: verify trusted paths, capture live document
// versions, and submit all text changes together after the last asynchronous open.
import * as vscode from "vscode";
import { addLineEdit, readNotebook } from "./notebookEditor";
import { isNotebookPath } from "./notebookLines";
import type { ReplaceFile, Root } from "./protocol.gen";
import type { DocumentLines, LineEdit } from "./replaceEdits";
import { validateReplacementPath } from "./replacementPaths";

/** A file as the editor has it, unsaved changes included; undefined when it isn't a text file that opens. */
export async function readReplacementDocument(
  file: string,
  planned: readonly ReplaceFile[],
  roots: () => readonly Root[],
): Promise<DocumentLines | undefined> {
  const paths = planned.map((entry) => validateReplacementPath(entry, roots));
  if (paths.length === 0 || paths.some((guard) => !guard)) return undefined;
  const pathIsCurrent = () => paths.every((guard) => guard?.() === true);
  if (isNotebookPath(file)) {
    const document = await readNotebook(
      file,
      planned.some((entry) => entry.outputHash !== undefined),
    );
    if (!document || !pathIsCurrent()) return undefined;
    return { ...document, pathIsCurrent, isCurrent: () => pathIsCurrent() && (document.isCurrent?.() ?? false) };
  }
  try {
    const document = await vscode.workspace.openTextDocument(vscode.Uri.file(file));
    const version = document.version;
    if (!pathIsCurrent()) return undefined;
    return {
      lineCount: document.lineCount,
      lineText: (index) => document.lineAt(index).text,
      pathIsCurrent,
      isCurrent: () => pathIsCurrent() && !document.isClosed && document.version === version,
    };
  } catch {
    return undefined;
  }
}

/**
 * Applies a replace's edits as one WorkspaceEdit: a single step that ⌘Z
 * undoes in every file at once. The files stay unsaved, as after a rename.
 */
export async function applyReplacementEdits(
  edits: LineEdit[],
  label: string,
  isCurrent: () => boolean,
): Promise<boolean> {
  const edit = new vscode.WorkspaceEdit();
  const metadata: vscode.WorkspaceEditEntryMetadata = { label, needsConfirmation: false };
  for (const lineEdit of edits) {
    if (!(await addLineEdit(edit, lineEdit, metadata))) return false;
  }
  // Assembly can await notebook opens. Validate after its last await; the
  // editor then receives one WorkspaceEdit, with no asynchronous gap here.
  if (!isCurrent()) return false;
  return vscode.workspace.applyEdit(edit);
}
