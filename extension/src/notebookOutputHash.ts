// Mirrors the daemon's notebook output selection and line normalization,
// so a replace plan cannot reuse source matches after output eligibility changes.
import { createHash } from "node:crypto";

/** One live notebook cell, with MIME output items as supplied by the editor. */
export interface SearchableOutputCell {
  code: boolean;
  outputs: readonly {
    items: readonly { mime: string; data: Uint8Array }[];
  }[];
}

/**
 * Hashes the searchable outputs in the same form as notebook.Read and
 * notebook.OutputHash in the daemon. Each cell's printed chunks are joined,
 * one final LF removed before CRLF/CR normalization, then LF appended.
 * Blocks are framed as cell number, colon, UTF-8 byte length, colon, text.
 * No output still has a digest: adding the first one must invalidate a plan.
 */
export function notebookOutputHash(cells: readonly SearchableOutputCell[]): string {
  const digest = createHash("sha256");
  for (const [index, cell] of cells.entries()) {
    if (!cell.code) continue;
    const printed = cell.outputs.map((output) => outputText(output)).filter((text) => text !== "");
    if (printed.length === 0) continue;
    const joined = printed.join("");
    const body = joined.endsWith("\n") ? joined.slice(0, -1) : joined;
    const text = body.replaceAll("\r\n", "\n").replaceAll("\r", "\n") + "\n";
    digest.update(`${index + 1}:${Buffer.byteLength(text, "utf8")}:`);
    digest.update(text, "utf8");
  }
  return digest.digest("hex");
}

/** Streams supply their text; rich images hide captions, otherwise text/plain is searched. */
function outputText(output: SearchableOutputCell["outputs"][number]): string {
  const stream = output.items.find(
    (item) =>
      item.mime === "application/vnd.code.notebook.stdout" || item.mime === "application/vnd.code.notebook.stderr",
  );
  if (stream) return Buffer.from(stream.data).toString("utf8");
  if (output.items.some((item) => item.mime.startsWith("image/"))) return "";
  const plain = output.items.find((item) => item.mime === "text/plain");
  return plain ? Buffer.from(plain.data).toString("utf8") : "";
}
