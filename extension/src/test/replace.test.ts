// Replace all, the extension's one write path: what it applies, what it
// skips, and that it applies only what the preview showed. The round trip
// drives the real daemon binary; the Ui stands in for VS Code's documents.
// @covers msg:replace.preview msg:replace.plan msg:replace.apply msg:replace.done msg:replace.save
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { SearchController, type Ui } from "../controller";
import type { HostToWebview, ReplacePlan, UiSettings } from "../protocol.gen";
import { checkReplace, type DocumentLines, type LineEdit } from "../replaceEdits";
import { makeRoot } from "../roots";
import { MESSAGE_VERSION } from "../webviewMessages";
import { newTestDaemon, SKIP_WITHOUT_DAEMON as skip, untilIndexed } from "./realDaemon";

const UI_SETTINGS: UiSettings = {
  typingDelayMs: 0,
  openTrigger: "doubleClick",
  preview: true,
  showParsedQuery: true,
  caseSensitive: "off",
  wholeWord: false,
  order: "best",
};

/** A document holding `text`. */
function documentOf(text: string): DocumentLines {
  const lines = text.split(/\r?\n/);
  return { lineCount: lines.length, lineText: (index) => lines[index] ?? "" };
}

/** A plan replacing "Retry" with "Backoff" on line 2 of a.py and line 1 of b.py. */
const PLAN: ReplacePlan = {
  matches: 2,
  truncated: false,
  files: [
    {
      repoId: "r1",
      path: "a.py",
      file: "/work/a.py",
      lines: [{ line: 2, text: "x = Retry()", edits: [{ start: 4, end: 9, newText: "Backoff" }] }],
    },
    {
      repoId: "r1",
      path: "b.py",
      file: "/work/b.py",
      lines: [{ line: 1, text: "Retry", edits: [{ start: 0, end: 5, newText: "Backoff" }] }],
    },
  ],
};

test("a replace keeps the files that still read as indexed and skips the rest whole", async () => {
  const documents: Record<string, DocumentLines> = {
    "/work/a.py": documentOf("import x\nx = Retry()\n"),
    "/work/b.py": documentOf("Retry  # edited since\n"),
  };
  const checked = await checkReplace(PLAN, (file) => Promise.resolve(documents[file]));
  assert.deepEqual(checked, {
    edits: [{ file: "/work/a.py", line: 1, start: 4, end: 9, newText: "Backoff" }],
    files: ["/work/a.py"],
    skipped: ["b.py"],
  });
  const unreadable = await checkReplace(PLAN, () => Promise.resolve(undefined as DocumentLines | undefined));
  assert.deepEqual(unreadable.skipped, ["a.py", "b.py"]);
  const tooShort = await checkReplace(PLAN, () => Promise.resolve(documentOf("Retry")));
  assert.deepEqual(tooShort.skipped, ["a.py"], "a.py has no line 2 any more");
});

/** A Ui whose documents are the files on disk and whose edits are written back to them. */
function diskUi() {
  const posted: { type: keyof HostToWebview; payload: unknown }[] = [];
  const applied: { edits: LineEdit[]; label: string }[] = [];
  const saved: string[][] = [];
  const ui: Ui = {
    post: (type, payload) => void posted.push({ type, payload }),
    openTarget: () => Promise.resolve(),
    hidePanel: () => {},
    openHelp: () => {},
    openSettings: () => {},
    restartDaemon: () => {},
    setContext: () => {},
    saveState: () => {},
    showOpened: () => {},
    savePinned: () => {},
    readDocument: (file) => Promise.resolve(documentOf(readFileSync(file, "utf8"))),
    applyEdits: (edits, label) => {
      applied.push({ edits, label });
      for (const file of new Set(edits.map((edit) => edit.file))) {
        const lines = readFileSync(file, "utf8").split("\n");
        // Right to left, so earlier ranges on a line stay put.
        const inFile = edits.filter((each) => each.file === file).sort((a, b) => b.line - a.line || b.start - a.start);
        for (const edit of inFile) {
          const text = lines[edit.line] ?? "";
          lines[edit.line] = text.slice(0, edit.start) + edit.newText + text.slice(edit.end);
        }
        writeFileSync(file, lines.join("\n"));
      }
      return Promise.resolve(true);
    },
    saveFiles: (files) => void saved.push(files),
  };
  const payloads = <T extends keyof HostToWebview>(type: T) =>
    posted.filter((message) => message.type === type).map((message) => message.payload as HostToWebview[T]);
  return { ui, payloads, applied, saved };
}

