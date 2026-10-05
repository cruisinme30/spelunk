// Read-only documents that show a commit as a unified diff. Commit results
// open here: result.open on a commit resolves to a sha, and the document's
// content comes from preview/get on the result's ref.
import * as vscode from "vscode";
import { commitDocumentName, renderCommit } from "./commitText";
import type { Daemon } from "./daemon";

/** Unchanged lines shown around each change, as `git show` does. */
const DIFF_CONTEXT_LINES = 3;

/** Provides the `unified-search-commit:` documents that show commits. */
export class CommitDocuments implements vscode.TextDocumentContentProvider {
  static readonly scheme = "unified-search-commit";

  /** Commit contents come from the daemon's preview/get. */
  constructor(private readonly daemon: Daemon) {}

  /** The daemon's opaque ref rides in the URI query; nothing here parses it. */
  uriFor(ref: string, sha: string, subject?: string): vscode.Uri {
    const name = encodeURIComponent(commitDocumentName(sha, subject));
    return vscode.Uri.parse(`${CommitDocuments.scheme}:/${name}`).with({ query: ref });
  }

  /** The diff text for a URI made by uriFor(). */
  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const preview = await this.daemon.request("preview/get", { ref: uri.query, contextLines: DIFF_CONTEXT_LINES });
    return renderCommit(preview);
  }
}
