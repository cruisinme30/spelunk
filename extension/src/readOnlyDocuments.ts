// The read-only documents results open in: a commit as a diff, and a file as
// it was at a revision (a notebook in the notebook editor), opened at its match.
import { existsSync } from "node:fs";
import * as vscode from "vscode";
import { CommitDocuments } from "./commitDocuments";
import type { Daemon } from "./daemon";
import { openNotebookAt, RevisionNotebooks } from "./notebookEditor";
import { isNotebookPath } from "./notebookLines";
import type { OpenTarget, OpenWhere } from "./protocol.gen";
import { RevisionDocuments } from "./revisionDocuments";
import { revisionDiffTitle } from "./revisionPath";

/** The read-only documents results open in: commits as diffs, and files and notebooks as they were at a revision. */
export interface ReadOnlyDocuments {
  commits: CommitDocuments;
  revisions: RevisionDocuments;
  notebooks: RevisionNotebooks;
}

/** The read-only documents, read from `daemon`. */
export function readOnlyDocuments(daemon: Daemon): ReadOnlyDocuments {
  return {
    commits: new CommitDocuments(daemon),
    revisions: new RevisionDocuments(daemon),
    notebooks: new RevisionNotebooks(daemon),
  };
}

/** Registers the read-only documents' schemes with VS Code. */
export function registerReadOnlyDocuments(documents: ReadOnlyDocuments): vscode.Disposable[] {
  return [
    vscode.workspace.registerTextDocumentContentProvider(CommitDocuments.scheme, documents.commits),
    vscode.workspace.registerTextDocumentContentProvider(RevisionDocuments.scheme, documents.revisions),
    vscode.workspace.registerFileSystemProvider(RevisionNotebooks.scheme, documents.notebooks, {
      isReadonly: true,
      isCaseSensitive: true,
    }),
  ];
}

/**
 * The selection a file target makes: its match, from its line and column to
 * endLine and endColumn when it spans lines. OpenTarget lines and columns are
 * 1-based; VS Code positions are 0-based.
 */
export function selectionOf(target: OpenTarget): vscode.Selection {
  const line = Math.max(0, (target.line ?? 1) - 1);
  const column = Math.max(0, (target.column ?? 1) - 1);
  if (target.endLine === undefined) return new vscode.Selection(line, column, line, column + (target.length ?? 0));
  return new vscode.Selection(
    line,
    column,
    Math.max(line, target.endLine - 1),
    Math.max(0, (target.endColumn ?? 1) - 1),
  );
}

/** A result at a revision to open: where, whether in a preview editor, and its ref. */
export interface RevisionOpening {
  target: OpenTarget;
  where: OpenWhere;
  preview: boolean;
  ref: string;
}

/**
 * Opens a result at a revision: its file as it was, read-only, with the match selected; or, for compare, a diff of
 * that file against the file now. A file deleted since has nothing to compare with, so it opens read-only instead.
 * A notebook opens in the notebook editor. `path` is the file's repo-relative path.
 */
export async function openAtRevision(
  documents: ReadOnlyDocuments,
  { target, where, preview, ref }: RevisionOpening,
  path: string,
): Promise<void> {
  const label = target.revisionLabel ?? target.revision?.slice(0, 7) ?? "";
  // The notebook editor reads files; anything else is a text document.
  const notebook = isNotebookPath(path);
  const uri = (notebook ? documents.notebooks : documents.revisions).uriFor(ref, label, path);
  if (where === "compare" && target.path && existsSync(target.path)) {
    await vscode.commands.executeCommand(
      "vscode.diff",
      uri,
      vscode.Uri.file(target.path),
      revisionDiffTitle(label, path),
      {
        preview,
      },
    );
    return;
  }
  const viewColumn = where === "side" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
  if (notebook) {
    await openNotebookAt(uri, target, { viewColumn, preview });
    return;
  }
  const selection = selectionOf(target);
  const editor = await vscode.window.showTextDocument(uri, { viewColumn, preview, selection });
  editor.revealRange(selection, vscode.TextEditorRevealType.InCenterIfOutsideViewport);
}
