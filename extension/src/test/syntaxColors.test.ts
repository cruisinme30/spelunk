// Syntax colors: vscode-textmate running a grammar an extension contributes,
// with a theme read as VS Code reads it, and the host coloring what it posts.
// The grammar and theme are small files written for each test.
// @covers msg:syntax.lines
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { readColorTheme, themeFile } from "../colorTheme";
import type { CellPlace as ResultCell, ResultItem, SyntaxLinesMessage } from "../protocol.gen";
import { GrammarCatalog, type InstalledExtension, SyntaxColors, type ThemeRule } from "../syntaxColors";
import { type FileLines, SyntaxPosts, type SyntaxSource } from "../syntaxLines";

/** vscode-oniguruma's engine, from the repo's node_modules (dist-test/ is two folders down). */
const wasm = () =>
  Promise.resolve(readFileSync(join(__dirname, "../../node_modules/vscode-oniguruma/release/onig.wasm")));

/** A token's style: a color index plus 512 times its font style. */
const ITALIC = 512;

/** An extension contributing the "toy" language (.toy files, and TOYS) with a small grammar, and two themes. */
function toyExtension(): InstalledExtension {
  const extensionPath = mkdtempSync(join(tmpdir(), "spelunk-syntax-"));
  mkdirSync(join(extensionPath, "syntaxes"));
  mkdirSync(join(extensionPath, "themes"));
  writeFileSync(
    join(extensionPath, "syntaxes", "toy.tmLanguage.json"),
    JSON.stringify({
      scopeName: "source.toy",
      patterns: [
        { match: "#.*", name: "comment.line.toy" },
        { begin: String.raw`/\*`, end: String.raw`\*/`, name: "comment.block.toy" },
        { match: String.raw`\b(if|return)\b`, name: "keyword.control.toy" },
        { match: '"[^"]*"', name: "string.quoted.toy" },
      ],
    }),
  );
  return {
    extensionPath,
    packageJSON: {
      contributes: {
        languages: [{ id: "toy", extensions: [".toy"], filenames: ["TOYS"] }],
        grammars: [{ language: "toy", scopeName: "source.toy", path: "./syntaxes/toy.tmLanguage.json" }],
        themes: [
          { id: "Toy Dark", label: "Toy Dark Label", uiTheme: "vs-dark", path: "./themes/dark.json" },
          { id: "Dark Modern", uiTheme: "vs-dark", path: "./themes/dark.json" },
        ],
      },
    },
  };
}

const RULES: ThemeRule[] = [
  { settings: { foreground: "#d4d4d4" } },
  { scope: "comment", settings: { foreground: "#6a9955", fontStyle: "italic" } },
  { scope: "keyword.control", settings: { foreground: "#c586c0" } },
  { scope: "string", settings: { foreground: "#ce9178" } },
];

/** The color and font style of each token of `tokens`, by the text it covers. */
function styled(text: string, tokens: number[], colors: string[]): [string, string][] {
  const out: [string, string][] = [];
  for (let index = 0; index < tokens.length; index += 2) {
    const start = tokens[index] ?? 0;
    const end = tokens[index + 2] ?? text.length;
    const style = tokens[index + 1] ?? 0;
    const color = colors[style % ITALIC] ?? "";
    out.push([text.slice(start, end), style >= ITALIC ? `${color} italic` : color]);
  }
  return out;
}

test("the grammar an extension contributes colors its files with the theme's colors", async () => {
  const extension = toyExtension();
  const colors = new SyntaxColors(new GrammarCatalog([extension]), RULES, wasm);
  const lines = ['if x return "y" # done'];
  const tokens = await colors.tokenize("src/a.toy", lines, true);
  assert.ok(tokens);
  const palette = colors.colors();
  assert.deepEqual(
    styled(lines[0] ?? "", tokens[0] ?? [], palette).map(([text, color]) => [text, color.toLowerCase()]),
    [
      ["if", "#c586c0"],
      [" x ", "#d4d4d4"],
      ["return", "#c586c0"],
      [" ", "#d4d4d4"],
      ['"y"', "#ce9178"],
      [" ", "#d4d4d4"],
      ["# done", "#6a9955 italic"],
    ],
  );
  assert.ok(await colors.tokenize("TOYS", lines, true), "a file name the language claims");
  assert.equal(await colors.tokenize("a.txt", lines, true), undefined, "no grammar colors it");
});

