// Workspace folders as protocol Roots, with stable ids derived from their paths.
import { createHash } from "node:crypto";
import { basename } from "node:path";
import type { Root } from "./protocol.gen";

/** A root's id: the first 12 hex digits of sha256(path), stable across sessions. */
export function rootId(path: string): string {
  return createHash("sha256").update(path).digest("hex").slice(0, 12);
}

export function makeRoot(path: string, name?: string): Root {
  return { id: rootId(path), path, name: name ?? basename(path) };
}
