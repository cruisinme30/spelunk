// Helpers for tests that run the real daemon binary: where it is, settings
// with a throwaway index, and waiting for it to finish indexing.
import { existsSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Daemon, type DaemonOptions } from "../daemon";
import type { Root, Settings } from "../protocol.gen";
import { daemonSettings } from "../settings";

/** The binary `npm test` builds into daemon/bin, or $UNIFIED_SEARCH_DAEMON. Tests run from extension/dist-test. */
const DAEMON_BINARY = process.env["UNIFIED_SEARCH_DAEMON"] ?? join(__dirname, "../../daemon/bin/unified-search-daemon");

/** The `skip` option of a test that needs the binary: why it is skipped, or false when the binary exists. */
export const SKIP_WITHOUT_DAEMON = !existsSync(DAEMON_BINARY) && "daemon not built";

/** The extension's default settings, with a fresh empty index directory each call (so each start). */
function testSettings(): Settings {
  const defaultsOnly = { get: <T>(_key: string, defaultValue: T) => defaultValue };
  return { ...daemonSettings(defaultsOnly, tmpdir()), location: mkdtempSync(join(tmpdir(), "us-idx-")) };
}

/** A daemon (not yet started) over `roots`, with test settings; `options` adds or overrides any option. */
export function newTestDaemon(roots: Root[], options: Partial<DaemonOptions> = {}): Daemon {
  return new Daemon({ binary: DAEMON_BINARY, roots: () => roots, settings: testSettings, ...options });
}

/** Polls `condition` every 10 ms until it holds; fails after `timeoutMs`, naming what it waited for. */
export async function waitFor(
  description: string,
  condition: () => boolean | Promise<boolean>,
  timeoutMs = 5000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error(`timed out after ${timeoutMs}ms waiting for: ${description}`);
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

/** Waits until the daemon has indexed every root's files, so searches see all of them. */
export async function untilIndexed(daemon: Daemon): Promise<void> {
  await waitFor("every root indexed", async () => {
    const { repos } = await daemon.request("index/status", {});
    return repos.every((repo) => repo.tree === "ready");
  });
}
