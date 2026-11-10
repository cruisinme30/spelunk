// @covers msg:replace.apply msg:replace.save
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, renameSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import type { ReplaceFile, Root } from "../protocol.gen";
import { validateReplacementPath } from "../replacementPaths";
import { makeRoot } from "../roots";

/** A regular file and an identical external file, with cleanup after the test. */
function fixture(context: TestContext) {
  const directory = mkdtempSync(join(tmpdir(), "spelunk-replacement-path-"));
  context.after(() => {
    rmSync(directory, { recursive: true, force: true });
  });
  const root = makeRoot(join(directory, "workspace"));
  const outside = join(directory, "outside");
  mkdirSync(join(root.path, "src"), { recursive: true });
  mkdirSync(outside);
  writeFileSync(join(root.path, "src", "a.py"), "Retry\n");
  writeFileSync(join(outside, "a.py"), "Retry\n");
  const file: ReplaceFile = {
    repoId: root.id,
    path: "src/a.py",
    file: join(root.path, "src", "a.py"),
    sourceHash: "the path guard does not authenticate contents",
    lines: [],
  };
  return { directory, root, outside, file };
}

test("a regular replacement file stays valid while its selected root and identity stay unchanged", (context) => {
  const { root, file } = fixture(context);
  const current = validateReplacementPath(file, () => [root]);
  assert.ok(current);
  assert.equal(current(), true);
  writeFileSync(file.file, "Backoff\n");
  assert.equal(current(), true, "source changes are checked separately; our own replacement changes the text");
});

test("replacement locations reject traversal, absolute paths, streams and mismatched absolute files", (context) => {
  const { root, outside, file } = fixture(context);
  for (const path of ["", ".", "..", "../outside/a.py", "/src/a.py", "src/../a.py", "src//a.py", "src/\0a.py"]) {
    assert.equal(
      validateReplacementPath({ ...file, path }, () => [root]),
      undefined,
      path,
    );
  }
  assert.equal(
    validateReplacementPath({ ...file, file: join(outside, "a.py") }, () => [root]),
    undefined,
  );
  assert.equal(
    validateReplacementPath({ ...file, file: "src/a.py" }, () => [root]),
    undefined,
  );
  if (process.platform === "win32") {
    for (const path of [String.raw`src\a.py`, "C:/a.py", "src/a.py:stream"]) {
      assert.equal(
        validateReplacementPath({ ...file, path }, () => [root]),
        undefined,
        path,
      );
    }
  }
});

test("ordinary host filenames retain spaces, Unicode and POSIX punctuation", (context) => {
  const { root, file } = fixture(context);
  const names = ["space name.py", "caf\u00E9.py"];
  if (process.platform !== "win32") names.push("colon:name.py", String.raw`back\slash.py`);
  for (const name of names) {
    const target = { ...file, path: `src/${name}`, file: join(root.path, "src", name) };
    writeFileSync(target.file, "Retry\n");
    const current = validateReplacementPath(target, () => [root]);
    assert.ok(current, name);
    assert.equal(current(), true, name);
  }
});

test("a replacement must belong to its own currently selected root", (context) => {
  const { root, outside, file } = fixture(context);
  const other = makeRoot(outside);
  assert.equal(
    validateReplacementPath({ ...file, repoId: other.id }, () => [root, other]),
    undefined,
  );
  assert.equal(
    validateReplacementPath(file, () => [other]),
    undefined,
  );
  let selected: Root[] = [root];
  const current = validateReplacementPath(file, () => selected);
  assert.ok(current);
  selected = [];
  assert.equal(current(), false);
  selected = [{ ...root, path: outside }];
  assert.equal(current(), false);
});

test("missing paths and directories cannot be replacement files", (context) => {
  const { root, file } = fixture(context);
  rmSync(file.file);
  assert.equal(
    validateReplacementPath(file, () => [root]),
    undefined,
  );
  mkdirSync(file.file);
  assert.equal(
    validateReplacementPath(file, () => [root]),
    undefined,
  );
});

test(
  "a same-content leaf symlink is rejected before opening and after validation",
  { skip: process.platform === "win32" },
  (context) => {
    const { root, outside, file } = fixture(context);
    const current = validateReplacementPath(file, () => [root]);
    assert.ok(current);
    renameSync(file.file, `${file.file}.original`);
    symlinkSync(join(outside, "a.py"), file.file);
    assert.equal(
      validateReplacementPath(file, () => [root]),
      undefined,
    );
    assert.equal(current(), false);
  },
);

test(
  "a notebook path uses the same leaf protection as a text file",
  { skip: process.platform === "win32" },
  (context) => {
    const { root, outside, file } = fixture(context);
    const notebook = { ...file, path: "book.ipynb", file: join(root.path, "book.ipynb") };
    writeFileSync(notebook.file, "{}");
    const current = validateReplacementPath(notebook, () => [root]);
    assert.ok(current);
    renameSync(notebook.file, `${notebook.file}.original`);
    symlinkSync(join(outside, "a.py"), notebook.file);
    assert.equal(current(), false);
    assert.equal(
      validateReplacementPath(notebook, () => [root]),
      undefined,
    );
  },
);

test("a symlink or junction ancestor is rejected even when its target stays inside the root", (context) => {
  const { root, outside, file } = fixture(context);
  const current = validateReplacementPath(file, () => [root]);
  assert.ok(current);
  const source = join(root.path, "src");
  const original = join(root.path, "original");
  renameSync(source, original);
  for (const target of [outside, original]) {
    symlinkSync(target, source, "junction");
    assert.equal(
      validateReplacementPath(file, () => [root]),
      undefined,
    );
    assert.equal(current(), false);
    rmSync(source);
  }
});

test("replacing a file or directory with an identical regular copy invalidates its guard", (context) => {
  const { root, file } = fixture(context);
  const current = validateReplacementPath(file, () => [root]);
  assert.ok(current);
  renameSync(file.file, `${file.file}.original`);
  writeFileSync(file.file, "Retry\n");
  assert.equal(current(), false);
  const copied = validateReplacementPath(file, () => [root]);
  assert.ok(copied);
  renameSync(join(root.path, "src"), join(root.path, "original"));
  mkdirSync(join(root.path, "src"));
  renameSync(join(root.path, "original", "a.py"), file.file);
  assert.equal(copied(), false, "the leaf inode is unchanged but its parent was replaced");
});

test("a stable symlink workspace root is allowed but retargeting it invalidates the guard", (context) => {
  const { directory, root, outside, file } = fixture(context);
  const alias = makeRoot(join(directory, "selected"));
  symlinkSync(root.path, alias.path, "junction");
  const viaAlias = { ...file, repoId: alias.id, file: join(alias.path, "src", "a.py") };
  const current = validateReplacementPath(viaAlias, () => [alias]);
  assert.ok(current);
  assert.equal(current(), true);
  mkdirSync(join(outside, "src"));
  writeFileSync(join(outside, "src", "a.py"), "Retry\n");
  rmSync(alias.path);
  symlinkSync(outside, alias.path, "junction");
  assert.equal(current(), false);
});