test("Replace all applies what the preview showed through the real daemon", { skip }, async () => {
  const workspace = mkdtempSync(join(tmpdir(), "us-replace-"));
  writeFileSync(join(workspace, "policy.py"), "class RetryPolicy:\n    pass\n\nRetryPolicy() or RetryPolicy()\n");
  writeFileSync(join(workspace, "client.py"), "from policy import RetryPolicy\n");
  const daemon = newTestDaemon([makeRoot(workspace, "payments-api")]);
  await daemon.start();
  try {
    await untilIndexed(daemon);
    const host = diskUi();
    const controller = new SearchController(daemon, host.ui, {
      recentLimit: () => 20,
      closeOnOpen: () => true,
      uiSettings: () => UI_SETTINGS,
      openFiles: () => [],
      pinnedQueries: () => [],
    });
    const message = { text: "RetryPolicy", replacement: "BackoffPolicy" };

    await controller.handle({ v: MESSAGE_VERSION, type: "replace.preview", payload: { seq: 1, ...message } });
    const [preview] = host.payloads("replace.plan");
    assert.equal(preview?.seq, 1);
    assert.equal(preview.plan?.matches, 4);
    assert.equal(preview.plan.files.length, 2);

    // A count that differs from the plan's applies nothing and sends the new preview.
    await controller.handle({ v: MESSAGE_VERSION, type: "replace.apply", payload: { seq: 2, ...message, matches: 3 } });
    assert.deepEqual(host.payloads("replace.done").at(-1), {
      seq: 2,
      replaced: 0,
      files: 0,
      skipped: [],
      changed: true,
    });
    assert.equal(host.payloads("replace.plan").at(-1)?.plan?.matches, 4);
    assert.equal(host.applied.length, 0);

    await controller.handle({ v: MESSAGE_VERSION, type: "replace.apply", payload: { seq: 3, ...message, matches: 4 } });
    assert.deepEqual(host.payloads("replace.done").at(-1), { seq: 3, replaced: 4, files: 2, skipped: [] });
    assert.equal(host.applied.length, 1, "one edit, so one undo");
    assert.equal(host.applied[0]?.label, "Replace 4 matches");
    assert.equal(
      readFileSync(join(workspace, "policy.py"), "utf8"),
      "class BackoffPolicy:\n    pass\n\nBackoffPolicy() or BackoffPolicy()\n",
    );
    assert.equal(readFileSync(join(workspace, "client.py"), "utf8"), "from policy import BackoffPolicy\n");

    await controller.handle({ v: MESSAGE_VERSION, type: "replace.save", payload: {} });
    assert.deepEqual(host.saved.at(-1)?.sort(), [join(workspace, "client.py"), join(workspace, "policy.py")]);

    // Again replaces nothing: until the daemon re-reads the files its plan no longer fits them,
    // so both are skipped, and once it has, the matches differ from the preview.
    await controller.handle({ v: MESSAGE_VERSION, type: "replace.apply", payload: { seq: 4, ...message, matches: 4 } });
    const again = host.payloads("replace.done").at(-1);
    assert.equal(again?.replaced, 0);
    assert.ok(again.changed === true || again.skipped.length === 2, JSON.stringify(again));

    await controller.handle({
      v: MESSAGE_VERSION,
      type: "replace.preview",
      payload: { seq: 5, text: "a OR b", replacement: "c" },
    });
    assert.match(host.payloads("replace.plan").at(-1)?.error ?? "", /exactly one search term/);
  } finally {
    await daemon.stop();
  }
});
