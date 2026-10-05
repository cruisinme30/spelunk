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

test("a daemon that exits at once is restarted within the budget, then stays stopped", async () => {
  const counter = countingSpawn();
  const { daemon } = newFakeDaemon("exitAtOnce", { maxRestartsPerMinute: 2, spawn: counter.spawn });
  await assert.rejects(daemon.start(), /exited|closed/);
  await waitFor("stopped after the budget", () => daemon.state === "stopped");
  await pause(100);
  assert.equal(counter.spawns, 3, "the first start plus two restarts, and no more");
});

test("restarts back off, doubling the pause after each crash in a row up to a cap", async () => {
  // Crashes 25 s apart never spend the per-minute budget; without a backoff
  // such a daemon was restarted every 200 ms forever.
  let clock = 0;
  const lines: string[] = [];
  const { daemon } = newFakeDaemon("exitAtOnce", {
    restartDelayMs: 5,
    maxRestartDelayMs: 40,
    now: () => (clock += 25_000),
    log: (line) => lines.push(line),
  });
  await assert.rejects(daemon.start(), /exited|closed/);
  const delays = () => lines.flatMap((line) => /restarting in (\d+) ms/.exec(line)?.[1] ?? []).map(Number);
  await waitFor("six restarts", () => delays().length >= 6);
  await daemon.stop();
  assert.deepEqual(delays().slice(0, 6), [5, 10, 20, 40, 40, 40]);
});

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

test("stray output on stdout is skipped and the handshake still succeeds", async () => {
  const { daemon, log } = newFakeDaemon("garbage");
  await daemon.start();
  assert.equal(daemon.state, "ok");
  assert.deepEqual(await daemon.request("index/status", {}), { repos: [] });
  assert.ok(
    log.some((line) => line.startsWith("[rpc]")),
    "the garbage is logged",
  );
  await daemon.stop();
});

test("a flood of stderr is drained, so the daemon never blocks writing it", async () => {
  const { daemon, log } = newFakeDaemon("stderrFlood", { initializeTimeoutMs: 5000 });
  await daemon.start();
  assert.equal(daemon.state, "ok");
  assert.ok(log.join("").length >= 4 * 1024 * 1024);
  await daemon.stop();
});

test("stop called twice at once stops the daemon once, and both calls resolve", async () => {
  const { daemon, states } = newFakeDaemon("ok");
  await daemon.start();
  const pid = runningPid(daemon);
  await Promise.all([daemon.stop(), daemon.stop()]);
  assert.equal(daemon.state, "stopped");
  assert.equal(isRunning(pid), false);
  await daemon.stop();
  assert.deepEqual(states, ["starting", "ok", "stopped"]);
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

test("stop while a crash restart is scheduled cancels it", async () => {
  const counter = countingSpawn();
  const { daemon } = newFakeDaemon("ok", { restartDelayMs: 200, spawn: counter.spawn });
  await daemon.start();
  process.kill(runningPid(daemon), "SIGKILL");
  await waitFor("the crash is noticed", () => daemon.state === "restarting");
  await daemon.stop();
  await pause(300);
  assert.equal(daemon.state, "stopped");
  assert.equal(counter.spawns, 1);
});

test("stop kills a daemon that ignores shutdown, and resolves only once it has exited", async () => {
  const { daemon } = newFakeDaemon("ignoreShutdown", { shutdownGraceMs: 50 });
  await daemon.start();
  const pid = runningPid(daemon);
  await daemon.stop();
  assert.equal(daemon.state, "stopped");
  assert.equal(isRunning(pid), false);
});

test("a request in flight when the daemon dies rejects instead of hanging", async () => {
  const { daemon } = newFakeDaemon("exitOnRequest", { maxRestartsPerMinute: 0 });
  await daemon.start();
  await assert.rejects(daemon.request("index/status", {}), /exited|closed/);
  await waitFor("stopped", () => daemon.state === "stopped");
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
