// A replacement must still name a regular file under the selected workspace
// root when its editor document is opened, edited, and later saved.
import { lstatSync, realpathSync } from "node:fs";
import { isAbsolute, join, relative, resolve } from "node:path";
import type { ReplaceFile, Root } from "./protocol.gen";

/** The immutable location a replacement plan names. */
interface ReplacementLocation {
  repoId: string;
  path: string;
  file: string;
}

/** The identity of one directory or regular file, independent of its contents. */
interface PathIdentity {
  device: bigint;
  inode: bigint;
}

/** A selected root and the identities from its resolved directory to the file. */
interface PathSnapshot {
  root: string;
  realRoot: string;
  identities: PathIdentity[];
}

/**
 * Validates the plan's location against the currently selected workspace
 * roots, then returns a synchronous recheck for use immediately before an
 * editor edit or save. The root itself may be a symlink; none of its
 * descendant components may be one. Replacing any directory or the file
 * invalidates the guard, even if the new file has identical contents.
 *
 * This checks the filesystem at each call. VS Code's later edit/save is not
 * an atomic filesystem operation with this check; its API offers no such lock.
 */
export function validateReplacementPath(file: ReplaceFile, roots: () => readonly Root[]): (() => boolean) | undefined {
  const location = { repoId: file.repoId, path: file.path, file: file.file };
  const original = pathSnapshot(location, roots);
  if (!original) return undefined;
  return () => {
    const current = pathSnapshot(location, roots);
    return (
      current !== undefined &&
      samePath(current.root, original.root) &&
      samePath(current.realRoot, original.realRoot) &&
      current.identities.length === original.identities.length &&
      current.identities.every((identity, index) => {
        const expected = original.identities[index];
        return identity.device === expected?.device && identity.inode === expected.inode;
      })
    );
  };
}

/** Reads only a validated descendant, failing closed on missing or changed paths. */
function pathSnapshot(file: ReplacementLocation, roots: () => readonly Root[]): PathSnapshot | undefined {
  try {
    const root = roots().find((candidate) => candidate.id === file.repoId);
    if (!root || !isAbsolute(root.path) || !isAbsolute(file.file) || !localPath(file.path)) return undefined;
    const parts = file.path.split("/");
    const rootPath = resolve(root.path);
    const expected = resolve(rootPath, ...parts);
    if (!samePath(expected, resolve(file.file))) return undefined;
    const realRoot = realpathSync.native(rootPath);
    const identities: PathIdentity[] = [];
    let current = realRoot;
    for (const [index, part] of ["", ...parts].entries()) {
      current = join(current, part);
      const info = lstatSync(current, { bigint: true });
      const correctType = index === parts.length ? info.isFile() : info.isDirectory();
      if (info.isSymbolicLink() || !correctType) return undefined;
      identities.push({ device: info.dev, inode: info.ino });
    }
    // A directory may have moved while its descendants were inspected.
    if (!samePath(realpathSync.native(rootPath), realRoot) || !samePath(realpathSync.native(expected), current)) {
      return undefined;
    }
    return { root: rootPath, realRoot, identities };
  } catch {
    return undefined;
  }
}

/** Canonical slash-relative paths; Windows also rejects drives and alternate streams. */
function localPath(path: string): boolean {
  if (path.includes("\0") || (process.platform === "win32" && /[\\:]/.test(path))) return false;
  return path.split("/").every((part) => part !== "" && part !== "." && part !== "..");
}

/** Uses the host's path rules, including drive-letter case on Windows. */
function samePath(first: string, second: string): boolean {
  return relative(first, second) === "";
}
