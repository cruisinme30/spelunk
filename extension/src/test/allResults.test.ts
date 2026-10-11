// Every result of a query for an export: its own searches, page after page.
import assert from "node:assert/strict";
import { test } from "node:test";
import { AllResults } from "../allResults";
import type { ResultItem, SearchBatchParams, SearchStartParams } from "../protocol.gen";

/** A code line result numbered n. */
const result = (n: number): ResultItem => ({
  kind: "line",
  ref: `l${n}`,
  repoId: "r1",
  path: "a.py",
  line: n,
  text: "retry()",
  hits: [],
});

test("an export runs the query again page by page, keeping only its own batches, up to the limit", async () => {
  const pages = [[result(1), result(2)], [result(3)]];
  const listeners: ((batch: SearchBatchParams) => void)[] = [];
  const onBatch = (batch: SearchBatchParams) => {
    for (const listener of listeners) listener(batch);
  };
  const asked: SearchStartParams[] = [];
  const all = new AllResults({
    on: (_event: string, listener: (batch: SearchBatchParams) => void) => {
      listeners.push(listener);
    },
    request: (_method: string, params: SearchStartParams) => {
      asked.push(params);
      const page = params.cursor === undefined ? 0 : 1;
      // The panel's own search streams too; its batch isn't the export's.
      onBatch({ searchId: "s9", items: [result(99)] });
      onBatch({ searchId: params.searchId, items: pages[page] ?? [] });
      return Promise.resolve({
        total: 3,
        truncated: false,
        hidden: [],
        ms: 1,
        ...(page === 0 ? { nextCursor: "p2" } : {}),
      });
    },
  } as never);
  const found = await all.of("retry", ["/work/a.py"], 10_000);
  assert.deepEqual(
    found.map((item) => item.ref),
    ["l1", "l2", "l3"],
  );
  assert.deepEqual(asked, [
    { searchId: "all1", text: "retry", openFiles: ["/work/a.py"] },
    { searchId: "all1", text: "retry", openFiles: ["/work/a.py"], cursor: "p2" },
  ]);
  const first = await all.of("retry", [], 1);
  assert.deepEqual(
    first.map((item) => item.ref),
    ["l1"],
  );
  assert.equal(asked.at(-1)?.searchId, "all2", "it stops after the page that reached the limit");
});
