// The recent list: newest first, one row per query, labeled with the question Ask wrote it for.
import assert from "node:assert/strict";
import { test } from "node:test";
import { openingState, withRecent } from "../savedState";

test("a query kept again moves to the top and keeps the question it was written for", () => {
  const recent = withRecent([{ query: "timeout" }], "kind:method ref:Retry", 20, "which methods use Retry");
  assert.deepEqual(recent, [
    { query: "kind:method ref:Retry", question: "which methods use Retry" },
    { query: "timeout" },
  ]);
  assert.deepEqual(withRecent(recent, " kind:method ref:Retry ", 1), [
    { query: "kind:method ref:Retry", question: "which methods use Retry" },
  ]);
  assert.equal(withRecent([], "  ", 20), undefined, "a blank box keeps nothing");
  assert.equal(withRecent([], "retry", 0), undefined, "a limit of 0 keeps nothing");
});

test("a window opens with 3.0's plain strings read as queries, under the query left in the box", () => {
  assert.deepEqual(openingState({ text: "b", recent: ["a", { query: "b", question: "where is b" }] }, 20), {
    text: "b",
    recent: [{ query: "b", question: "where is b" }, { query: "a" }],
  });
  assert.deepEqual(openingState({ text: "", recent: ["a"] }, 20).recent, [{ query: "a" }], "an empty box adds nothing");
  assert.deepEqual(openingState(undefined, 20), { text: "", recent: [] });
});
