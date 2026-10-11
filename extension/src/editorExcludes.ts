// VS Code's files.exclude and search.exclude, for each workspace folder, as
// the daemon applies them: the searches leave out the files they hide.
import type { ExcludePattern, Root, RootExcludes } from "./protocol.gen";

/** Reads `<section>.exclude` as VS Code resolves it for the folder at `folder`: user, workspace and folder merged. */
export type FolderExcludes = (folder: string, section: "files" | "search") => unknown;

/**
 * The enabled patterns of files.exclude and search.exclude for each root, search.exclude winning where both
 * name a pattern, as in VS Code's search. A pattern set to false is off; one set to `{ "when": … }` keeps its
 * clause. Roots with none are left out.
 */
export function editorExcludes(roots: Root[], read: FolderExcludes): RootExcludes[] {
  return roots.flatMap((root) => {
    const merged = { ...entries(read(root.path, "files")), ...entries(read(root.path, "search")) };
    const patterns = Object.entries(merged).flatMap(([glob, value]) => pattern(glob, value));
    return patterns.length > 0 ? [{ rootId: root.id, patterns }] : [];
  });
}

/** An exclude setting's entries, or none when it isn't an object. */
function entries(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

/** The pattern an entry turns on: true, or a when clause; nothing for false or anything else. */
function pattern(glob: string, value: unknown): ExcludePattern[] {
  if (!glob.trim()) return [];
  if (value === true) return [{ glob }];
  if (typeof value === "object" && value !== null && typeof (value as { when?: unknown }).when === "string") {
    return [{ glob, when: (value as { when: string }).when }];
  }
  return [];
}
