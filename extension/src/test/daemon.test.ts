// Supervision of the daemon: the real binary (built by `go build` into
// daemon/bin), and fake daemons (fakeDaemon.ts) that misbehave on purpose.
// @covers rpc:initialize failure:daemon-crash
import assert from "node:assert/strict";
import { spawn as nodeSpawn } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import type { Daemon, DaemonState } from "../daemon";
import { makeRoot } from "../roots";
import { newFakeDaemon, nonExecutableFile } from "./fakeDaemon";
import { newTestDaemon, SKIP_WITHOUT_DAEMON as skip, waitFor } from "./realDaemon";

const RESTART_BUDGET = 3;

/** A daemon over an empty folder that records its states and restarts quickly after a crash. */
function crashTestDaemon(states: DaemonState[] = [], restartDelayMs = 10): Daemon {
  const root = makeRoot(mkdtempSync(join(tmpdir(), "us-root-")));
  const daemon = newTestDaemon([root], { restartDelayMs, maxRestartsPerMinute: RESTART_BUDGET });
  daemon.on("state", (state) => states.push(state));
  return daemon;
}

/** The running daemon's process id; fails the test if there is none. */
function runningPid(daemon: Daemon): number {
  assert.ok(daemon.pid !== undefined, "the daemon is running");
  return daemon.pid;
}

test("start completes the handshake and reports the daemon version", { skip }, async () => {
  const daemon = crashTestDaemon();
  await daemon.start();
  assert.equal(daemon.state, "ok");
  assert.match(daemon.daemonVersion ?? "", /^\d+\.\d+\.\d+$/);
  await daemon.stop();
  assert.equal(daemon.state, "stopped");
});

test("crashes restart the daemon until the per-minute budget is spent", { skip }, async () => {
  const states: DaemonState[] = [];
  const daemon = crashTestDaemon(states);
  await daemon.start();
  for (let crash = 1; crash <= RESTART_BUDGET; crash++) {
    const pid = runningPid(daemon);
    process.kill(pid, "SIGKILL");
    await waitFor(`restart after crash ${crash}`, () => daemon.state === "ok" && daemon.pid !== pid);
  }
  assert.ok(states.includes("restarting"));
  process.kill(runningPid(daemon), "SIGKILL");
  await waitFor("stopped after one crash too many", () => daemon.state === "stopped");
  await daemon.restart();
  assert.equal(daemon.state, "ok", "a manual restart works after the budget is spent");
  await daemon.stop();
});

test("a manual restart cancels the restart scheduled after a crash", { skip }, async () => {
  const restartDelayMs = 300;
  const daemon = crashTestDaemon([], restartDelayMs);
  await daemon.start();
  process.kill(runningPid(daemon), "SIGKILL");
  await waitFor("the crash is noticed", () => daemon.state === "restarting");
  await daemon.restart();
  const pid = daemon.pid;
  await new Promise((resolve) => setTimeout(resolve, restartDelayMs * 2));
  assert.equal(daemon.pid, pid, "the scheduled restart must not replace the daemon the manual restart started");
  assert.equal(daemon.state, "ok");
  await daemon.stop();
});

/** A spawn that counts the processes it starts. */
function countingSpawn() {
  const counter = {
    spawns: 0,
    spawn: ((...args: Parameters<typeof nodeSpawn>) => {
      counter.spawns++;
      return nodeSpawn(...args);
    }) as typeof nodeSpawn,
  };
  return counter;
}

/** Whether the process `pid` is still running. */
function isRunning(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

for (const [what, binary] of [
  ["a missing binary", "/nonexistent/unified-search-daemon"],
  ["a binary that isn't executable", nonExecutableFile()],
] as const) {
  test(`${what} stops at once with the reason, without restart attempts`, async () => {
    const counter = countingSpawn();
    const { daemon, states } = newFakeDaemon("ok", { binary, args: [], spawn: counter.spawn });
    const messages: (string | undefined)[] = [];
    daemon.on("state", (_state, message) => messages.push(message));
    await assert.rejects(daemon.start());
    await pause(100);
    assert.equal(daemon.state, "stopped");
    assert.match(messages.at(-1) ?? "", /could not start: .*(ENOENT|EACCES)/);
    assert.equal(counter.spawns, 1);
    assert.deepEqual(states, ["starting", "stopped"]);
    await assert.rejects(daemon.request("index/status", {}), /stopped/, "requests fail at once, not after a wait");
  });
}

test("a daemon that never answers initialize is killed and restarted, not waited on forever", async () => {
  const counter = countingSpawn();
  const { daemon } = newFakeDaemon("silent", {
    initializeTimeoutMs: 100,
    maxRestartsPerMinute: 1,
    spawn: counter.spawn,
  });
  await assert.rejects(daemon.start(), /did not answer initialize/);
  await waitFor("stopped after the budget", () => daemon.state === "stopped");
  assert.equal(counter.spawns, 2);
  assert.equal(daemon.pid, undefined, "no hung process is left running");
});

test("a refused initialize stops the daemon with the reason instead of leaving it starting", async () => {
  const counter = countingSpawn();
  const { daemon } = newFakeDaemon("refuse", { spawn: counter.spawn });
  const messages: (string | undefined)[] = [];
  daemon.on("state", (_state, message) => messages.push(message));
  await assert.rejects(daemon.start(), /bad settings/);
  assert.equal(daemon.state, "stopped");
  assert.match(messages.at(-1) ?? "", /refused to start: invalid params: bad settings/);
  await pause(100);
  assert.equal(counter.spawns, 1, "the same settings would be refused again");
});

test("a garbled initialize answer counts as a crash, not as a daemon stuck starting", async () => {
  const { daemon } = newFakeDaemon("nullResult", { maxRestartsPerMinute: 0 });
  await assert.rejects(daemon.start());
  await waitFor("stopped", () => daemon.state === "stopped");
});

test("settings that can't be read stop the start before any process is spawned", async () => {
  const counter = countingSpawn();
  const { daemon } = newFakeDaemon("ok", {
    spawn: counter.spawn,
    settings: () => {
      throw new TypeError("path.startsWith is not a function");
    },
  });
  await assert.rejects(daemon.start(), /startsWith/);
  assert.equal(daemon.state, "stopped");
  assert.equal(counter.spawns, 0);
});

test("stop during the handshake never reports the daemon as ready", async () => {
  const { daemon, states } = newFakeDaemon("slowInitialize");
  const starting = assert.rejects(daemon.start());
  await pause(20);
  await daemon.stop();
  await starting;
  assert.deepEqual(states, ["starting", "stopped"]);
  await assert.rejects(daemon.request("index/status", {}), /stopped/);
});

test("restart during the handshake replaces the starting daemon", async () => {
  const { daemon } = newFakeDaemon("slowInitialize");
  const first = assert.rejects(daemon.start());
  await pause(20);
  const firstPid = runningPid(daemon);
  await daemon.restart();
  await first;
  assert.equal(daemon.state, "ok");
  assert.notEqual(daemon.pid, firstPid);
  await waitFor("the first process is gone", () => !isRunning(firstPid));
  await daemon.stop();
});
