// The extension manifest: the keys each shortcut preset binds, and how settings are grouped.
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
}

interface Manifest {
  contributes: {
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
    ["Shortcut", "Search", "Results", "Opening results", "Indexing", "Display", "Watches"],
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
