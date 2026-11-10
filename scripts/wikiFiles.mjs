// Only these static source files may cross the code-repository/wiki boundary.
import { lstatSync, readdirSync, readFileSync } from "node:fs";
import { join, posix } from "node:path";

/** Rejects traversal, hidden metadata and anything outside Markdown pages/PNG images. */
export function wikiPath(name) {
  if (typeof name !== "string" || name.includes("\\") || name.includes("\0")) return false;
  const parts = name.split("/");
  if (parts.some((part) => part === "" || part.startsWith("."))) return false;
  return (
    (parts.length === 1 && posix.extname(name) === ".md") ||
    (parts[0] === "images" && parts.length > 1 && posix.extname(name) === ".png")
  );
}

/** Lists a safe static tree without following symlinks; the mirror alone may contain Git metadata. */
export function wikiFiles(directory, mirror = false) {
  const files = [];
  const visit = (relative) => {
    const path = join(directory, relative);
    const stat = lstatSync(path);
    if (stat.isSymbolicLink()) throw new Error(`Wiki symlink is forbidden: ${path}`);
    if (stat.isDirectory()) {
      for (const entry of readdirSync(path).sort()) {
        if (mirror && relative === "" && entry === ".git") {
          if (!lstatSync(join(path, entry)).isDirectory()) throw new Error("Wiki .git must be a directory");
          continue;
        }
        const child = relative === "" ? entry : `${relative}/${entry}`;
        if (entry.startsWith(".") || entry.includes("\\")) throw new Error(`Unsafe wiki path: ${child}`);
        visit(child);
      }
    } else if (stat.isFile() && wikiPath(relative)) files.push(relative);
    else throw new Error(`Unsupported wiki file: ${path}`);
  };
  visit("");
  return files;
}

/** Reports content differences against an optional deployed clone, independent of Git state. */
export function wikiDifferences(source, mirror) {
  const sourceFiles = new Set(wikiFiles(source));
  const mirrorFiles = new Set(wikiFiles(mirror, true));
  const differences = [];
  for (const name of sourceFiles) {
    if (!mirrorFiles.delete(name) || !readFileSync(join(source, name)).equals(readFileSync(join(mirror, name)))) {
      differences.push(name);
    }
  }
  return [...differences, ...mirrorFiles].sort();
}

/** Source paths whose changes require a new validated publication run. Matches wiki.yml's path filter. */
export function wikiPublicationPath(name) {
  return (
    name.startsWith("docs/wiki/") ||
    /^scripts\/wiki[^/]*\.mjs$/.test(name) ||
    [
      "scripts/publishWiki.mjs",
      "scripts/checkWikiSync.mjs",
      "scripts/checkWikiDeployment.mjs",
      "scripts/mockTable.mjs",
      "scripts/specIds.mjs",
      ".github/workflows/wiki.yml",
    ].includes(name)
  );
}

/** Gets image paths from both the wiki's HTML screenshots and Markdown image links. */
export function wikiImagePaths(text) {
  return [...text.matchAll(/(?:src|href)="(images\/[^"]+)"|\]\((images\/[^)\s]+)\)/g)].map(
    ([, html, markdown]) => html ?? markdown,
  );
}
