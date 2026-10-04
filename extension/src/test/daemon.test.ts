// Runs the real daemon binary (built by `go build` into daemon/bin).
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { Daemon, type DaemonState } from "../daemon";
import { makeRoot } from "../roots";
import { DEFAULTS } from "../settings";

const binary = process.env.UNIFIED_SEARCH_DAEMON ?? join(__dirname, "../../daemon/bin/unified-search-daemon");
const settings = () => ({ ...DEFAULTS, historyDepth: "2y" as const, location: mkdtempSync(join(tmpdir(), "us-idx-")) });

test(
  "handshake, crash restarts, and the 3-per-minute budget",
  { skip: !existsSync(binary) && "daemon not built" },
  async () => {
    const root = makeRoot(mkdtempSync(join(tmpdir(), "us-root-")));
    const states: DaemonState[] = [];
    const d = new Daemon({ binary, roots: () => [root], settings, restartDelayMs: 10 });
    d.on("state", (s) => states.push(s));
    await d.start();
    assert.equal(d.state, "ok");
    assert.ok(d.daemonVersion);

    for (let i = 0; i < 3; i++) {
      const pid = d.pid!;
      process.kill(pid, "SIGKILL");
      await waitFor(() => d.state === "ok" && d.pid !== pid);
    }
    process.kill(d.pid!, "SIGKILL");
    await waitFor(() => d.state === "stopped");
    assert.ok(states.includes("restarting"));

    await d.restart();
    assert.equal(d.state, "ok");
    await d.stop();
    assert.equal(d.state, "stopped");
  },
);

async function waitFor(cond: () => boolean, ms = 5000): Promise<void> {
  const end = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > end) throw new Error("timed out");
    await new Promise((r) => setTimeout(r, 10));
  }
}
