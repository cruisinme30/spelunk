#!/usr/bin/env node
// Checks the wiki still matches the repo's mock table (docs/dev/mocks.md):
// the same screens with the same numbers, ids and milestones on the Screens
// page, a section for each screen on its group page, an image for each canvas
// board (or one per variant), and the same design-canvas link. Every screen
// the panel draws needs a built screenshot from docs/dev/proof/ in its section,
// and its Built column must say so. Every image a page shows must be committed
// under that exact name, since GitHub's paths are case-sensitive and the Mac's
// aren't. A screen's section is the block from `<!-- screen:<id> -->` to
// `<!-- /screen -->`, on whichever page documents that feature. Every wiki link
// must reach a page (and heading) that exists, every page must be in the
// sidebar, and the repo's links into the wiki must name real pages. Every
// operator must be on the Query Language page, every diagnostic code have an
// entry on the Error Index, and every setting and command be on Settings and
// Commands. It also
// fails while the wiki has uncommitted or unpushed changes, which GitHub can't
// see.
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
import { commands, diagnosticCodes, operatorNames, settingKeys } from "./specIds.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const wiki = process.env.SPELUNK_WIKI ?? join(root, "..", `${basename(root)}.wiki`);
const problems = [];
/** Screens with no built screenshot: they're VS Code's own UI, which scripts/screenshotPanel.mjs can't draw. */
const vsCodeUi = "VS Code's own UI";
/** A screen's section: its mock, its built screenshot and what it shows. */
const screenBlock = /<!-- screen:([\w-]+) -->\n([\s\S]*?)<!-- \/screen -->/g;
/** Links into the wiki from anywhere: https://github.com/cruisinme30/spelunk/wiki/<page>#<anchor>. */
const wikiUrl = /https:\/\/github\.com\/cruisinme30\/spelunk\/wiki\/([\w%.-]+)(?:#([\w%-]+))?/g;

if (existsSync(join(wiki, "Screens.md"))) {
  checkScreens();
  checkImages();
  checkLinks();
  checkCoverage();
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
 * Checks each screen's block and built screenshot against its Screens row, and
 * that every image the pages show is a committed file: images/ in the wiki,
 * and docs/dev/proof/ in this repo, which the pages load from GitHub.
 */
function checkImages() {
  const wikiFiles = new Set(git("ls-files").split("\n"));
  const proofFiles = new Set(
    execFileSync("git", ["-C", root, "ls-files", "docs/dev/proof"], { encoding: "utf8" }).trim().split("\n"),
  );
  for (const [page, text] of wikiPages()) {
    for (const [, path] of text.matchAll(/(?:src|href)="(images\/[^"]+)"|\]\((images\/[^)\s]+)\)/g)) {
      if (path && !wikiFiles.has(path)) problems.push(`${page}.md shows ${path}, which isn't committed to the wiki`);
    }
    for (const [path] of text.matchAll(/docs\/dev\/proof\/[\w.-]+/g)) {
      if (!proofFiles.has(path)) problems.push(`${page}.md shows ${path}, which isn't committed to this repo`);
    }
  }
  const blocks = screenBlocks();
  for (const row of screenRows(readFileSync(join(wiki, "Screens.md"), "utf8"))) checkScreenBlock(row, blocks);
}

/**
 * Every screen block on the wiki, by screen id, with the page that shows it.
 * @returns {Map<string, {page: string, body: string}>} The blocks.
 */
function screenBlocks() {
  const blocks = new Map();
  for (const [page, text] of wikiPages()) {
    for (const [, id, body] of text.matchAll(screenBlock)) {
      if (blocks.has(id)) problems.push(`${String(id)} has a screen block on ${blocks.get(id).page} and ${page}`);
      else blocks.set(id, { page, body });
    }
  }
  return blocks;
}

/** Checks one Screens row against its screen's block: where it is, and whether it has a built screenshot. */
function checkScreenBlock(row, blocks) {
  const id = row["Screen id"];
  const block = blocks.get(id);
  if (!block) {
    problems.push(`${id} has no <!-- screen:${id} --> … <!-- /screen --> block on a wiki page`);
    return;
  }
  const target = /\]\(([\w-]+)(?:#[^)]*)?\)/.exec(row.Screen ?? "")?.[1];
  if (target !== block.page) problems.push(`${id}: Screens.md links to ${String(target)}, but ${block.page} shows it`);
  const built = /docs\/dev\/proof\//.test(block.body);
  if (row.Built === vsCodeUi) {
    if (built) problems.push(`${id}: Screens.md says ${vsCodeUi}, but its section has a built screenshot`);
  } else if (!built) {
    problems.push(`${id} has no built screenshot from docs/dev/proof/ (or mark it "${vsCodeUi}" in Screens.md)`);
  } else if (!row.Built.includes("✓")) {
    problems.push(`${id} has a built screenshot, but Screens.md's Built column isn't ✓`);
  }
}

/**
 * Checks that every link between wiki pages reaches a page, and a heading on
 * it when it names one; that the sidebar lists every page; and that the
 * repo's own docs link only to pages that exist.
 */
function checkLinks() {
  const pages = wikiPages();
  const anchors = new Map([...pages].map(([page, text]) => [page, headingAnchors(text)]));
  /** Flags a link from `from` to `page#anchor` that goes nowhere. */
  const check = (from, page, anchor) => {
    const name = decodeURIComponent(page).replaceAll(" ", "-");
    if (!anchors.has(name)) problems.push(`${from} links to a page ${page} that doesn't exist`);
    else if (anchor === "") problems.push(`${from} links to ${name}# with no heading after the #`);
    else if (anchor && !anchors.get(name).has(decodeURIComponent(anchor).toLowerCase()))
      problems.push(`${from} links to ${name}#${anchor}, which isn't a heading there`);
  };
  for (const [page, text] of pages) {
    for (const [linked, anchor] of pageLinks(text)) check(page, linked || page, anchor);
  }
  const listed = new Set(pageLinks(pages.get("_Sidebar") ?? "").map(([linked]) => linked));
  for (const page of pages.keys()) {
    if (!page.startsWith("_") && page !== "Home" && !listed.has(page)) problems.push(`${page} isn't in _Sidebar.md`);
  }
  const repoDocuments = execFileSync("git", ["-C", root, "ls-files", "*.md"], { encoding: "utf8" }).trim().split("\n");
  for (const document of repoDocuments.filter((path) => !path.startsWith("testdata/"))) {
    for (const [, linked, anchor] of readFileSync(join(root, document), "utf8").matchAll(wikiUrl))
      check(document, linked ?? "", anchor);
  }
}

/**
 * The wiki pages each link in a page goes to, as [page, anchor]: [[Page]] and
 * [[Text|Page]], relative [text](Page#anchor) links (page "" is this page),
 * and full wiki URLs. Code is skipped, and so are images and other sites.
 * @param {string} text Markdown source.
 * @returns {[string, string | undefined][]} The links.
 */
function pageLinks(text) {
  const links = [];
  const prose = withoutCode(text);
  for (const [, inner] of prose.matchAll(/\[\[([^\]]+)\]\]/g)) {
    const [page = "", anchor] = (inner?.split("|").at(-1) ?? "").trim().split("#");
    links.push([page.replaceAll(" ", "-"), anchor]);
  }
  for (const [, target = ""] of prose.matchAll(/\]\(([^)\s]+)\)/g)) {
    if (/^(?:[a-z]+:|images\/|\.\.?\/)/.test(target)) continue;
    const [page = "", anchor] = target.split("#");
    links.push([page, anchor]);
  }
  for (const [, page = "", anchor] of prose.matchAll(wikiUrl)) links.push([page, anchor]);
  return links;
}

