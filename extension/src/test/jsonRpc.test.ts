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

test("a frame without Content-Length is skipped and the frames after it still decode", () => {
  const badHeaders: string[] = [];
  const decoder = new FrameDecoder((header) => badHeaders.push(header));
  const stream = Buffer.concat([encodeFrame("[1]"), Buffer.from("X-Bogus: 1\r\n\r\n"), encodeFrame("[2]")]);
  assert.deepEqual(decoder.push(stream), ["[1]", "[2]"]);
  assert.deepEqual(badHeaders, ["X-Bogus: 1"]);
});
