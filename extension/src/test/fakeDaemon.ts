// A stand-in daemon for supervision tests: a small Node script that speaks
// just enough JSON-RPC to misbehave in one chosen way.
import { chmodSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Daemon, type DaemonOptions } from "../daemon";
import { PROTOCOL_VERSION } from "../protocol.gen";
import { daemonSettings } from "../settings";

/**
 * How the fake daemon behaves:
 * - "ok" answers initialize and shutdown, and exits on exit;
 * - "exitAtOnce" exits with status 3 before reading anything;
 * - "silent" never answers anything;
 * - "refuse" answers initialize with an invalid-params error;
 * - "nullResult" answers initialize with a null result;
 * - "garbage" writes stray output and a body that isn't JSON before each answer;
 * - "stderrFlood" writes 4 MB to stderr, blocking until it is read, before answering initialize;
 * - "slowInitialize" answers initialize after 200 ms, and later messages only after that;
 * - "ignoreShutdown" answers initialize but never shutdown, and doesn't exit on exit;
 * - "exitOnRequest" exits as soon as a request other than initialize arrives.
 */
export type FakeBehaviour =
  | "ok"
  | "exitAtOnce"
  | "silent"
  | "refuse"
  | "nullResult"
  | "garbage"
  | "stderrFlood"
  | "slowInitialize"
  | "ignoreShutdown"
  | "exitOnRequest";

const SCRIPT = String.raw`
const { writeSync } = require("node:fs");
const behaviour = process.argv[2];
if (behaviour === "exitAtOnce") process.exit(3);
let buffered = Buffer.alloc(0);
let queue = Promise.resolve();
const send = (message) => {
  const body = Buffer.from(JSON.stringify({ jsonrpc: "2.0", ...message }));
  if (behaviour === "garbage") process.stdout.write("log: not a frame\r\n\r\nContent-Length: 5\r\n\r\n{oops");
  process.stdout.write(Buffer.concat([Buffer.from("Content-Length: " + body.length + "\r\n\r\n"), body]));
};
const handle = async ({ id, method }) => {
  if (behaviour === "silent") return;
  if (method === "initialize") {
    if (behaviour === "stderrFlood") writeSync(2, "x".repeat(4 * 1024 * 1024) + "\n");
    if (behaviour === "slowInitialize") await new Promise((resolve) => setTimeout(resolve, 200));
    if (behaviour === "refuse") return send({ id, error: { code: -32602, message: "invalid params: bad settings" } });
    if (behaviour === "nullResult") return send({ id, result: null });
    return send({ id, result: { protocol: ${PROTOCOL_VERSION}, daemonVersion: "0.0.0-fake" } });
  }
  if (behaviour === "exitOnRequest" && id !== undefined) process.exit(4);
  if (behaviour === "ignoreShutdown") return;
  if (method === "shutdown") return send({ id, result: null });
  if (method === "exit") process.exit(0);
  if (id !== undefined) send({ id, result: { repos: [] } });
};
process.stdin.on("data", (chunk) => {
  buffered = Buffer.concat([buffered, chunk]);
  for (;;) {
    const end = buffered.indexOf("\r\n\r\n");
    if (end === -1) return;
    const length = Number(/Content-Length: (\d+)/.exec(buffered.subarray(0, end).toString())[1]);
    if (buffered.length < end + 4 + length) return;
    const message = JSON.parse(buffered.subarray(end + 4, end + 4 + length).toString());
    buffered = buffered.subarray(end + 4 + length);
    queue = queue.then(() => handle(message));
  }
});
`;

let scriptPath: string | undefined;

/** The fake daemon script, written once per test run. */
function fakeDaemonScript(): string {
  if (!scriptPath) {
    scriptPath = join(mkdtempSync(join(tmpdir(), "us-fake-daemon-")), "fakeDaemon.js");
    writeFileSync(scriptPath, SCRIPT);
  }
  return scriptPath;
}

/** A file that exists but can't be executed. */
export function nonExecutableFile(): string {
  const path = join(mkdtempSync(join(tmpdir(), "us-not-exec-")), "spelunk-daemon");
  writeFileSync(path, "#!/bin/sh\nexit 0\n");
  chmodSync(path, 0o644);
  return path;
}

/** A fake daemon with the states it went through and the lines it logged, oldest first. */
interface FakeDaemon {
  daemon: Daemon;
  states: string[];
  log: string[];
}

/** A daemon (not yet started) running the fake script, which restarts quickly and records what happens. */
export function newFakeDaemon(behaviour: FakeBehaviour, options: Partial<DaemonOptions> = {}): FakeDaemon {
  const log: string[] = [];
  const defaultsOnly = { get: <T>(_key: string, defaultValue: T) => defaultValue };
  const daemon = new Daemon({
    binary: process.execPath,
    args: [fakeDaemonScript(), behaviour],
    roots: () => [],
    settings: () => daemonSettings(defaultsOnly, tmpdir()),
    restartDelayMs: 10,
    log: (line) => log.push(line),
    ...options,
  });
  const states: string[] = [];
  daemon.on("state", (state) => states.push(state));
  return { daemon, states, log };
}
