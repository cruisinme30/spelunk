// Files at a revision as documents: the path that names them in the editor, and the diff's title.
import assert from "node:assert/strict";
import { test } from "node:test";
import { revisionDiffTitle, revisionDocumentPath } from "../revisionPath";

test("a file at a revision is named by the revision, then its path in the repo", () => {
  assert.equal(revisionDocumentPath("v1.0.0", "src/payments/client.py"), "/v1.0.0/src/payments/client.py");
  assert.equal(revisionDocumentPath("HEAD~10", "README.md"), "/HEAD~10/README.md");
  // A branch's slash would make the label two folders.
  assert.equal(revisionDocumentPath("release/2.x", "a.go"), "/release∕2.x/a.go");
});

test("the diff with the file now is titled by the file's name and the revision", () => {
  assert.equal(revisionDiffTitle("v1.0.0", "src/payments/client.py"), "client.py (v1.0.0) ↔ now");
  assert.equal(revisionDiffTitle("3f9c2e1", "README.md"), "README.md (3f9c2e1) ↔ now");
});
