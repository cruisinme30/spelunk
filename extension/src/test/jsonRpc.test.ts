// Content-Length framing of the extension's JSON-RPC client, and how the
// connection copes with a peer that sends something unexpected or goes away.
import assert from "node:assert/strict";
import { PassThrough } from "node:stream";
import { test } from "node:test";
import { CancelSource, Connection, encodeFrame, FrameDecoder, RpcError } from "../jsonRpc";

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

/** A Connection over in-memory pipes: `peer` writes what the connection reads; `sent` collects what it wrote. */
function pipedConnection(listenForErrors = true) {
  const fromPeer = new PassThrough();
  const toPeer = new PassThrough();
  const connection = new Connection(fromPeer, toPeer);
  const errors: unknown[] = [];
  if (listenForErrors) connection.on("error", (error) => errors.push(error));
  const sent: Record<string, unknown>[] = [];
  const decoder = new FrameDecoder();
  toPeer.on("data", (chunk: Buffer) => {
    for (const body of decoder.push(chunk)) sent.push(JSON.parse(body) as Record<string, unknown>);
  });
  const peer = (...bodies: (string | object)[]) =>
    fromPeer.write(
      Buffer.concat(bodies.map((body) => encodeFrame(typeof body === "string" ? body : JSON.stringify(body)))),
    );
  return { connection, fromPeer, toPeer, peer, errors, sent };
}

const indexStatus = (connection: Connection, cancel?: CancelSource) =>
  connection.request("index/status", {}, cancel?.token);

/** Whether `promise` is still pending after the current I/O callbacks ran. */
async function isPending(promise: Promise<unknown>): Promise<boolean> {
  const pending = Symbol("pending");
  const outcome = await Promise.race([
    promise.catch(() => {}),
    new Promise((resolve) =>
      setImmediate(() => {
        resolve(pending);
      }),
    ),
  ]);
  return outcome === pending;
}

test("answers for unknown ids are ignored and the right answer still resolves its request", async () => {
  const { connection, peer } = pipedConnection();
  const status = indexStatus(connection);
  peer({ jsonrpc: "2.0", id: 999, result: { repos: ["wrong"] } }, { jsonrpc: "2.0", id: "1", result: {} });
  peer({ jsonrpc: "2.0", id: 1, result: { repos: [] } });
  assert.deepEqual(await status, { repos: [] });
});

test("errors with missing or mistyped fields still reject as an RpcError", async () => {
  const { connection, peer } = pipedConnection();
  const answers = [{}, { code: "x", message: 5 }, "boom", { code: 7, message: "bad", data: { why: 1 } }];
  const requests = answers.map(() => indexStatus(connection));
  peer(...answers.map((error, index) => ({ jsonrpc: "2.0", id: index + 1, error })));
  const rejected = await Promise.all(
    requests.map((request) =>
      request.then(
        () => assert.fail("resolved"),
        (error: unknown) => {
          assert.ok(error instanceof RpcError);
          return error;
        },
      ),
    ),
  );
  assert.deepEqual(
    rejected.map((error) => [error.code, error.message]),
    [
      [-32_603, "request failed"],
      [-32_603, "request failed"],
      [-32_603, "boom"],
      [7, "bad"],
    ],
  );
  assert.deepEqual(rejected[3]?.data, { why: 1 });
});

test("a body that is not a message object is reported, and the frames after it in the chunk still arrive", async () => {
  const { connection, peer, errors } = pipedConnection();
  const status = indexStatus(connection);
  peer("null", "[]", '"text"', "{not json", { jsonrpc: "2.0", id: 1, result: { repos: [] } });
  assert.deepEqual(await status, { repos: [] });
  assert.equal(errors.length, 4);
});

test("a notification listener that throws does not lose the answer behind it", async () => {
  const { connection, peer, errors } = pipedConnection();
  connection.onNotification("search/batch", (batch) => {
    assert.ok(batch.items.length >= 0); // throws: items is missing below
  });
  const status = indexStatus(connection);
  peer({ jsonrpc: "2.0", method: "search/batch", params: {} }, { jsonrpc: "2.0", id: 1, result: { repos: [] } });
  assert.deepEqual(await status, { repos: [] });
  assert.equal(errors.length, 1);
});

test("with no error listener, garbage from the peer is dropped instead of thrown", async () => {
  const { connection, peer, fromPeer } = pipedConnection(false);
  const status = indexStatus(connection);
  peer("null", "{not json");
  fromPeer.write("X-Bogus: 1\r\n\r\n");
  peer({ jsonrpc: "2.0", id: 1, result: { repos: [] } });
  assert.deepEqual(await status, { repos: [] });
});

test("the stream ending mid-frame rejects every pending request", async () => {
  const { connection, fromPeer } = pipedConnection();
  const requests = [indexStatus(connection), indexStatus(connection)];
  fromPeer.end(encodeFrame('{"jsonrpc":"2.0","id":1,"result":{}}').subarray(0, 20));
  for (const request of requests) await assert.rejects(request, /connection closed/);
  await assert.rejects(indexStatus(connection), /connection closed/, "requests after close fail at once");
});

test("cancelling sends $/cancelRequest only while the request is pending", async () => {
  const { connection, peer, sent } = pipedConnection();
  const answered = new CancelSource();
  const status = indexStatus(connection, answered);
  peer({ jsonrpc: "2.0", id: 1, result: { repos: [] } });
  await status;
  answered.cancel();
  const waiting = new CancelSource();
  const pending = indexStatus(connection, waiting);
  waiting.cancel();
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(
    sent.map((message) => message["method"]),
    ["index/status", "index/status", "$/cancelRequest"],
  );
  assert.ok(await isPending(pending), "the request waits for the peer's answer to the cancel");
  connection.dispose();
  await assert.rejects(pending, /connection disposed/);
});
