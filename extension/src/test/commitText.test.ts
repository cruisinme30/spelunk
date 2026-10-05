// Commits as documents: the name in the editor tab and the git show text.
import assert from "node:assert/strict";
import { test } from "node:test";
import { commitDocumentName, renderCommit } from "../commitText";

const SHA = "0123456789abcdef0123456789abcdef01234567";

test("a commit document is named by its short sha and the start of its subject", () => {
  assert.equal(commitDocumentName(SHA), "0123456.diff");
  assert.equal(commitDocumentName(SHA, ""), "0123456.diff");
  assert.equal(commitDocumentName(SHA, "fix: a/b\\c\tthing\n"), "0123456 fix: a b c thing.diff");
});

test("a long subject is cut between characters, never inside an emoji", () => {
  // Cutting at 60 UTF-16 units would leave half a surrogate pair, which encodeURIComponent rejects.
  const subject = "a".repeat(59) + "😀 and more";
  const name = commitDocumentName(SHA, subject);
  assert.equal(name, `0123456 ${"a".repeat(59)}😀.diff`);
  assert.doesNotThrow(() => encodeURIComponent(name));
  for (const weird of ["%41 #frag ?q=1", "naïve 検索 🎉🎉", "\u0000\u001F"]) {
    const encoded = encodeURIComponent(commitDocumentName(SHA, weird));
    assert.equal(decodeURIComponent(encoded), commitDocumentName(SHA, weird), "the name survives the URI");
  }
});

test("a commit preview renders like git show", () => {
  const text = renderCommit({
    kind: "commit",
    sha: SHA,
    subject: "feat: retry",
    body: "Why it retries.\n",
    author: "Jane <jane@example.com>",
    at: "2026-01-02T03:04:05Z",
    files: [
      { path: "a.py", added: 1, removed: 1, hiddenByFilter: false },
      { path: "b.md", added: 2, removed: 0, hiddenByFilter: true },
    ],
    hunks: [
      {
        path: "a.py",
        header: "@@ -1 +1 @@",
        lines: [
          { kind: "del", text: "old()", hits: [] },
          { kind: "add", text: "new()", hits: [] },
        ],
      },
    ],
    subjectHits: [],
    bodyHits: [],
  });
  assert.equal(
    text,
    [
      `commit ${SHA}`,
      "Author: Jane <jane@example.com>",
      "Date:   2026-01-02T03:04:05Z",
      "",
      "    feat: retry",
      "    ",
      "    Why it retries.",
      "",
      "* a.py +1 -1",
      "  b.md +2 -0",
      "",
      "--- a/a.py",
      "+++ b/a.py",
      "@@ -1 +1 @@",
      "-old()",
      "+new()",
      "",
    ].join("\n"),
  );
});
