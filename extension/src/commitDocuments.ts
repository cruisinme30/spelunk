// Read-only documents that show a commit as a unified diff. Commit results
// open here: result.open on a commit resolves to a sha, and the document's
// content comes from preview/get on the result's ref.
import * as vscode from "vscode";
import type { Daemon } from "./daemon";
import type { DiffLine, Preview } from "./protocol.gen";

const MAX_SUBJECT_IN_TITLE = 60;
const DIFF_CONTEXT_LINES = 3;
const DIFF_PREFIX: Record<DiffLine["kind"], string> = { add: "+", del: "-", ctx: " " };

export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

export class CommitDocuments implements vscode.TextDocumentContentProvider {
  static readonly scheme = "unified-search-commit";

  constructor(private readonly daemon: Daemon) {}

  /** The daemon's opaque ref rides in the URI query; nothing here parses it. */
  uriFor(ref: string, sha: string, subject?: string): vscode.Uri {
    const subjectPart = subject ? " " + subject.replace(/[\\/]/g, " ").slice(0, MAX_SUBJECT_IN_TITLE) : "";
    const title = `${shortSha(sha)}${subjectPart}.diff`;
    return vscode.Uri.parse(`${CommitDocuments.scheme}:/${encodeURIComponent(title)}`).with({ query: ref });
  }

  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const preview = await this.daemon.request("preview/get", { ref: uri.query, contextLines: DIFF_CONTEXT_LINES });
    return renderCommit(preview);
  }
}

/** Formats a commit preview like `git show`. */
export function renderCommit(preview: Preview): string {
  if (preview.kind !== "commit") return "";
  const message = `${preview.subject}\n\n${preview.body}`.trimEnd().split("\n");
  const lines = [
    `commit ${preview.sha}`,
    `Author: ${preview.author}`,
    `Date:   ${preview.at}`,
    "",
    ...message.map((line) => "    " + line),
    "",
  ];
  for (const file of preview.files) {
    const marker = file.hiddenByFilter ? " " : "*";
    lines.push(`${marker} ${file.path} +${file.added} -${file.removed}`);
  }
  lines.push("");
  let currentPath = "";
  for (const hunk of preview.hunks) {
    if (hunk.path !== currentPath) {
      lines.push(`--- a/${hunk.path}`, `+++ b/${hunk.path}`);
      currentPath = hunk.path;
    }
    lines.push(hunk.header);
    for (const line of hunk.lines) lines.push(DIFF_PREFIX[line.kind] + line.text);
  }
  return lines.join("\n") + "\n";
}
