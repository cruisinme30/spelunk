// How a file at a revision is named in the editor. A plain function with no
// vscode import, so it is tested in Node; revisionDocuments.ts uses it.

/**
 * The path of a file's document at a revision: the revision's label as its first folder, then the file's
 * repo-relative path, so the tab reads "client.py" with the language its extension gives, and the breadcrumbs
 * "v1.0.0 › src › client.py". A "/" in the label (release/2.x) becomes "∕", so it stays one folder.
 */
export function revisionDocumentPath(label: string, path: string): string {
  return `/${label.replaceAll("/", "∕")}/${path}`;
}

/** The title of the diff that compares a file at a revision with the file now: "client.py (v1.0.0) ↔ now". */
export function revisionDiffTitle(label: string, path: string): string {
  return `${path.slice(path.lastIndexOf("/") + 1)} (${label}) ↔ now`;
}
