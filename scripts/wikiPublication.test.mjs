import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";

import { publishWiki } from "./publishWiki.mjs";
import { wikiDifferences, wikiFiles, wikiPath, wikiPublicationPath, wikiImagePaths } from "./wikiFiles.mjs";

function git(directory, ...args) {
  return execFileSync("git", ["-C", directory, ...args], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function put(directory, name, body = name) {
  const path = join(directory, name);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, body);
}

function commit(directory, message) {
  git(directory, "add", "--all");
  git(directory, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", message);
  return git(directory, "rev-parse", "HEAD");
}

function fixture(context) {
  const base = mkdtempSync(join(tmpdir(), "spelunk-wiki-"));
  context.after(() => {
    rmSync(base, { recursive: true, force: true });
  });
  const repository = join(base, "source");
  const mirror = join(base, "wiki");
  const remote = join(base, "source.git");
  const wikiRemote = join(base, "wiki.git");
  git(base, "init", "--bare", "--initial-branch=develop", remote);
  git(base, "init", "--initial-branch=develop", repository);
  put(repository, "docs/wiki/Home.md", "# Home\n");
  put(repository, "docs/wiki/Screens.md", "# Screens\n");
  put(repository, "docs/wiki/images/mocks/Safe.png", Buffer.from([0x89, 0x50, 0x4e, 0x47]));
  const sha = commit(repository, "source");
  git(repository, "remote", "add", "origin", remote);
  git(repository, "push", "origin", "develop");
  git(base, "init", "--bare", "--initial-branch=master", wikiRemote);
  git(base, "init", "--initial-branch=master", mirror);
  put(mirror, "Home.md", "old home\n");
  put(mirror, "Obsolete.md", "removed\n");
  commit(mirror, "initial wiki");
  git(mirror, "remote", "add", "origin", wikiRemote);
  git(mirror, "push", "origin", "master");
  return { repository, mirror, remote, sha, base, wikiRemote };
}

test("wiki paths reject traversal, metadata, unsupported payloads and platform separators", () => {
  for (const name of [
    "../Home.md",
    "/Home.md",
    "a/../../Home.md",
    ".git/config",
    "images/.git/a.png",
    "images/../a.png",
    String.raw`images\a.png`,
    "Home.md\0",
    "script.mjs",
    "images/active.svg",
    "images//a.png",
  ]) {
    assert.equal(wikiPath(name), false, name);
  }
  for (const name of ["Home.md", "_Sidebar.md", "images/mocks/Screen.png", "images/Space and café.png"]) {
    assert.equal(wikiPath(name), true, name);
  }
});

test("publication copies safe assets, removes obsolete pages, preserves history and makes no duplicate commit", (context) => {
  const options = fixture(context);
  const previous = git(options.mirror, "rev-parse", "HEAD");
  assert.equal(publishWiki(options), "published");
  assert.deepEqual(wikiDifferences(join(options.repository, "docs/wiki"), options.mirror), []);
  assert.equal(git(options.mirror, "rev-parse", "HEAD^"), previous);
  assert.equal(git(options.wikiRemote, "show", "master:Home.md"), "# Home");
  assert.deepEqual(readFileSync(join(options.mirror, "images/mocks/Safe.png")), Buffer.from([0x89, 0x50, 0x4e, 0x47]));
  assert.match(git(options.mirror, "log", "-1", "--format=%s"), new RegExp(options.sha));
  const published = git(options.mirror, "rev-parse", "HEAD");
  assert.equal(publishWiki(options), "unchanged");
  assert.equal(git(options.mirror, "rev-parse", "HEAD"), published);
});

test("an older develop SHA cannot publish over newer code", (context) => {
  const options = fixture(context);
  const previous = git(options.wikiRemote, "rev-parse", "master");
  put(options.repository, "docs/wiki/Home.md", "# Newer source\n");
  commit(options.repository, "new develop");
  git(options.repository, "push", "origin", "develop");
  git(options.repository, "checkout", "--detach", options.sha);
  assert.equal(publishWiki(options), "stale");
  assert.equal(git(options.wikiRemote, "rev-parse", "master"), previous);
  assert.equal(git(options.mirror, "status", "--porcelain"), "");
});

test("an unrelated develop commit does not strand an otherwise current publication", (context) => {
  const options = fixture(context);
  put(options.repository, "implementation.txt", "unrelated code");
  commit(options.repository, "code only");
  git(options.repository, "push", "origin", "develop");
  git(options.repository, "checkout", "--detach", options.sha);
  assert.equal(publishWiki(options), "published");
  assert.equal(git(options.wikiRemote, "show", "master:Home.md"), "# Home");
});

test("new publication tooling supersedes an older run even when docs match", (context) => {
  const options = fixture(context);
  const previous = git(options.wikiRemote, "rev-parse", "master");
  put(options.repository, "scripts/wikiFiles.mjs", "new validator");
  commit(options.repository, "publication tooling");
  git(options.repository, "push", "origin", "develop");
  git(options.repository, "checkout", "--detach", options.sha);
  assert.equal(publishWiki(options), "stale");
  assert.equal(git(options.wikiRemote, "rev-parse", "master"), previous);
});

test("publication identity includes only paths that trigger the workflow", () => {
  for (const name of [
    "docs/wiki/Home.md",
    "scripts/wikiPublication.test.mjs",
    "scripts/publishWiki.mjs",
    "scripts/checkWikiSync.mjs",
    ".github/workflows/wiki.yml",
  ]) {
    assert.equal(wikiPublicationPath(name), true, name);
  }
  for (const name of ["daemon/search.go", "scripts/unrelated.mjs", "docs/dev/notes.md"]) {
    assert.equal(wikiPublicationPath(name), false, name);
  }
});

test("a develop advance while preparing publication leaves the remote untouched", (context) => {
  const options = fixture(context);
  const previous = git(options.wikiRemote, "rev-parse", "master");
  let checks = 0;
  const isCurrent = () => ++checks === 1;
  assert.equal(publishWiki(options, isCurrent), "stale");
  assert.equal(checks, 2);
  assert.equal(git(options.wikiRemote, "rev-parse", "master"), previous);
});

test("source symlinks and unexpected metadata fail before altering the mirror", (context) => {
  const options = fixture(context);
  const source = join(options.repository, "docs/wiki");
  const previous = git(options.mirror, "rev-parse", "HEAD");
  const outside = join(options.base, "outside.md");
  writeFileSync(outside, "private");
  symlinkSync(outside, join(source, "Escape.md"));
  options.sha = commit(options.repository, "unsafe link");
  git(options.repository, "push", "origin", "develop");
  assert.throws(() => publishWiki(options), /symlink/);
  assert.equal(git(options.mirror, "rev-parse", "HEAD"), previous);
  assert.equal(readFileSync(outside, "utf8"), "private");
  rmSync(join(source, "Escape.md"));
  mkdirSync(join(source, ".git"));
  assert.throws(() => wikiFiles(source), /Unsafe wiki path/);
});

test("symlink ancestors and leaf files in a mirror cannot redirect publication", (context) => {
  const options = fixture(context);
  const outside = join(options.base, "outside");
  mkdirSync(outside);
  put(outside, "Safe.png", "untouched");
  symlinkSync(outside, join(options.mirror, "images"));
  commit(options.mirror, "unsafe mirror directory");
  assert.throws(() => publishWiki(options), /symlink/);
  assert.equal(readFileSync(join(outside, "Safe.png"), "utf8"), "untouched");
  rmSync(join(options.mirror, "images"));
  rmSync(join(options.mirror, "Home.md"));
  symlinkSync(join(outside, "Safe.png"), join(options.mirror, "Home.md"));
  commit(options.mirror, "unsafe mirror leaf");
  assert.throws(() => publishWiki(options), /symlink/);
  assert.equal(readFileSync(join(outside, "Safe.png"), "utf8"), "untouched");
});

test("a concurrent wiki update is rejected without force or history loss", (context) => {
  const options = fixture(context);
  const other = join(options.base, "other");
  git(options.base, "clone", options.wikiRemote, other);
  put(other, "Home.md", "manual edit\n");
  const manual = commit(other, "manual wiki edit");
  git(other, "push", "origin", "master");
  assert.throws(() => publishWiki(options), /Command failed/);
  assert.equal(git(options.wikiRemote, "rev-parse", "master"), manual);
  assert.equal(git(options.wikiRemote, "show", "master:Home.md"), "manual edit");
});

test("missing source and dirty or mismatched checkouts never publish", (context) => {
  const options = fixture(context);
  assert.throws(() => wikiFiles(join(options.base, "missing")), /ENOENT/);
  assert.throws(() => publishWiki({ ...options, sha: "1".repeat(40) }), /differs from expected SHA/);
  put(options.repository, "docs/wiki/Home.md", "uncommitted");
  assert.throws(() => publishWiki(options), /Source checkout is dirty/);
});

test("the source checker sees Markdown images as well as HTML screenshot links", () => {
  const text = '<img src="images/a.png"> ![screen](images/b.png) <a href="images/c.png">';
  assert.deepEqual(wikiImagePaths(text), ["images/a.png", "images/b.png", "images/c.png"]);
});