test("consecutive lines carry a comment over; lines on their own don't", async () => {
  const colors = new SyntaxColors(new GrammarCatalog([toyExtension()]), RULES, wasm);
  const lines = ["/* opened", "return here */ if"];
  const palette = colors.colors();
  const comment = palette.findIndex((color) => color.toLowerCase() === "#6a9955");
  const continued = await colors.tokenize("a.toy", lines, true);
  assert.equal(continued?.[1]?.[1], comment + ITALIC, "the second line starts inside the comment");
  const alone = await colors.tokenize("a.toy", lines, false);
  assert.notEqual(alone?.[1]?.[1], comment + ITALIC, "each line on its own");
});

test("a theme is read as VS Code reads it: comments, includes, a tokenColors file and the user's customizations", async () => {
  const extension = toyExtension();
  const themes = join(extension.extensionPath, "themes");
  writeFileSync(
    join(themes, "base.json"),
    `{ // the base
      "colors": { "editor.foreground": "#111111", "editor.background": "#000000" },
      "tokenColors": [{ "scope": "string", "settings": { "foreground": "#222222" } },],
    }`,
  );
  writeFileSync(
    join(themes, "rules.json"),
    JSON.stringify([{ scope: "comment", settings: { foreground: "#333333" } }]),
  );
  writeFileSync(
    join(themes, "dark.json"),
    `/* the theme */ { "include": "./base.json", "tokenColors": "./rules.json", "name": "has // in a string",
      "colors": { "editor.background": "#0a0a0a" } }`,
  );
  const found = { file: join(themes, "dark.json"), uiTheme: "vs-dark" };
  assert.deepEqual(themeFile([extension], "Toy Dark"), found);
  assert.deepEqual(themeFile([extension], "Toy Dark Label"), found, "an older setting names the label");
  assert.deepEqual(themeFile([extension], "Default Dark Modern"), found, "VS Code's old name for Dark Modern");
  assert.equal(themeFile([extension], "Missing"), undefined);

  const { rules, colors } = await readColorTheme(join(themes, "dark.json"), "Toy Dark", {
    keywords: "#444444",
    textMateRules: [{ scope: "string", settings: { foreground: "#555555" } }],
    "[Toy Dark]": { comments: { foreground: "#666666" } },
    "[Other Theme]": { comments: "#777777" },
  });
  assert.deepEqual(
    rules.map((rule) => [rule.scope ?? null, rule.settings.foreground]),
    [
      [null, "#111111"],
      ["string", "#222222"],
      ["comment", "#333333"],
      [["keyword - keyword.operator", "keyword.control", "storage", "storage.type"], "#444444"],
      ["string", "#555555"],
      [["comment", "punctuation.definition.comment"], "#666666"],
    ],
  );
  assert.deepEqual(
    colors,
    { "editor.foreground": "#111111", "editor.background": "#0a0a0a" },
    "its own over the base's",
  );
});

test("results and previews posted to the panel are colored after them, and again from scratch on a theme change", async () => {
  const asked: FileLines[][] = [];
  const posted: SyntaxLinesMessage[] = [];
  const source = {
    color: (files: readonly FileLines[]) => {
      asked.push([...files]);
      return Promise.resolve({
        colors: ["", "#ffffff"],
        lines: files.flatMap((file) => file.lines.map((text) => ({ path: file.path, text, tokens: [0, 1] }))),
      });
    },
  };
  const posts = new SyntaxPosts(source, (message) => posted.push(message));
  const item: ResultItem = {
    kind: "line",
    ref: "l1",
    repoId: "r",
    path: "a.toy",
    line: 5,
    text: "return x",
    hits: [],
    contextBefore: ["if y"],
    spanLines: [{ text: "# more", hits: [] }],
  };
  posts.posted("search.batch", {
    seq: 1,
    searchId: "s1",
    items: [item, { kind: "file", ref: "f1", repoId: "r", path: "a.toy", nameHits: [], dirty: false }],
  });
  posts.posted("preview.result", {
    ref: "l1",
    preview: {
      kind: "file",
      path: "a.toy",
      firstLine: 4,
      lines: ["if y", "return x"],
      focusLine: 5,
      hits: [],
      dirtyLines: [],
    },
  });
  posts.posted("index.status", { repos: [] });
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(asked, [
    [{ path: "a.toy", lines: ["if y", "return x", "# more"], continued: true }],
    [{ path: "a.toy", lines: ["if y", "return x"], continued: true }],
  ]);
  assert.equal(posted.length, 2);
  assert.equal(posted.at(0)?.reset, undefined);

  posts.posted("search.batch", { seq: 2, searchId: "s2", items: [{ ...item, text: "if z", contextBefore: [] }] });
  posts.recolor();
  await new Promise((resolve) => setImmediate(resolve));
  const reset = posted.at(-1);
  assert.equal(reset?.reset, true);
  assert.deepEqual(
    reset.lines.map((line) => line.text),
    ["if z", "# more", "if y", "return x"],
    "the new search's results (not the old one's) and the preview",
  );
});

