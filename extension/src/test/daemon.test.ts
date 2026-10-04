// Supervision of the real daemon binary (built by `go build` into daemon/bin).
// @covers rpc:initialize failure:daemon-crash
import assert from "node:assert/strict";
import { existsSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { Daemon, type DaemonState } from "../daemon";
import { makeRoot } from "../roots";
import { DEFAULTS } from "../settings";

const DAEMON_BINARY = process.env.UNIFIED_SEARCH_DAEMON ?? join(__dirname, "../../daemon/bin/unified-search-daemon");
const skip = !existsSync(DAEMON_BINARY) && "daemon not built";
const RESTART_BUDGET = 3;

function newDaemon(states: DaemonState[] = []): Daemon {
  const root = makeRoot(mkdtempSync(join(tmpdir(), "us-root-")));
  const daemon = new Daemon({
    binary: DAEMON_BINARY,
    roots: () => [root],
    settings: () => ({ ...DEFAULTS, exclude: [...DEFAULTS.exclude], location: mkdtempSync(join(tmpdir(), "us-idx-")) }),
    restartDelayMs: 10,
    maxRestartsPerMinute: RESTART_BUDGET,
  });
  daemon.on("state", (state) => states.push(state));
  return daemon;
}

async function waitFor(description: string, condition: () => boolean, timeoutMs = 5000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!condition()) {
    if (Date.now() > deadline) throw new Error(`timed out after ${timeoutMs}ms waiting for: ${description}`);
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

test("start completes the handshake and reports the daemon version", { skip }, async () => {
  const daemon = newDaemon();
  await daemon.start();
  assert.equal(daemon.state, "ok");
  assert.match(daemon.daemonVersion ?? "", /^\d+\.\d+\.\d+$/);
  await daemon.stop();
  assert.equal(daemon.state, "stopped");
});

test("crashes restart the daemon until the per-minute budget is spent", { skip }, async () => {
  const states: DaemonState[] = [];
  const daemon = newDaemon(states);
  await daemon.start();
  for (let crash = 1; crash <= RESTART_BUDGET; crash++) {
    const pid = daemon.pid!;
    process.kill(pid, "SIGKILL");
    await waitFor(`restart after crash ${crash}`, () => daemon.state === "ok" && daemon.pid !== pid);
  }
  assert.ok(states.includes("restarting"));
  process.kill(daemon.pid!, "SIGKILL");
  await waitFor("stopped after one crash too many", () => daemon.state === "stopped");
  await daemon.restart();
  assert.equal(daemon.state, "ok", "a manual restart works after the budget is spent");
  await daemon.stop();
});
