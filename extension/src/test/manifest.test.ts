// The extension manifest: the keys each shortcut preset binds.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

interface Keybinding {
  command: string;
  key: string;
  mac?: string;
  when?: string;
}

interface Manifest {
  contributes: {
    keybindings: Keybinding[];
    configuration: { properties: Record<string, { enum?: string[] }> };
  };
}

const manifest = JSON.parse(readFileSync(join(__dirname, "../package.json"), "utf8")) as Manifest;

/** The "command key/mac" pairs bound while a preset is chosen. */
function boundFor(preset: string): string[] {
  return manifest.contributes.keybindings
    .filter((binding) => binding.when === `config.spelunk.shortcut.preset == '${preset}'`)
    .map((binding) => `${binding.command} ${binding.key}/${binding.mac ?? binding.key}`);
}

test("each shortcut preset binds its keys, and none binds nothing", () => {
  // @covers setting:shortcut.preset
  const presets = manifest.contributes.configuration.properties["spelunk.shortcut.preset"]?.enum;
  assert.deepEqual(presets, ["quickOpen", "findInFiles", "none"]);
  assert.deepEqual(boundFor("quickOpen"), [
    "spelunk.open ctrl+p/cmd+p",
    "workbench.action.quickOpen ctrl+alt+p/cmd+alt+p",
  ]);
  assert.deepEqual(boundFor("findInFiles"), ["spelunk.open ctrl+shift+f/cmd+shift+f"]);
  assert.deepEqual(boundFor("none"), []);
});
