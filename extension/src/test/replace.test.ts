// Replace all, the extension's one write path: what it applies, what it
// skips, and that it applies only what the preview showed. The round trip
// drives the real daemon binary; the Ui stands in for VS Code's documents.
// @covers msg:replace.preview msg:replace.plan msg:replace.apply msg:replace.done msg:replace.save
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, sep } from "node:path";
import { test } from "node:test";
import { SearchController, type STOP_WATCHING, type Ui } from "../controller";
import type { HostToWebview, ReplacePlan, UiSettings } from "../protocol.gen";
import {
  checkReplace,
  documentHash,
  type DocumentLines,
  fileIdentity,
  type LineEdit,
  OPEN_AT_ONCE,
} from "../replaceEdits";
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
  contextLines: 0,
  renderWhitespace: "selection",
  tabSize: 4,
  fontLigatures: '"liga" off, "calt" off',
  askEnabled: false,
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
      sourceHash: documentHash(documentOf("import x\nx = Retry()\n")),
      lines: [{ line: 2, text: "x = Retry()", edits: [{ start: 4, end: 9, newText: "Backoff" }] }],
    },
    {
      repoId: "r1",
      path: "b.py",
      file: "/work/b.py",
      sourceHash: documentHash(documentOf("Retry\n")),
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
    isCurrent: checked.isCurrent,
    pathsCurrent: checked.pathsCurrent,
  });
  const unreadable = await checkReplace(PLAN, () => Promise.resolve(undefined as DocumentLines | undefined));
  assert.deepEqual(unreadable.skipped, ["a.py", "b.py"]);
  const tooShort = await checkReplace(PLAN, () => Promise.resolve(documentOf("Retry\n")));
  assert.deepEqual(tooShort.skipped, ["a.py"], "a.py has no line 2 any more");
});

test("a file the plan lists twice, under nested workspace folders, is edited once", async () => {
  const twice: ReplacePlan = {
    matches: 3,
    truncated: false,
    files: [
      PLAN.files[0] ?? assert.fail(),
      { ...(PLAN.files[0] ?? assert.fail()), repoId: "inner", path: "a.py", file: "/work/./a.py" },
      PLAN.files[1] ?? assert.fail(),
    ],
  };
  const documents: Record<string, DocumentLines> = {
    "/work/a.py": documentOf("import x\nx = Retry()\n"),
    "/work/b.py": documentOf("Retry\n"),
  };
  const opened: string[] = [];
  const checked = await checkReplace(twice, (file) => {
    opened.push(file);
    return Promise.resolve(documents[file]);
  });
  assert.deepEqual(checked, {
    edits: [
      { file: "/work/a.py", line: 1, start: 4, end: 9, newText: "Backoff" },
      { file: "/work/b.py", line: 0, start: 0, end: 5, newText: "Backoff" },
    ],
    files: ["/work/a.py", "/work/b.py"],
    skipped: [],
    isCurrent: checked.isCurrent,
    pathsCurrent: checked.pathsCurrent,
  });
  assert.deepEqual(opened, ["/work/a.py", "/work/b.py"], "each file is read once");
});

test("a file whose plan entries would overlap is skipped whole", async () => {
  const [a] = PLAN.files;
  assert.ok(a);
  const clashing: ReplacePlan = {
    matches: 2,
    truncated: false,
    files: [a, { ...a, lines: [{ line: 2, text: "x = Retry()", edits: [{ start: 6, end: 9, newText: "X" }] }] }],
  };
  const checked = await checkReplace(clashing, () => Promise.resolve(documentOf("import x\nx = Retry()\n")));
  assert.deepEqual(checked, {
    edits: [],
    files: [],
    skipped: ["a.py"],
    isCurrent: checked.isCurrent,
    pathsCurrent: checked.pathsCurrent,
  });
});

test("a file's identity is its absolute path as a URI spells it", () => {
  assert.equal(fileIdentity("/work/sub/../a.py"), "/work/a.py");
  assert.equal(fileIdentity("a.py"), fileIdentity(join(process.cwd(), "a.py")), "relative paths resolve");
});

