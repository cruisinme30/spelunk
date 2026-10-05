// Mapping spelunk.* configuration to protocol types, and root ids.
import assert from "node:assert/strict";
import { test } from "node:test";
import { rootId } from "../roots";
import {
  closeOnOpen,
  daemonSettings,
  migratedCaseSetting,
  recentQueriesLimit,
  uiSettings,
  welcomeSettings,
  type ConfigReader,
} from "../settings";

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

test("the order setting reaches both the daemon and the panel", () => {
  const config = configWith({ order: "path" });
  assert.equal(daemonSettings(config, "/home/u").order, "path");
  assert.equal(uiSettings(config).order, "path");
});

/** The index location the daemon gets for an index.location setting, with /home/u as home. */
const location = (value: string) => daemonSettings(configWith({ "index.location": value }), "/home/u").location;

test("the index location expands ~ and ~/ but not ~user", () => {
  assert.equal(location("~/idx"), "/home/u/idx");
  assert.equal(location("~"), "/home/u");
  assert.equal(location("~alice/idx"), "~alice/idx");
});

test("caseSensitive is off, on or smart, and reads the boolean it used to be", () => {
  // @covers setting:caseSensitive
  for (const value of ["off", "on", "smart"]) {
    assert.equal(daemonSettings(configWith({ caseSensitive: value }), "/h").caseSensitive, value);
    assert.equal(uiSettings(configWith({ caseSensitive: value })).caseSensitive, value);
  }
  assert.equal(daemonSettings(configWith({ caseSensitive: true }), "/h").caseSensitive, "on");
  assert.equal(uiSettings(configWith({ caseSensitive: false })).caseSensitive, "off");
  assert.equal(migratedCaseSetting(true), "on");
  assert.equal(migratedCaseSetting(false), "off");
  assert.equal(migratedCaseSetting("smart"), undefined, "already migrated");
  assert.equal(migratedCaseSetting(null), undefined, "not a boolean");
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

test("settings of the wrong type or out of range fall back to defaults the daemon accepts", () => {
  // @covers setting:defaultCount setting:index.maxFileSizeKB setting:index.exclude setting:caseSensitive setting:wholeWord
  const wrong = configWith({
    caseSensitive: "yes",
    wholeWord: 1,
    defaultCount: "lots",
    "index.symbols": 1,
    "index.exclude": "**/dist/**",
    "index.includeIgnored": null,
    "index.maxFileSizeKB": 1.5,
    "index.location": 42,
    "index.historyDepth": ["all"],
    order: "random",
  });
  assert.deepEqual(daemonSettings(wrong, "/home/u"), {
    caseSensitive: "off",
    wholeWord: false,
    defaultCount: 500,
    historyDepth: "2y",
    symbols: true,
    exclude: ["**/vendor/**", "**/node_modules/**", "**/*.min.js"],
    includeIgnored: false,
    maxFileSizeKB: 2,
    location: "/home/u/.spelunk/index",
    order: "best",
  });
  const extremes = daemonSettings(
    configWith({
      defaultCount: -5,
      "index.maxFileSizeKB": 1e300,
      "index.exclude": ["a", 3, null, "b"],
      "index.location": "",
    }),
    "/home/u",
  );
  assert.equal(extremes.defaultCount, 1);
  assert.equal(extremes.maxFileSizeKB, 1024 * 1024, "kept inside the daemon's int");
  assert.deepEqual(extremes.exclude, ["a", "b"]);
  assert.equal(extremes.location, "/home/u/.spelunk/index", "an empty location means the default");
  assert.equal(JSON.stringify(daemonSettings(configWith({ defaultCount: Number.NaN }), "/h")).includes("null"), false);
});

test("panel settings of the wrong type fall back to their defaults", () => {
  // @covers setting:typingDelayMs setting:open.trigger setting:ui.recentQueries
  const wrong = configWith({
    typingDelayMs: "fast",
    "open.trigger": 2,
    "open.preview": "no",
    "ui.showParsedQuery": 0,
    caseSensitive: "true",
    wholeWord: "true",
    "ui.recentQueries": "many",
    "open.closeOnOpen": "false",
    order: 1,
  });
  assert.deepEqual(uiSettings(wrong), {
    typingDelayMs: 120,
    openTrigger: "doubleClick",
    preview: true,
    showParsedQuery: true,
    caseSensitive: "off",
    wholeWord: false,
    order: "best",
  });
  assert.equal(recentQueriesLimit(wrong), 20, "not NaN, which would empty the recent list");
  assert.equal(closeOnOpen(wrong), true);
  assert.equal(recentQueriesLimit(configWith({ "ui.recentQueries": -3 })), 0);
  assert.equal(recentQueriesLimit(configWith({ "ui.recentQueries": 2.6 })), 3);
  assert.equal(uiSettings(configWith({ typingDelayMs: Number.POSITIVE_INFINITY })).typingDelayMs, 120);
});
