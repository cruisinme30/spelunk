// The extension manifest: the keys each shortcut preset binds, where F4 applies, and how settings are grouped.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { MOVED_SETTINGS, USER_ONLY_SETTINGS } from "../settings";

interface Keybinding {
  command: string;
  key: string;
  mac?: string;
  when?: string;
  args?: unknown;
}

interface Manifest {
  contributes: {
    commands: { command: string; title: string }[];
    menus: {
      commandPalette: { command: string; when?: string }[];
      "webview/context": { command?: string; submenu?: string; when: string }[];
      "spelunk.export": { command: string }[];
      "editor/context": { command: string; group: string; when?: string }[];
      "explorer/context": { command: string; group: string; when: string }[];
    };
    keybindings: Keybinding[];
    configuration: {
      title: string;
      properties: Record<string, { enum?: string[]; deprecationMessage?: string; scope?: string }>;
    }[];
  };
}

const manifest = JSON.parse(readFileSync(join(__dirname, "../package.json"), "utf8")) as Manifest;

/** The setting key declares, from whichever section holds it. */
function setting(key: string): { enum?: string[]; scope?: string } | undefined {
  return manifest.contributes.configuration.find((section) => key in section.properties)?.properties[key];
}

/** The "command key/mac" pairs bound while a preset is chosen. */
function boundFor(preset: string): string[] {
  return manifest.contributes.keybindings
    .filter((binding) => binding.when === `config.spelunk.shortcut.preset == '${preset}'`)
    .map((binding) => `${binding.command} ${binding.key}/${binding.mac ?? binding.key}`);
}

test("each shortcut preset binds its keys, and none binds nothing", () => {
  // @covers setting:shortcut.preset
  const presets = setting("spelunk.shortcut.preset")?.enum;
  assert.deepEqual(presets, ["quickOpen", "findInFiles", "none"]);
  assert.deepEqual(boundFor("quickOpen"), [
    "spelunk.open ctrl+p/cmd+p",
    "workbench.action.quickOpen ctrl+alt+p/cmd+alt+p",
  ]);
  assert.deepEqual(boundFor("findInFiles"), ["spelunk.open ctrl+shift+f/cmd+shift+f"]);
  assert.deepEqual(boundFor("none"), []);
});

test("settings are grouped in sections, each key under the section its name starts with, or moved there", () => {
  const sections = manifest.contributes.configuration;
  assert.deepEqual(
    sections.map((section) => section.title),
    ["Shortcut", "Search", "Results", "Opening results", "Indexing", "Display", "Watches", "Links"],
  );
  for (const section of sections) {
    const current = Object.entries(section.properties).filter(([, property]) => !property.deprecationMessage);
    const prefixes = new Set(current.map(([key]) => key.split(".").slice(0, 2).join(".")));
    assert.equal(prefixes.size, 1, `${section.title} mixes ${[...prefixes].join(", ")}`);
  }
});

test("the moved settings only the user may hold are the application-scoped ones", () => {
  const userOnly = Object.values(MOVED_SETTINGS).filter((key) => setting(`spelunk.${key}`)?.scope === "application");
  assert.deepEqual([...USER_ONLY_SETTINGS].sort(), userOnly.sort());
});

test("no setting offers a folder's value, since one value serves the whole window", () => {
  for (const section of manifest.contributes.configuration) {
    for (const [key, property] of Object.entries(section.properties)) {
      assert.notEqual(property.scope, "resource", `${key} is read once for the window`);
    }
  }
});

test("F4 and Shift+F4 step through Spelunk's results except where VS Code's own lists use them", () => {
  for (const command of ["spelunk.nextResult", "spelunk.prevResult"]) {
    const binding = manifest.contributes.keybindings.find((each) => each.command === command);
    const clauses = binding?.when?.split(" && ") ?? [];
    assert.deepEqual(clauses, [
      "spelunk.hasResults",
      "!searchViewletFocus", // the Search view
      "!inSearchEditor",
      "!referenceSearchVisible", // a references peek
      "!inReferenceSearchEditor",
      "focusedView != 'references-view.tree'", // the References view
    ]);
  }
});