/** A Ui whose documents are the files on disk and whose edits are written back to them. */
function diskUi() {
  const posted: { type: keyof HostToWebview; payload: unknown }[] = [];
  const applied: { edits: LineEdit[]; label: string }[] = [];
  const saved: string[][] = [];
  const ui: Ui = {
    post: (type, payload) => void posted.push({ type, payload }),
    openTarget: () => Promise.resolve(true),
    hidePanel: () => {},
    openHelp: () => {},
    openSettings: () => {},
    restartDaemon: () => {},
    setContext: () => {},
    saveState: () => {},
    showOpened: () => {},
    savePinned: () => Promise.resolve(),
    saveContextLines: () => {},
    saveOrder: () => {},
    readDocument: (file) => Promise.resolve(documentOf(readFileSync(file, "utf8"))),
    applyEdits: (edits, label, isCurrent) => {
      if (!isCurrent()) return Promise.resolve(false);
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
    showWatchMatch: () => Promise.resolve(undefined as typeof STOP_WATCHING | undefined),
    showQuery: () => {},
    showError: () => {},
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
      previewContextLines: () => 7,
      uiSettings: () => UI_SETTINGS,
      openFiles: () => [],
      pinnedQueries: () => [],
      watchNotify: () => true,
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

test("a replace reads its files several at a time, in the plan's order", async () => {
  const files = Array.from({ length: 40 }, (_, index) => ({
    repoId: "r1",
    path: `f${index}.py`,
    file: `/work/f${index}.py`,
    sourceHash: documentHash(documentOf("Retry")),
    lines: [{ line: 1, text: "Retry", edits: [{ start: 0, end: 5, newText: "Backoff" }] }],
  }));
  let reading = 0;
  let most = 0;
  const checked = await checkReplace({ matches: files.length, truncated: false, files }, async (file) => {
    most = Math.max(most, ++reading);
    // The later files are read faster, so finishing order isn't the plan's.
    await new Promise((resolve) => setTimeout(resolve, 50 - Number(/\d+/.exec(file)?.[0])));
    reading--;
    return documentOf("Retry");
  });
  assert.equal(most, OPEN_AT_ONCE, "reads overlap, up to OPEN_AT_ONCE");
  assert.deepEqual(
    checked.files,
    files.map((file) => file.file),
  );
});

test("a change outside edited lines invalidates the entire file", async () => {
  const original = "// header\nretry()\n";
  const file = {
    repoId: "r1",
    path: "a.js",
    file: "/work/a.js",
    sourceHash: documentHash(documentOf(original)),
    lines: [{ line: 2, text: "retry()", edits: [{ start: 0, end: 5, newText: "next" }] }],
  };
  // The first edit changes the target's lexical scope without touching its line.
  for (const changed of ["/* header\nretry()\n", "// header\nretry()\nexcludedTerm\n", "// header\nretry()"]) {
    const checked = await checkReplace({ matches: 1, truncated: false, files: [file] }, () =>
      Promise.resolve(documentOf(changed)),
    );
    assert.equal(checked.edits.length, 0, changed);
    assert.deepEqual(checked.skipped, ["a.js"]);
  }
});

test("a replacement rechecks document versions after asynchronous edit assembly", async () => {
  let version = 1;
  const document = { ...documentOf("Retry\n"), isCurrent: () => version === 1 };
  const checked = await checkReplace({ ...PLAN, files: [PLAN.files[1] ?? assert.fail()] }, () =>
    Promise.resolve(document),
  );
  assert.equal(checked.edits.length, 1);
  assert.equal(checked.isCurrent(), true);
  version++;
  assert.equal(checked.isCurrent(), false, "no edit may be submitted using the earlier snapshot");
});

test("replacement source hashes agree with daemon vectors, including empty and trailing lines", () => {
  const vectors = [
    ["", "01ba4719c80b6fe911b091a7c05124b64eeece964e09c058ef8f9805daca546b"],
    ["a", "87428fc522803d31065e7bce3cf03fe475096631e5e07bbd7a0fde60c4cf25c7"],
    ["a\n", "a7da489976d0047490617adb4f7a1f27f7af8b52a5176fd002ffe471863520ab"],
  ];
  for (const [source, hash] of vectors) assert.equal(documentHash(documentOf(source ?? "")), hash);
  assert.equal(documentHash(documentOf("😀retry\r\nend\r\n")), documentHash(documentOf("😀retry\nend\n")));
});

test("notebook replacements require the same source cells, kinds and languages", async () => {
  const text = documentOf("header\nretry()");
  const cells = [{ kind: "code", language: "python", lineCount: 2 }];
  const file = {
    repoId: "r1",
    path: "a.ipynb",
    file: "/work/a.ipynb",
    sourceHash: documentHash(text),
    cells,
    lines: [{ line: 2, text: "retry()", edits: [{ start: 0, end: 5, newText: "next" }] }],
  };
  const plan = { matches: 1, truncated: false, files: [file] };
  for (const changed of [
    [
      { kind: "code", language: "python", lineCount: 1 },
      { kind: "code", language: "python", lineCount: 1 },
    ],
    [{ kind: "markdown", language: "markdown", lineCount: 2 }],
    [{ kind: "code", language: "javascript", lineCount: 2 }],
  ]) {
    const checked = await checkReplace(plan, () => Promise.resolve({ ...text, cells: changed }));
    assert.equal(checked.edits.length, 0);
    assert.deepEqual(checked.skipped, ["a.ipynb"]);
  }
  const current = await checkReplace(plan, () => Promise.resolve({ ...text, cells }));
  assert.equal(current.edits.length, 1);
  const cpp = { ...file, cells: [{ kind: "code", language: "c++", lineCount: 2 }] };
  const renamedLanguage = await checkReplace({ ...plan, files: [cpp] }, () =>
    Promise.resolve({ ...text, cells: [{ kind: "code", language: "cpp", lineCount: 2 }] }),
  );
  assert.equal(renamedLanguage.edits.length, 1, "canonical daemon names match editor language IDs");
});

test("malformed or surrogate-splitting edit ranges skip the file instead of being clamped", async () => {
  const text = "😀Retry";
  const file = { repoId: "r1", path: "a.py", file: "/work/a.py", sourceHash: documentHash(documentOf(text)) };
  for (const [start, end] of [
    [-1, 2],
    [0, 99],
    [2, 2],
    [2, 1],
    [0.5, 2],
    [0, 1],
    [1, 2],
  ]) {
    const plan = {
      matches: 1,
      truncated: false,
      files: [{ ...file, lines: [{ line: 1, text, edits: [{ start: start ?? 0, end: end ?? 0, newText: "next" }] }] }],
    };
    const checked = await checkReplace(plan, () => Promise.resolve(documentOf(text)));
    assert.equal(checked.edits.length, 0, `${start}:${end}`);
    assert.deepEqual(checked.skipped, ["a.py"]);
  }
});

test("distinct POSIX backslash and directory paths are replaced separately", { skip: sep === "\\" }, async () => {
  const paths = [String.raw`a\b.py`, "a/b.py"];
  const files = paths.map((path) => ({
    repoId: "r1",
    path,
    file: `/work/${path}`,
    sourceHash: documentHash(documentOf("Retry")),
    lines: [{ line: 1, text: "Retry", edits: [{ start: 0, end: 5, newText: "next" }] }],
  }));
  assert.notEqual(fileIdentity(files[0]?.file ?? ""), fileIdentity(files[1]?.file ?? ""));
  const checked = await checkReplace({ matches: 2, files, truncated: false }, () =>
    Promise.resolve(documentOf("Retry")),
  );
  assert.deepEqual(
    checked.files,
    files.map((file) => file.file),
  );
  assert.equal(checked.edits.length, 2);
});
