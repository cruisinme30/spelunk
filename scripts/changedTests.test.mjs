// Dependency-closure and safe-default tests for develop's behavioral selection.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { testLayers } from "./changedTests.mjs";

const all = ["daemon", "extension", "webview"];

test("backend changes test every consumer", () => {
  assert.deepEqual(testLayers(["daemon/internal/query/checks.go"]), all);
});

test("extension changes test the host and webview contracts", () => {
  assert.deepEqual(testLayers(["extension/src/host.ts"]), ["extension", "webview"]);
});

test("webview-only changes select webview behavior", () => {
  assert.deepEqual(testLayers(["webview/src/panel.ts", "webview/test/search.test.mjs"]), ["webview"]);
});

test("documentation-only changes still leave static checks to the runner", () => {
  assert.deepEqual(testLayers(["README.md", "docs/dev/progress.md", "docs/dev/proof/panel.png", "LICENSE"]), []);
});

test("shared contracts, config, tooling, fixtures and unknown paths run everything", () => {
  for (const path of [
    "protocol/protocol.schema.json",
    "package-lock.json",
    ".github/workflows/ci.yml",
    "scripts/test-all.sh",
    "testdata/repos/example.go",
    "new-engine/source.rs",
    "new-engine/unknown.md",
  ]) {
    assert.deepEqual(testLayers(["webview/src/panel.ts", path]), all, path);
  }
});

test("renames crossing a subsystem preserve both dependency closures", () => {
  // git diff --no-renames emits both paths, including names with spaces.
  assert.deepEqual(testLayers(["daemon/internal/old name.go", "webview/new name.ts"]), all);
});

test("an empty change set requires static quality but no behavioral rerun", () => {
  assert.deepEqual(testLayers([]), []);
});

test("CLI detects deleted paths and fails closed on an invalid base", (context) => {
  const directory = mkdtempSync(join(tmpdir(), "spelunk-changed-tests-"));
  context.after(() => rmSync(directory, { recursive: true, force: true }));
  const git = (...args) =>
    execFileSync("git", args, { cwd: directory, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  git("init", "--initial-branch=main");
  mkdirSync(join(directory, "daemon"));
  writeFileSync(join(directory, "daemon", "old file.go"), "package old\n");
  git("add", ".");
  const commit = () => git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixture");
  commit();
  const base = git("rev-parse", "HEAD").trim();
  rmSync(join(directory, "daemon", "old file.go"));
  mkdirSync(join(directory, "webview"));
  writeFileSync(join(directory, "webview", "new file.ts"), "export {};\n");
  git("add", "-A");
  commit();
  const script = fileURLToPath(new URL("changedTests.mjs", import.meta.url));
  const selected = execFileSync(process.execPath, [script, "--base", base, "--print"], {
    cwd: directory,
    encoding: "utf8",
  });
  assert.equal(selected.trim(), "daemon,extension,webview");
  const failed = spawnSync(process.execPath, [script, "--base", "missing-commit", "--print"], {
    cwd: directory,
    encoding: "utf8",
  });
  assert.notEqual(failed.status, 0);
  assert.equal(failed.stdout, "");
});
