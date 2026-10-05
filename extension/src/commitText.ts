// How a commit reads as a document: the file name in its tab and its text,
// formatted like `git show`. Plain functions with no vscode import, so they
// are tested in Node; commitDocuments.ts serves them to VS Code.
import type { DiffLine, Preview } from "./protocol.gen";

/** Characters (grapheme clusters, not UTF-16 units) of the commit subject kept in the document's name. */
const MAX_SUBJECT_IN_TITLE = 60;
const DIFF_PREFIX: Record<DiffLine["kind"], string> = { add: "+", del: "-", ctx: " " };

/** The first seven characters of a commit sha, as Git shows them. */
function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

/**
 * The document's file name: the short sha and the start of the subject.
 * Path separators and control characters become spaces, and the subject is
 * cut between characters, so an emoji is never split into half a surrogate
 * pair (which encodeURIComponent refuses).
 */
export function commitDocumentName(sha: string, subject?: string): string {
  const cleaned = subject?.replaceAll(/[\\/\p{Cc}]/gu, " ").trim();
  if (!cleaned) return `${shortSha(sha)}.diff`;
  const characters = Array.from(new Intl.Segmenter().segment(cleaned), ({ segment }) => segment);
  const subjectPart = " " + characters.slice(0, MAX_SUBJECT_IN_TITLE).join("");
  return `${shortSha(sha)}${subjectPart}.diff`;
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
