// @covers msg:replace.apply setting:index.notebookOutputs
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { notebookOutputHash } from "../notebookOutputHash";
import type { ReplacePlan } from "../protocol.gen";
import { checkReplace, documentHash, replacementPlanKey } from "../replaceEdits";

/** Golden vectors shared with the daemon's notebook reader and output fingerprint. */
interface OutputVector {
  name: string;
  hash: string;
  editor: { code: boolean; outputs: { items: { mime: string; text: string }[] }[] }[];
}

const vectors = JSON.parse(
  readFileSync(join(__dirname, "../../testdata/notebook-output-hashes.json"), "utf8"),
) as OutputVector[];

test("live output fingerprints match the daemon golden vectors", () => {
  for (const vector of vectors) {
    const cells = vector.editor.map((cell) => ({
      code: cell.code,
      outputs: cell.outputs.map((output) => ({
        items: output.items.map((item) => ({ mime: item.mime, data: Buffer.from(item.text, "utf8") })),
      })),
    }));
    assert.equal(notebookOutputHash(cells), vector.hash, vector.name);
  }
});

test("an output-only change invalidates a replacement including the first output", async () => {
  const empty = vectors.find((vector) => vector.name === "no outputs")?.hash ?? assert.fail();
  const changed = vectors.find((vector) => vector.name === "first output")?.hash ?? assert.fail();
  const document = {
    lineCount: 1,
    lineText: () => "Retry",
    cells: [{ kind: "code", language: "python", lineCount: 1 }],
  };
  const plan: ReplacePlan = {
    matches: 1,
    truncated: false,
    files: [
      {
        repoId: "r",
        path: "a.ipynb",
        file: "/work/a.ipynb",
        sourceHash: documentHash(document),
        outputHash: empty,
        cells: document.cells,
        lines: [{ line: 1, text: "Retry", edits: [{ start: 0, end: 5, newText: "Backoff" }] }],
      },
    ],
  };
  for (const outputHash of [changed, undefined]) {
    const checked = await checkReplace(plan, () =>
      Promise.resolve({ ...document, ...(outputHash ? { outputHash } : {}) }),
    );
    assert.deepEqual(checked.skipped, ["a.ipynb"]);
    assert.equal(checked.edits.length, 0);
  }
  const unchanged = await checkReplace(plan, () => Promise.resolve({ ...document, outputHash: empty }));
  assert.equal(unchanged.edits.length, 1);
  const different = { ...plan, files: plan.files.map((file) => ({ ...file, outputHash: changed })) };
  assert.notEqual(replacementPlanKey(plan), replacementPlanKey(different));
  const sourceOnly = { ...plan, files: plan.files.map(({ outputHash: _outputHash, ...file }) => file) };
  const accepted = await checkReplace(sourceOnly, () => Promise.resolve({ ...document, outputHash: changed }));
  assert.equal(accepted.edits.length, 1, "outputs were not part of this indexed generation's predicate");
});
