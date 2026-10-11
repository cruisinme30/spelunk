// Jupyter notebooks in VS Code's notebook editor: opening a result at its
// cell, replacing in cells, and a notebook as it was at a revision. The
// daemon numbers a notebook's lines by its cells' text (see notebookLines).
import * as vscode from "vscode";
import type { Daemon } from "./daemon";
import { cellLineOf, isNotebookPath, notebookLines } from "./notebookLines";
import type { OpenTarget } from "./protocol.gen";
import type { DocumentLines, LineEdit } from "./replaceEdits";
import { revisionDocumentPath } from "./revisionPath";

/**
 * Opens a notebook in the notebook editor at a result: its cell scrolled into view and selected. In a code or raw
 * cell the match is selected in the cell's text; a markdown cell stays rendered, and an output shows below its cell.
 */
export async function openNotebookAt(
  uri: vscode.Uri,
  target: OpenTarget,
  options: { viewColumn: vscode.ViewColumn; preview: boolean },
): Promise<void> {
  const notebook = await vscode.workspace.openNotebookDocument(uri);
  const at = cellOf(notebook, target);
  const range = at ? new vscode.NotebookRange(at.cell.index, at.cell.index + 1) : undefined;
  const editor = await vscode.window.showNotebookDocument(notebook, {
    ...options,
    ...(range ? { selections: [range] } : {}),
  });
  if (!at || !range) return;
  editor.revealRange(range, vscode.NotebookEditorRevealType.InCenterIfOutsideViewport);
  if (at.cell.kind === vscode.NotebookCellKind.Markup || target.cell?.kind === "output") return;
  const line = at.line;
  const column = Math.max(0, (target.column ?? 1) - 1);
  const endLine = target.endLine === undefined ? line : line + target.endLine - (target.line ?? 1);
  const endColumn =
    target.endLine === undefined ? column + (target.length ?? 0) : Math.max(0, (target.endColumn ?? 1) - 1);
  const selection = new vscode.Selection(line, column, endLine, endColumn);
  await vscode.window.showTextDocument(at.cell.document, {
    viewColumn: editor.viewColumn ?? options.viewColumn,
    preview: options.preview,
    selection,
  });
}

/** The cell a target is in, and its line there (0-based): the daemon's cell when it says, else by counting lines. */
function cellOf(
  notebook: vscode.NotebookDocument,
  target: OpenTarget,
): { cell: vscode.NotebookCell; line: number } | undefined {
  if (target.cell) {
    const cell = notebook.cellAt(target.cell.number - 1);
    return cell.index === target.cell.number - 1 ? { cell, line: target.cell.line - 1 } : undefined;
  }
  const cells = notebook.getCells();
  const at = cellLineOf(
    cells.map((cell) => cell.document),
    (target.line ?? 1) - 1,
  );
  const cell = at ? cells[at.cell] : undefined;
  return at && cell ? { cell, line: at.line } : undefined;
}

/** A notebook as replace's check reads it: its cells' text as the editor has it, unsaved edits included. */
export async function readNotebook(file: string): Promise<DocumentLines | undefined> {
  try {
    const notebook = await vscode.workspace.openNotebookDocument(vscode.Uri.file(file));
    return notebookLines(
      notebook.getCells().map((cell) => ({
        lineCount: cell.document.lineCount,
        lineAt: (line: number) => cell.document.lineAt(line).text,
      })),
    );
  } catch {
    return undefined;
  }
}

/**
 * Adds a replace's edit of one line to `edit`: in a file's text, or in a notebook in the cell the line is in, so the
 * notebook's own serializer writes the file, escaping what JSON needs. False when the notebook has no such line.
 */
export async function addLineEdit(
  edit: vscode.WorkspaceEdit,
  { file, line, start, end, newText }: LineEdit,
  metadata: vscode.WorkspaceEditEntryMetadata,
): Promise<boolean> {
  if (!isNotebookPath(file)) {
    edit.replace(vscode.Uri.file(file), new vscode.Range(line, start, line, end), newText, metadata);
    return true;
  }
  const notebook = await vscode.workspace.openNotebookDocument(vscode.Uri.file(file));
  const cells = notebook.getCells();
  const at = cellLineOf(
    cells.map((cell) => cell.document),
    line,
  );
  const cell = at ? cells[at.cell] : undefined;
  if (!at || !cell) return false;
  edit.replace(cell.document.uri, new vscode.Range(at.line, start, at.line, end), newText, metadata);
  return true;
}

/** Saves the file or notebook at `file` when it has unsaved changes, as after a replace. */
export function saveFile(file: string, onError: (error: unknown) => void): void {
  // As URIs, which normalize the drive letter's case that a path may spell either way.
  const uri = vscode.Uri.file(file).toString();
  const open = isNotebookPath(file)
    ? vscode.workspace.notebookDocuments.find((notebook) => notebook.uri.toString() === uri)
    : vscode.workspace.textDocuments.find((document) => document.uri.toString() === uri);
  if (open?.isDirty) void open.save().then(undefined, onError);
}

/**
 * Notebooks as they were at a revision, read-only, for the notebook editor: a file system whose files are the
 * daemon's revision/text, the notebook's JSON. Text files at a revision are RevisionDocuments.
 */
export class RevisionNotebooks implements vscode.FileSystemProvider {
  static readonly scheme = "spelunk-revision-notebook";
  private readonly changes = new vscode.EventEmitter<vscode.FileChangeEvent[]>();
  readonly onDidChangeFile = this.changes.event;

  /** Notebooks' JSON comes from the daemon's revision/text. */
  constructor(private readonly daemon: Daemon) {}

  /** The notebook at `path` (repo-relative) as `label` (v1.0.0) names its revision; the daemon's ref rides in the query. */
  uriFor(ref: string, label: string, path: string): vscode.Uri {
    return vscode.Uri.from({ scheme: RevisionNotebooks.scheme, path: revisionDocumentPath(label, path), query: ref });
  }

  /** A notebook's JSON as it was, for a URI made by uriFor(). */
  async readFile(uri: vscode.Uri): Promise<Uint8Array> {
    const { text } = await this.daemon.request("revision/text", { ref: uri.query });
    return new TextEncoder().encode(text);
  }

  /** A read-only file, as big as its JSON. */
  async stat(uri: vscode.Uri): Promise<vscode.FileStat> {
    const content = await this.readFile(uri);
    const size = content.byteLength;
    return { type: vscode.FileType.File, ctime: 0, mtime: 0, size, permissions: vscode.FilePermission.Readonly };
  }

  /** A revision never changes, so there's nothing to watch. */
  watch(): vscode.Disposable {
    return new vscode.Disposable(nothing);
  }

  /** There are no folders, only notebooks. */
  readDirectory(): never {
    throw vscode.FileSystemError.FileNotADirectory();
  }

  /** Read-only. */
  createDirectory(): never {
    throw vscode.FileSystemError.NoPermissions();
  }

  /** Read-only. */
  writeFile(): never {
    throw vscode.FileSystemError.NoPermissions();
  }

  /** Read-only. */
  delete(): never {
    throw vscode.FileSystemError.NoPermissions();
  }

  /** Read-only. */
  rename(): never {
    throw vscode.FileSystemError.NoPermissions();
  }

  /** Stops the change events, which never fire. */
  dispose(): void {
    this.changes.dispose();
  }
}

/** Does nothing: what a watch of a revision has to stop. */
function nothing(): void {
  // A revision never changes.
}
