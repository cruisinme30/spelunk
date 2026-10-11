// What the result menu copies and exports: a result as a path and line, or
// with its code; a commit's sha and subject; and every result as text,
// Markdown, CSV or JSON. No vscode import, so it's tested in plain Node.
import type { ResultItem } from "./protocol.gen";

/** A result in a file: a file name, a code line or a definition. */
type FileResult = Exclude<ResultItem, { kind: "commit" }>;
type CommitResult = Extract<ResultItem, { kind: "commit" }>;

/** The export formats, by file extension. */
export type ExportFormat = "txt" | "md" | "csv" | "json";

const shortSha = (sha: string) => sha.slice(0, 7);

/** ":46" for a code line or definition; "" for a file name. */
const lineSuffix = (item: FileResult) => (item.kind === "file" ? "" : `:${item.line}`);

/** The code a result shows: its line, trimmed, or a definition's name. */
function codeOf(item: FileResult): string {
  if (item.kind === "line") return item.text.trim();
  return item.kind === "symbol" ? item.name : "";
}

/** Copy: "src/payments/client.py:46: for attempt in range(3):", the form terminals and editors make clickable. */
export function copyText(item: FileResult): string {
  const code = codeOf(item);
  return code ? `${item.path}${lineSuffix(item)}: ${code}` : item.path;
}

/** Copy Path: the file's full path, then ":line". `root` is its workspace folder. */
export function absolutePath(item: FileResult, root: string, separator = "/"): string {
  const trimmed = root.endsWith(separator) ? root.slice(0, -1) : root;
  return `${trimmed}${separator}${item.path.split("/").join(separator)}${lineSuffix(item)}`;
}

/** Copy Relative Path: the path from the workspace folder, as the panel shows it, then ":line". */
export function relativePath(item: FileResult): string {
  return `${item.path}${lineSuffix(item)}`;
}

/** Copy SHA and Subject: "4421bd6 Retry charges with the shared retry_policy". */
export function shaAndSubject(item: CommitResult): string {
  return `${shortSha(item.sha)} ${item.subject}`;
}

/** What every export reads: the results, each repo's name, the query, and each result's permalink. */
export interface ExportInput {
  items: readonly ResultItem[];
  repoName: (repoId: string) => string;
  query: string;
  /** The link to open this search in Spelunk. */
  searchLink: string;
  /** A result's permalink, when it has one. */
  link: (item: ResultItem) => string | undefined;
}

/** One line per result, grep-style: "repo/path:line: code", "repo/path", or "repo sha date author: subject". */
export function asText({ items, repoName, query }: ExportInput): string {
  const lines = [`# Spelunk: ${query}`];
  for (const item of items) {
    const repo = repoName(item.repoId);
    if (item.kind === "commit") {
      lines.push(`${repo} ${shortSha(item.sha)} ${item.at.slice(0, 10)} ${item.author.name}: ${item.subject}`);
    } else {
      lines.push(`${repo}/${copyText(item)}`);
    }
  }
  return lines.join("\n") + "\n";
}

/** Backticks inside inline code: a longer run of them around it, as Markdown allows. */
function inlineCode(text: string): string {
  const longest = Math.max(0, ...[...text.matchAll(/`+/gu)].map((run) => run[0].length));
  const fence = "`".repeat(longest + 1);
  const pad = text.startsWith("`") || text.endsWith("`") ? " " : "";
  return `${fence}${pad}${text}${pad}${fence}`;
}

/** Brackets in link text, escaped. */
const linkText = (text: string) => text.replaceAll(/[[\]\\]/gu, (character) => `\\${character}`);

/** Text linked to `url`, or as it is without one. */
const linkedText = (text: string, url: string | undefined) => (url ? `[${linkText(text)}](${url})` : text);

/** A heading, a link to open the search, and each result linked to its permalink when it has one; code lines grouped by file. */
export function asMarkdown({ items, repoName, query, searchLink, link }: ExportInput): string {
  const lines = [`## Spelunk: ${inlineCode(query)}`, "", `[Open this search in Spelunk](${searchLink})`, ""];
  let file = "";
  for (const item of items) {
    const url = link(item);
    const repo = repoName(item.repoId);
    if (item.kind === "commit") {
      lines.push(
        `- ${url ? `[${inlineCode(shortSha(item.sha))}](${url})` : inlineCode(shortSha(item.sha))} ${item.subject} (${item.author.name})`,
      );
      file = "";
    } else if (item.kind === "file") {
      lines.push(`- ${linkedText(`${repo}/${item.path}`, url)}`);
      file = "";
    } else {
      const key = `${repo}/${item.path}`;
      if (key !== file) lines.push("", `**${key}**`, "");
      file = key;
      lines.push(`- ${linkedText(`L${item.line}`, url)}: ${inlineCode(codeOf(item))}`);
    }
  }
  return lines.join("\n") + "\n";
}

/** A CSV cell, quoted when it holds a comma, quote or line break. */
function csvCell(value: string | number): string {
  const text = String(value);
  return /[",\r\n]/u.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

/** A header row, then a row per result: kind, repo, path, line, text, sha, author, date and permalink. */
export function asCsv({ items, repoName, link }: ExportInput): string {
  const rows: (string | number)[][] = [["kind", "repo", "path", "line", "text", "sha", "author", "date", "url"]];
  for (const item of items) {
    const url = link(item) ?? "";
    const repo = repoName(item.repoId);
    if (item.kind === "commit")
      rows.push(["commit", repo, "", "", item.subject, item.sha, item.author.name, item.at, url]);
    else rows.push([item.kind, repo, item.path, item.kind === "file" ? "" : item.line, codeOf(item), "", "", "", url]);
  }
  return rows.map((row) => row.map((cell) => csvCell(cell)).join(",")).join("\n") + "\n";
}

/** The query and every result, with its permalink when it has one. */
export function asJson({ items, repoName, query, link }: ExportInput): string {
  const results = items.map((item) => {
    const url = link(item);
    const repo = repoName(item.repoId);
    const fields =
      item.kind === "commit"
        ? { sha: item.sha, subject: item.subject, author: item.author.name, date: item.at }
        : item.kind === "file"
          ? { path: item.path }
          : { path: item.path, line: item.line, text: item.kind === "line" ? item.text : item.name };
    return { kind: item.kind, repo, ...fields, ...(url ? { url } : {}) };
  });
  return JSON.stringify({ query, results }, undefined, 2) + "\n";
}

/** The export in `format`. */
export function exportResults(format: ExportFormat, input: ExportInput): string {
  switch (format) {
    case "txt": {
      return asText(input);
    }
    case "md": {
      return asMarkdown(input);
    }
    case "csv": {
      return asCsv(input);
    }
    case "json": {
      return asJson(input);
    }
  }
}

/** A file name for an export of `query`: "spelunk-retry_policy.md", unsafe characters dashed. */
export function exportFileName(query: string, format: ExportFormat): string {
  const slug = query
    .replaceAll(/[^\w.-]+/gu, "-")
    .replaceAll(/^-+|-+$/gu, "")
    .slice(0, 60);
  return `spelunk-${slug || "results"}.${format}`;
}
