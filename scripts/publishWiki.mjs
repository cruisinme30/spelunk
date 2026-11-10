#!/usr/bin/env node
// Publishes an already validated, trusted develop commit. Never executes wiki content.
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { wikiFiles, wikiDifferences, wikiPublicationPath } from "./wikiFiles.mjs";

/** Runs Git without a shell, interactive prompts, hooks or signing programs. */
function git(directory, ...args) {
  return execFileSync(
    "git",
    ["-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-C", directory, ...args],
    {
      encoding: "utf8",
      env: { ...process.env, GIT_TERMINAL_PROMPT: "0" },
      stdio: ["ignore", "pipe", "pipe"],
    },
  ).trim();
}

/** Identifies every published file and the tooling that validated/publishes it. */
function sourceIdentity(directory, sha) {
  return git(directory, "ls-tree", "-r", "-z", sha, "--", "docs/wiki", "scripts", ".github/workflows/wiki.yml")
    .split("\0")
    .filter((entry) => wikiPublicationPath(entry.slice(entry.indexOf("\t") + 1)))
    .join("\0");
}

/** Rejects newer docs/tooling; unrelated code pushes do not strand a path-filtered publication. */
function currentSource(options) {
  const result = git(options.mirror, "ls-remote", "--exit-code", options.remote, "refs/heads/develop");
  const current = result.split(/\s/)[0];
  if (current === options.sha) return true;
  if (!/^[\da-f]{40}$/.test(current)) throw new Error("Invalid remote develop SHA");
  // Fetch objects only; do not move the wiki branch or execute any fetched content.
  git(options.mirror, "fetch", "--no-tags", "--depth=1", "--filter=blob:none", options.remote, current);
  return sourceIdentity(options.repository, options.sha) === sourceIdentity(options.mirror, current);
}

/** Refuses unchecked revisions, dirty mirrors, unexpected branches and unsafe file trees before mutation. */
function validate(options) {
  if (!/^[\da-f]{40}$/.test(options.sha)) throw new Error("Expected a complete source commit SHA");
  if (git(options.repository, "rev-parse", "HEAD") !== options.sha)
    throw new Error("Source checkout differs from expected SHA");
  if (git(options.repository, "status", "--porcelain") !== "") throw new Error("Source checkout is dirty");
  if (git(options.mirror, "status", "--porcelain") !== "") throw new Error("Wiki checkout is dirty");
  if (git(options.mirror, "branch", "--show-current") !== "master") throw new Error("Wiki checkout must use master");
  const files = wikiFiles(join(options.repository, "docs/wiki"));
  if (!files.includes("Home.md") || !files.includes("Screens.md")) throw new Error("Wiki source is incomplete");
  wikiFiles(options.mirror, true);
  return files;
}

/** Copies only the allowlisted source tree, preserving the mirror's own Git history. */
function copySource(options, files) {
  const wanted = new Set(files);
  for (const name of wikiFiles(options.mirror, true)) {
    if (!wanted.has(name)) rmSync(join(options.mirror, name));
  }
  for (const name of files) {
    const target = join(options.mirror, name);
    mkdirSync(dirname(target), { recursive: true });
    copyFileSync(join(options.repository, "docs/wiki", name), target);
  }
}

/** Publishes one trusted source SHA, or returns stale/unchanged without touching the remote. */
export function publishWiki(options, isCurrent = currentSource) {
  const files = validate(options);
  if (!isCurrent(options)) return "stale";
  if (wikiDifferences(join(options.repository, "docs/wiki"), options.mirror).length === 0) return "unchanged";
  copySource(options, files);
  git(options.mirror, "add", "--all", "--", ".");
  git(
    options.mirror,
    "-c",
    "user.name=github-actions[bot]",
    "-c",
    "user.email=41898282+github-actions[bot]@users.noreply.github.com",
    "commit",
    "-m",
    `docs: mirror spelunk ${options.sha}`,
  );
  if (!isCurrent(options)) return "stale";
  // Normal fast-forward only: a concurrent/manual wiki update must fail, never be overwritten.
  git(options.mirror, "push", "origin", "HEAD:refs/heads/master");
  return "published";
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [repository, mirror, sha, remote] = process.argv.slice(2);
  if (!repository || !mirror || !sha || !remote || process.argv.length !== 6) {
    throw new Error("Usage: publishWiki.mjs SOURCE_CHECKOUT WIKI_CHECKOUT SOURCE_SHA SOURCE_REMOTE");
  }
  console.log(`Wiki publication: ${publishWiki({ repository, mirror, sha, remote })}`);
}
