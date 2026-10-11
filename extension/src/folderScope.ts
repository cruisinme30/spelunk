// The query Search in Folder writes for folders (and files) picked in the
// Explorer: each one anchored at its repo's root, several ORed together.
// Plain Node with no vscode import, so tests run it directly.
import { relative, sep } from "node:path";
import type { FolderNote, HostToWebview, Root } from "./protocol.gen";

/** A folder or file picked in the Explorer, by its absolute path. */
export interface PickedPath {
  path: string;
  isFolder: boolean;
}

/** A picked path in the root that holds it: `path` is slash-separated and relative to it, "" for the root itself. */
export interface ScopedPath {
  root: Root;
  path: string;
  isFolder: boolean;
}

/** RE2's metacharacters, escaped. */
function escapeRegex(text: string): string {
  return text.replaceAll(/[\\^$.|?*+()[\]{}]/g, String.raw`\$&`);
}

/**
 * `op:pattern`, or `op:/pattern/` when the pattern holds a space, a bracket
 * or a quote: a bare value ends at a space, and a bracket or a quote would
 * end it too or start a group.
 */
function operator(op: "f" | "repo", pattern: string): string {
  return /[\s()"]/.test(pattern) ? `${op}:/${pattern.replaceAll("/", String.raw`\/`)}/` : `${op}:${pattern}`;
}

/**
 * Each picked path in the innermost root that holds it (a root opened
 * inside another one holds its own files), with paths no root holds left
 * out, and each path once.
 */
export function scopedPaths(picked: PickedPath[], roots: Root[]): ScopedPath[] {
  const scoped: ScopedPath[] = [];
  for (const { path, isFolder } of picked) {
    const root = roots
      .filter((candidate) => path === candidate.path || path.startsWith(candidate.path + sep))
      .sort((a, b) => b.path.length - a.path.length)[0];
    if (!root) continue;
    const inRoot = relative(root.path, path).split(sep).join("/");
    if (!scoped.some((other) => other.root.id === root.id && other.path === inRoot)) {
      scoped.push({ root, path: inRoot, isFolder: isFolder || inRoot === "" });
    }
  }
  return scoped;
}

/** Whether `inner` is inside folder `outer` of the same root, or is it. */
function covers(outer: ScopedPath, inner: ScopedPath): boolean {
  if (outer.root.id !== inner.root.id || !outer.isFolder) return false;
  return outer.path === "" || inner.path === outer.path || inner.path.startsWith(`${outer.path}/`);
}

/**
 * `earlier` with `added` joined on: a path already covered, by itself or a
 * folder that holds it, adds nothing, and a folder added drops the paths
 * inside it.
 */
export function joinPaths(earlier: ScopedPath[], added: ScopedPath[]): ScopedPath[] {
  let joined = [...earlier];
  for (const path of added) {
    if (joined.some((other) => covers(other, path))) continue;
    joined = [...joined.filter((other) => !covers(path, other)), path];
  }
  return joined;
}

/** One term, or several ORed in parentheses. */
function group(terms: string[]): string {
  return terms.length === 1 ? (terms[0] ?? "") : `(${terms.join(" OR ")})`;
}

/**
 * The query for `paths`: a folder anchored at its repo's root
 * (`f:^src/api/`), a file to its whole path (`f:^package\.json$`), a root
 * as `repo:^name$`. With more than one root open, each path names its repo
 * too. Several paths of one repo are ORed under one `repo:`, and several
 * repos ORed in turn: AND binds tighter than OR.
 */
export function scopeQuery(paths: ScopedPath[], multiRoot: boolean): string {
  const byRoot = new Map<string, { root: Root; filters: string[]; whole: boolean }>();
  for (const { root, path, isFolder } of paths) {
    const entry = byRoot.get(root.id) ?? { root, filters: [], whole: false };
    if (path === "") entry.whole = true;
    else entry.filters.push(operator("f", `^${escapeRegex(path)}${isFolder ? "/" : "$"}`));
    byRoot.set(root.id, entry);
  }
  const parts = [...byRoot.values()].map(({ root, filters, whole }) => {
    const repo = operator("repo", `^${escapeRegex(root.name)}$`);
    if (whole) return repo;
    return multiRoot ? `${repo} ${group(filters)}` : group(filters);
  });
  return group(parts);
}

/**
 * The box's text with `scope` in front: it replaces `earlier`, the scope
 * Search in Folder last wrote, when the text still starts with it, and
 * the rest of the query stays. With nothing else typed, a space follows,
 * ready for the search.
 */
export function withScope(text: string, scope: string, earlier?: string): string {
  let rest = text.trim();
  if (earlier !== undefined && startsWithScope(rest, earlier)) rest = rest.slice(earlier.length).trim();
  return rest ? `${scope} ${rest}` : `${scope} `;
}

/** Whether `text` starts with `scope`, as a whole term. */
export function startsWithScope(text: string, scope: string): boolean {
  const trimmed = text.trimStart();
  return trimmed === scope || trimmed.startsWith(`${scope} `);
}

/** A path as a note names it: `src/api/`, or `web-checkout/src/api/` with more than one root open. */
export function pathLabel(path: ScopedPath, multiRoot: boolean): string {
  const shown = path.isFolder && path.path !== "" ? `${path.path}/` : path.path;
  if (!multiRoot) return shown || `${path.root.name}/`;
  return shown ? `${path.root.name}/${shown}` : `${path.root.name}/`;
}

/**
 * The scope Search in Folder last wrote, and what its folders can't give:
 * the notes that go under the results of searches that start with it.
 */
export class FolderScopeNotes {
  private scope: string | undefined;
  private notes: FolderNote[] = [];

  /** Search in Folder wrote `scope` at the front of the box; `notes` say what its folders can't give. */
  set(scope: string, notes: FolderNote[]): void {
    this.scope = scope;
    this.notes = notes;
  }

  /** The scope, while `text` (the box) still starts with it. */
  current(text: string): string | undefined {
    return this.scope !== undefined && startsWithScope(text, this.scope) ? this.scope : undefined;
  }

  /**
   * `payload` as the panel gets it: a search.done for `searched`, when it
   * starts with the scope, carries the notes. The panel shows a Git repo's
   * in a history search only.
   */
  added<T extends keyof HostToWebview>(type: T, payload: HostToWebview[T], searched?: string): HostToWebview[T] {
    if (type !== "search.done" || searched === undefined || this.notes.length === 0) return payload;
    return this.current(searched) === undefined ? payload : { ...payload, folderNotes: this.notes };
  }
}
