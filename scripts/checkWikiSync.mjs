#!/usr/bin/env node
// Checks the wiki still matches the repo's mock table (docs/dev/mocks.md):
// the same screens with the same numbers, ids and milestones on the Screens
// page, a section for each screen on its group page, an image for each canvas
// board (or one per variant), and the same design-canvas link. It also fails
// while the wiki has uncommitted or unpushed changes, which GitHub can't see.
//
// The wiki is its own git repo, so this looks for a clone next to this one
// (../unified-search.wiki) or at $UNIFIED_SEARCH_WIKI. Without a clone it
// says so and passes; CI never has one, so this runs only in the presubmit.
//
// Usage: node scripts/checkWikiSync.mjs
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { screenRows } from "./mockTable.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const wiki = process.env.UNIFIED_SEARCH_WIKI ?? join(root, "../unified-search.wiki");
const problems = [];

if (existsSync(join(wiki, "Screens.md"))) {
  checkScreens();
  checkWikiPushed();
  for (const problem of problems) console.log(`  ${problem}`);
  console.log(problems.length > 0 ? `${String(problems.length)} wiki problem(s)` : "Wiki matches the mocks.");
  if (problems.length > 0) process.exitCode = 1;
} else {
  console.log(`No wiki clone at ${wiki}; skipped (git clone the unified-search.wiki repo there to check it).`);
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