/**
 * Each wiki page's name (its file without .md) and its text.
 * @returns {Map<string, string>} The pages.
 */
function wikiPages() {
  return new Map(
    readdirSync(wiki)
      .filter((file) => file.endsWith(".md"))
      .map((file) => [file.slice(0, -3), readFileSync(join(wiki, file), "utf8")]),
  );
}

/**
 * The anchors GitHub gives a page's headings (lowercase, punctuation dropped,
 * spaces as hyphens, a repeat numbered -1, -2, ...), plus any `id="..."`.
 * @param {string} text Markdown source.
 * @returns {Set<string>} The anchors.
 */
function headingAnchors(text) {
  const found = new Set();
  const plain = withoutFences(text);
  for (const [, heading] of plain.matchAll(/^#{1,6} +(.+?) *#*$/gm)) {
    const base = (heading ?? "")
      .replaceAll(/\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replaceAll(/<[^>]+>/g, "")
      .toLowerCase()
      .replaceAll(/[^\p{L}\p{N}\s_-]/gu, "")
      .replaceAll(" ", "-");
    let anchor = base;
    for (let n = 1; found.has(anchor); n++) anchor = `${base}-${String(n)}`;
    found.add(anchor);
  }
  for (const [, id] of text.matchAll(/\b(?:id|name)="([\w-]+)"/g)) found.add((id ?? "").toLowerCase());
  return found;
}

/** Markdown without fenced code blocks, whose text is never a heading. */
function withoutFences(text) {
  return text.replaceAll(/^```[\s\S]*?^```/gm, "");
}

/** Markdown without code, fenced or inline, whose text is never a link. */
function withoutCode(text) {
  return withoutFences(text).replaceAll(/`[^`\n]*`/g, "");
}

/**
 * Checks that the wiki describes every operator, diagnostic code, setting and
 * command the code defines, each on the page a reader would look for it.
 */
function checkCoverage() {
  const pages = wikiPages();
  /** Flags each of `names` that `page` doesn't mention as `shown(name)`. */
  const expect = (page, names, shown, what) => {
    const text = pages.get(page) ?? "";
    for (const name of names)
      if (!text.includes(shown(name))) problems.push(`${page}.md doesn't list the ${what} ${name}`);
  };
  expect("Query-Language", operatorNames(root), (name) => `\`${name}:\``, "operator");
  expect("Error-Index", diagnosticCodes(root), (code) => `### \`${code}\``, "diagnostic");
  expect("Settings-and-Commands", settingKeys(root), (key) => `\`${key}\``, "setting");
  expect(
    "Settings-and-Commands",
    commands(root).map((command) => command.title.replace(/^Spelunk: /, "")),
    (title) => title,
    "command",
  );
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
