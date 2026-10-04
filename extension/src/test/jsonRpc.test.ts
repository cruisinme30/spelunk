// Content-Length framing of the extension's JSON-RPC client.
import assert from "node:assert/strict";
import { test } from "node:test";
import { encodeFrame, FrameDecoder } from "../jsonRpc";

test("frames split across chunks are reassembled in order", () => {
  const decoder = new FrameDecoder();
  const first = encodeFrame('{"x":"é"}');
  const second = encodeFrame("{}");
  const stream = Buffer.concat([first, second]);
  assert.deepEqual(decoder.push(stream.subarray(0, 5)), []);
  assert.deepEqual(decoder.push(stream.subarray(5, first.length + 3)), ['{"x":"é"}']);
  assert.deepEqual(decoder.push(stream.subarray(first.length + 3)), ["{}"]);
});

test("a frame without Content-Length is dropped and later frames still decode", () => {
  const decoder = new FrameDecoder();
  assert.throws(() => decoder.push(Buffer.from("X-Bogus: 1\r\n\r\n")), /Content-Length/);
  assert.deepEqual(decoder.push(encodeFrame("{}")), ["{}"]);
});
