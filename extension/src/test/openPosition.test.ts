// Where a result opens after a double-click in its preview.
import assert from "node:assert/strict";
import { test } from "node:test";
import { movedTo } from "../openPosition";

const position = { line: 7, column: 3, length: 5 };

test("a file opens at the word double-clicked, not across a match that spans lines", () => {
  const match = { path: "/work/a.py", line: 2, column: 1, endLine: 4, endColumn: 9 };
  assert.deepEqual(movedTo(match, position), { path: "/work/a.py", line: 7, column: 3, length: 5 });
  const atRevision = { path: "/work/a.py", line: 2, column: 1, length: 4, revision: "abc", revisionLabel: "v1" };
  assert.deepEqual(movedTo(atRevision, position), { ...atRevision, ...position }, "still at its revision");
});

test("a commit, or no position, opens as it resolved", () => {
  const commit = { repoId: "r1", sha: "abc" };
  assert.equal(movedTo(commit, position), commit);
  const file = { path: "/work/a.py", line: 2, column: 1, length: 4 };
  assert.equal(movedTo(file), file);
});