test("the Command Palette lists Next and Previous Result only while there are results to step through", () => {
  // The result menu's commands are hidden there (when: false): see the next test.
  const palette = new Map(
    manifest.contributes.menus.commandPalette
      .filter((entry) => entry.when !== "false")
      .map((entry) => [entry.command, entry.when]),
  );
  assert.deepEqual(Object.fromEntries(palette), {
    "spelunk.nextResult": "spelunk.hasResults",
    "spelunk.prevResult": "spelunk.hasResults",
  });
  const declared = new Set(manifest.contributes.commands.map((entry) => entry.command));
  for (const command of palette.keys()) assert.ok(declared.has(command), `${command} is a declared command`);
});

test("the result menu's items are on results in the panel only, and kept out of the Command Palette", () => {
  const { commands, menus } = manifest.contributes;
  const declared = new Set(commands.map((entry) => entry.command));
  const hidden = new Set(menus.commandPalette.filter((entry) => entry.when === "false").map((entry) => entry.command));
  const items = [...menus["webview/context"], ...menus["spelunk.export"]];
  for (const item of menus["webview/context"]) {
    assert.match(item.when, /^webviewId == 'spelunk' && webviewSection == 'result'/u, JSON.stringify(item));
  }
  for (const { command } of items) {
    if (command === undefined) continue;
    assert.ok(declared.has(command), `${command} isn't declared`);
    assert.ok(hidden.has(command), `${command} shows in the Command Palette, where it has no result to act on`);
  }
  // Each kind of host has its own pair of items; the generic pair stands in for the rest.
  for (const host of ["github", "gitlab", "bitbucket", "gitea", "azure"]) {
    assert.ok(
      declared.has(`spelunk.result.copyPermalink.${host}`) && declared.has(`spelunk.result.openOnRemote.${host}`),
    );
  }
});

test("pressed in the editor, the search key tells Open to search the selection; elsewhere it doesn't", () => {
  // @covers command:open screen:seed-from-editor
  const opens = manifest.contributes.keybindings.filter((binding) => binding.command === "spelunk.open");
  const plain = opens.filter((binding) => binding.args === undefined).map((binding) => binding.when);
  const inEditor = opens.filter((binding) => binding.args !== undefined);
  assert.deepEqual(
    inEditor.map((binding) => [binding.when, binding.args]),
    plain.map((when) => [`${when} && editorTextFocus`, { fromEditor: true }]),
    "each preset's key has an editor-only twin, with the same key",
  );
  for (const binding of inEditor) {
    const twin = opens.find((other) => other.when === binding.when?.replace(" && editorTextFocus", ""));
    assert.equal(binding.key, twin?.key);
    assert.equal(binding.mac, twin?.mac);
    assert.ok(twin && opens.indexOf(binding) > opens.indexOf(twin), "listed after its twin, so it wins in the editor");
  }
});

test("Find Definitions and Find Uses are on the editor's right-click menu, at the top, and in the Command Palette", () => {
  // @covers command:findDefinitions command:findUses
  const ids = new Set(manifest.contributes.commands.map((command) => command.command));
  assert.ok(ids.has("spelunk.findDefinitions") && ids.has("spelunk.findUses"));
  assert.deepEqual(
    manifest.contributes.menus["editor/context"].map((item) => [item.command, item.group]),
    [
      ["spelunk.findDefinitions", "navigation@1.5"],
      ["spelunk.findUses", "navigation@1.6"],
    ],
  );
  const listed = new Set(manifest.contributes.menus.commandPalette.map((item) => item.command));
  assert.ok(!listed.has("spelunk.findDefinitions") && !listed.has("spelunk.findUses"), "no when hides them");
});

test("Search in Folder is on the Explorer's right-click menu for local folders, under Find in Folder, and not in the Command Palette", () => {
  // @covers command:searchInFolder screen:search-in-folder
  const command = manifest.contributes.commands.find((entry) => entry.command === "spelunk.searchInFolder");
  assert.equal(command?.title, "Search in Folder with Spelunk");
  assert.deepEqual(manifest.contributes.menus["explorer/context"], [
    {
      command: "spelunk.searchInFolder",
      when: "explorerResourceIsFolder && resourceScheme == file",
      group: "4_search@20",
    },
  ]);
  const palette = manifest.contributes.menus.commandPalette.find((item) => item.command === "spelunk.searchInFolder");
  assert.equal(palette?.when, "false", "it needs a folder, which only the Explorer gives");
});
