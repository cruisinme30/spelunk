// Mapping unifiedSearch.* configuration to protocol types, and root ids.
import assert from "node:assert/strict";
import { test } from "node:test";
import { rootId } from "../roots";
import { daemonSettings, welcomeSettings, type ConfigReader } from "../settings";

function configWith(values: Record<string, unknown>): ConfigReader {
  return { get: <T>(key: string, fallback: T) => (key in values ? (values[key] as T) : fallback) };
}

test("root ids are the first 12 hex digits of sha256(path)", () => {
  assert.equal(rootId("/a"), "6a50dc858413");
});

test("daemon settings clamp counts and reject unknown history depths", () => {
  const settings = daemonSettings(configWith({ defaultCount: 999_999, "index.historyDepth": "bogus" }), "/home/u");
  assert.equal(settings.defaultCount, 50_000);
  assert.equal(settings.historyDepth, "2y");
});

/** The index location the daemon gets for an index.location setting, with /home/u as home. */
const location = (value: string) => daemonSettings(configWith({ "index.location": value }), "/home/u").location;

test("the index location expands ~ and ~/ but not ~user", () => {
  assert.equal(location("~/idx"), "/home/u/idx");
  assert.equal(location("~"), "/home/u");
  assert.equal(location("~alice/idx"), "~alice/idx");
});

test("the welcome page shows the saved preset and index choices, and an unknown preset as the default", () => {
  const config = configWith({ "shortcut.preset": "findInFiles", "index.historyDepth": "6m", "index.symbols": false });
  assert.deepEqual(welcomeSettings(config, true), {
    preset: "findInFiles",
    historyDepth: "6m",
    symbols: false,
    mac: true,
  });
  assert.equal(welcomeSettings(configWith({ "shortcut.preset": "bogus" }), false).preset, "quickOpen");
});
