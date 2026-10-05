#!/usr/bin/env node
// Checks the wiki still matches the repo's mock table (docs/dev/mocks.md):
// the same screens with the same numbers, ids and milestones on the Screens
// page, a section for each screen on its group page, an image for each canvas
// board (or one per variant), and the same design-canvas link. Every screen
// the panel draws needs a built screenshot from docs/dev/proof/ in its section,
// and its Built column must say so. Every image a page shows must be committed
// under that exact name, since GitHub's paths are case-sensitive and the Mac's
// aren't. It also fails while the wiki has uncommitted or unpushed changes,
// which GitHub can't see.
//
// The wiki is its own git repo, so this looks for a clone next to this one
// (../<this folder>.wiki, e.g. ../spelunk.wiki) or at $SPELUNK_WIKI. Without a clone it
// says so and passes; CI never has one, so this runs only in the presubmit.
//
// Usage: node scripts/checkWikiSync.mjs
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { screenRows } from "./mockTable.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const wiki = process.env.SPELUNK_WIKI ?? join(root, "..", `${basename(root)}.wiki`);
const problems = [];
/** Screens with no built screenshot: they're VS Code's own UI, which scripts/screenshotPanel.mjs can't draw. */
const vsCodeUi = "VS Code's own UI";

if (existsSync(join(wiki, "Screens.md"))) {
  checkScreens();
  checkImages();
  checkWikiPushed();
  for (const problem of problems) console.log(`  ${problem}`);
  console.log(problems.length > 0 ? `${String(problems.length)} wiki problem(s)` : "Wiki matches the mocks.");
  if (problems.length > 0) process.exitCode = 1;
} else {
  console.log(`No wiki clone at ${wiki}; skipped (git clone the spelunk.wiki repo there to check it).`);
}

/** Compares docs/dev/mocks.md with the wiki's Screens page and group pages. */
function checkScreens() {
  const mocksText = readFileSync(join(root, "docs/dev/mocks.md"), "utf8");
  const screensText = readFileSync(join(wiki, "Screens.md"), "utf8");
  const wikiRows = new Map(screenRows(screensText).map((row) => [row["#"], row]));
  const images = readdirSync(join(wiki, "images/mocks"));
  const pages = readdirSync(wiki)
    .filter((name) => name.endsWith(".md"))
    .map((name) => readFileSync(join(wiki, name), "utf8"))
    .join("\n");
  for (const mock of screenRows(mocksText)) {
    const id = mock["Screen id"];
    const row = wikiRows.get(mock["#"]);
    wikiRows.delete(mock["#"]);
    if (!row) problems.push(`mock ${mock["#"]} (${id}) is missing from Screens.md`);
    else if (row["Screen id"] !== id)
      problems.push(`mock ${mock["#"]}: Screens.md says ${row["Screen id"]}, mocks.md ${id}`);
    else if (row.Milestone !== mock.Milestone) {
      problems.push(`${id}: Screens.md says ${row.Milestone}, mocks.md ${mock.Milestone}`);
    }
    if (!pages.includes(`Screen id \`${id}\``))
      problems.push(`${id} has no "Screen id \`${id}\`" section on a wiki page`);
    // A board with variants has one image per variant: OtherValues-repo.png, ...
    const board = mock["Canvas board"];
    if (!images.some((name) => name === `${board}.png` || (name.startsWith(`${board}-`) && name.endsWith(".png")))) {
      problems.push(`${id}: no wiki image images/mocks/${board}.png (or ${board}-<variant>.png)`);
    }
  }
  for (const [number, row] of wikiRows)
    problems.push(`Screens.md lists mock ${number} (${row["Screen id"]}), mocks.md doesn't`);
  const canvas = /https:\/\/claude\.ai\/artifact\/\w+/.exec(mocksText)?.[0];
  if (!canvas || !screensText.includes(canvas))
    problems.push(`Screens.md doesn't link the canvas in mocks.md (${String(canvas)})`);
}

/**
 * Checks each screen's built screenshot against its Built column, and that
 * every image the pages show is a committed file: images/ in the wiki, and
 * docs/dev/proof/ in this repo, which the pages load from GitHub.
 */
function checkImages() {
  const sections = new Map();
  const wikiFiles = new Set(git("ls-files").split("\n"));
  const proofFiles = new Set(
    execFileSync("git", ["-C", root, "ls-files", "docs/dev/proof"], { encoding: "utf8" }).trim().split("\n"),
  );
  for (const name of readdirSync(wiki).filter((file) => file.endsWith(".md"))) {
    const text = readFileSync(join(wiki, name), "utf8");
    for (const [, number, body] of text.matchAll(/^## (\d+) [^\n]*\n([\s\S]*?)(?=^## |(?![\s\S]))/gm))
      sections.set(number, body);
    for (const [, path] of text.matchAll(/(?:src|href)="(images\/[^"]+)"|\]\((images\/[^)\s]+)\)/g)) {
      if (path && !wikiFiles.has(path)) problems.push(`${name} shows ${path}, which isn't committed to the wiki`);
    }
    for (const [path] of text.matchAll(/docs\/dev\/proof\/[\w.-]+/g)) {
      if (!proofFiles.has(path)) problems.push(`${name} shows ${path}, which isn't committed to this repo`);
    }
  }
  for (const row of screenRows(readFileSync(join(wiki, "Screens.md"), "utf8"))) {
    const id = row["Screen id"];
    const built = /docs\/dev\/proof\//.test(sections.get(row["#"]) ?? "");
    if (row.Built === vsCodeUi) {
      if (built) problems.push(`${id}: Screens.md says ${vsCodeUi}, but its section has a built screenshot`);
    } else if (!built) {
      problems.push(`${id} has no built screenshot from docs/dev/proof/ (or mark it "${vsCodeUi}" in Screens.md)`);
    } else if (!row.Built.includes("✓"))
      problems.push(`${id} has a built screenshot, but Screens.md's Built column isn't ✓`);
  }
}

/** Flags wiki edits that GitHub can't see yet. */
function checkWikiPushed() {
  if (git("status", "--porcelain") !== "") problems.push("the wiki has uncommitted changes");
  try {
    if (git("rev-list", "--count", "@{upstream}..HEAD") !== "0") problems.push("the wiki has unpushed commits");
  } catch {
    problems.push("the wiki's branch has no upstream to compare with");
  }
}

/** Runs git in the wiki clone and returns its trimmed output. */
function git(...args) {
  return execFileSync("git", ["-C", wiki, ...args], { encoding: "utf8" }).trim();
}
