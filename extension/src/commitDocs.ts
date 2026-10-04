// Read-only documents showing a commit as a unified diff, opened from
// commit results (result.open on a commit resolves to repoId + sha).
import * as vscode from "vscode";
import type { Daemon } from "./daemon";
import type { Preview } from "./protocol.gen";

export class CommitDocuments implements vscode.TextDocumentContentProvider {
  static readonly scheme = "unified-search-commit";
  constructor(private readonly daemon: Daemon) {}

  uriFor(repoId: string, sha: string, subject?: string): vscode.Uri {
    const title = `${sha.slice(0, 7)}${subject ? " " + subject.replace(/[\\/]/g, " ").slice(0, 60) : ""}.diff`;
    return vscode.Uri.parse(`${CommitDocuments.scheme}:/${encodeURIComponent(title)}`).with({ query: `${repoId}:${sha}` });
  }

  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const ref = `c:${uri.query}`;
    const p = await this.daemon.request("preview/get", { ref, contextLines: 3 });
    return renderCommit(p);
  }
}

export function renderCommit(p: Preview): string {
  if (p.kind !== "commit") return "";
  const out = [`commit ${p.sha}`, `Author: ${p.author}`, `Date:   ${p.at}`, "", ...`${p.subject}\n\n${p.body}`.trimEnd().split("\n").map((l) => "    " + l), ""];
  for (const f of p.files) out.push(`${f.hiddenByFilter ? " " : "*"} ${f.path} +${f.added} -${f.removed}`);
  out.push("");
  let last = "";
  for (const h of p.hunks) {
    if (h.path !== last) {
      out.push(`--- a/${h.path}`, `+++ b/${h.path}`);
      last = h.path;
    }
    out.push(h.header);
    for (const l of h.lines) out.push((l.kind === "add" ? "+" : l.kind === "del" ? "-" : " ") + l.text);
  }
  return out.join("\n") + "\n";
}
