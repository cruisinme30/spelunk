// Mapping unifiedSearch.* configuration to protocol types, and root ids.
import assert from "node:assert/strict";
import { test } from "node:test";
import { rootId } from "../roots";
import { daemonSettings, type ConfigReader } from "../settings";

function configWith(values: Record<string, unknown>): ConfigReader {
  return { get: <T>(key: string, fallback: T) => (key in values ? (values[key] as T) : fallback) };
}

test("root ids are the first 12 hex digits of sha256(path)", () => {
  assert.equal(rootId("/a"), "6a50dc858413");
});

test("daemon settings clamp counts, expand ~ and reject unknown history depths", () => {
  const settings = daemonSettings(
    configWith({ defaultCount: 999999, "index.location": "~/idx", "index.historyDepth": "bogus" }),
    "/home/u",
  );
  assert.equal(settings.defaultCount, 50000);
  assert.equal(settings.location, "/home/u/idx");
  assert.equal(settings.historyDepth, "2y");
});
