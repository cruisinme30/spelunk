// Supervision of the real daemon binary (built by `go build` into daemon/bin).
// @covers rpc:initialize failure:daemon-crash
import assert from "node:assert/strict";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import type { Daemon, DaemonState } from "../daemon";
import { makeRoot } from "../roots";
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