/** Where a notebook result is: its cell, line 1. */
function cellAt(number: number, kind: "code" | "markdown" | "output", language?: string): ResultCell {
  return { number, kind, line: 1, ...(language ? { language } : {}) };
}

/** Takes what the panel would be posted, and drops it. */
function ignore(): void {
  // Only what is asked to be colored matters here.
}

test("a notebook's cells are colored each in its own language, and printed output not at all", async () => {
  // @covers screen:notebook-cells
  const colors = new SyntaxColors(new GrammarCatalog([toyExtension()]), RULES, wasm);
  assert.equal(await colors.tokenize("nb.ipynb", ["return x"], true), undefined, "a notebook's path says no language");
  assert.ok(await colors.tokenize("nb.ipynb", ["return x"], true, "toy"), "its cell's language colors it");
  assert.equal(
    await colors.color([{ path: "a.toy", lines: ["return x"], continued: true, language: null }]),
    undefined,
  );

  const asked: FileLines[][] = [];
  const color: SyntaxSource["color"] = (files) => {
    asked.push([...files]);
    return Promise.resolve({ colors: [], lines: [] });
  };
  const posts = new SyntaxPosts({ color }, ignore);
  posts.posted("search.batch", {
    seq: 1,
    searchId: "s1",
    items: [
      {
        kind: "line",
        ref: "l1",
        repoId: "r",
        path: "nb.ipynb",
        line: 4,
        text: "return x",
        hits: [],
        cell: cellAt(2, "code", "toy"),
      },
      {
        kind: "line",
        ref: "l2",
        repoId: "r",
        path: "nb.ipynb",
        line: 9,
        text: "loaded 5",
        hits: [],
        cell: cellAt(4, "output"),
      },
    ],
  });
  posts.posted("preview.result", {
    ref: "l1",
    preview: {
      kind: "file",
      path: "nb.ipynb",
      firstLine: 2,
      lines: ["notes", "return x", "if y"],
      focusLine: 3,
      hits: [],
      dirtyLines: [],
      cells: [
        { number: 1, kind: "markdown", language: "markdown", startLine: 1, lineCount: 2 },
        { number: 2, kind: "code", language: "toy", startLine: 3, lineCount: 2 },
      ],
    },
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(asked, [
    [
      { path: "nb.ipynb", lines: ["return x"], continued: true, language: "toy" },
      { path: "nb.ipynb", lines: ["loaded 5"], continued: true, language: null },
    ],
    [
      { path: "nb.ipynb", lines: ["notes"], continued: true, language: "markdown" },
      { path: "nb.ipynb", lines: ["return x", "if y"], continued: true, language: "toy" },
    ],
  ]);
});

test("with nothing to color, a theme change still clears the old colors", async () => {
  const posted: SyntaxLinesMessage[] = [];
  const posts = new SyntaxPosts(
    // eslint-disable-next-line unicorn/no-useless-undefined -- what a source answers when it colors nothing
    { color: () => Promise.resolve<SyntaxLinesMessage | undefined>(undefined) },
    (message) => posted.push(message),
  );
  posts.posted("search.batch", { seq: 1, searchId: "s1", items: [] });
  posts.recolor();
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(posted, [{ colors: [], lines: [], reset: true }]);
});
