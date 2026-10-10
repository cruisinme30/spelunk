// Read-only documents that show a file as it was at a revision. An at:
// result opens here: open/resolve gives the revision, and the document's
// text comes from revision/text on the result's ref.
import * as vscode from "vscode";
import type { Daemon } from "./daemon";
import { revisionDocumentPath } from "./revisionPath";

/** Provides the `spelunk-revision:` documents that show files at a revision. */
export class RevisionDocuments implements vscode.TextDocumentContentProvider {
  static readonly scheme = "spelunk-revision";

  /** File texts come from the daemon's revision/text. */
  constructor(private readonly daemon: Daemon) {}

  /**
   * The document of the file at `path` (repo-relative) as `label` (v1.0.0) names its revision. The daemon's opaque
   * ref rides in the query; nothing here parses it.
   */
  uriFor(ref: string, label: string, path: string): vscode.Uri {
    return vscode.Uri.from({ scheme: RevisionDocuments.scheme, path: revisionDocumentPath(label, path), query: ref });
  }

  /** The file's text for a URI made by uriFor(). */
  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const { text } = await this.daemon.request("revision/text", { ref: uri.query });
    return text;
  }
}
