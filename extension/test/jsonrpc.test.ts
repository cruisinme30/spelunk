import { test } from "node:test";
import assert from "node:assert/strict";
import { FrameDecoder, encodeFrame } from "../src/jsonrpc";
import { rootId } from "../src/roots";
import { daemonSettings } from "../src/settings";

test("frames split across chunks are reassembled", () => {
  const d = new FrameDecoder();
  const a = encodeFrame('{"x":"é"}');
  const b = encodeFrame("{}");
  const all = Buffer.concat([a, b]);
  assert.deepEqual(d.push(all.subarray(0, 5)), []);
  assert.deepEqual(d.push(all.subarray(5, a.length + 3)), ['{"x":"é"}']);
  assert.deepEqual(d.push(all.subarray(a.length + 3)), ["{}"]);
});

test("root ids are the first 12 hex of sha256(path)", () => {
  assert.equal(rootId("/a"), "6a50dc858413");
  assert.match(rootId("/a"), /^[0-9a-f]{12}$/);
});

test("settings map and clamp", () => {
  const vals: Record<string, unknown> = { defaultCount: 999999, "index.location": "~/idx", "index.historyDepth": "bogus" };
  const s = daemonSettings({ get: <T,>(k: string, d: T) => (k in vals ? (vals[k] as T) : d) }, "/home/u");
  assert.equal(s.defaultCount, 50000);
  assert.equal(s.location, "/home/u/idx");
  assert.equal(s.historyDepth, "2y");
});
