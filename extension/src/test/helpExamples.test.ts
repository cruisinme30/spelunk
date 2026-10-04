// Every example on the help page runs cleanly against the real daemon over
// the fixture workspace (test plan E17, mock 17).
// @covers mock:17 msg:help.try
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { allExamples } from "../../../webview/src/helpContent";
import { Daemon } from "../daemon";
import { makeRoot } from "../roots";
import { DEFAULTS } from "../settings";

const DAEMON_BINARY = process.env.UNIFIED_SEARCH_DAEMON ?? join(__dirname, "../../daemon/bin/unified-search-daemon");
const WORKSPACE = join(__dirname, "../../testdata/workspace");

/**
 * Examples that may find nothing yet: they need commit history (M3), the
 * symbol index (M4), or Git's change dates for since: on files (M4; until
 * then it uses file times, which depend on when the fixture was checked out).
 */
const NEEDS_LATER_MILESTONE = new Set([
  "since:2w timeout",
  "sym:RetryPolicy",
  "author:jane timeout",
  'msg:"fix flaky"',
  "author:jane since:30d f:_test\\.py$ timeout",
  'msg:"fix flaky" repo:web',
]);

test(
  "every help example parses without diagnostics and finds results",
  { skip: !existsSync(DAEMON_BINARY) && "daemon not built" },
  async () => {
    const roots = readdirSync(WORKSPACE, { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => makeRoot(join(WORKSPACE, entry.name)));
    const daemon = new Daemon({
      binary: DAEMON_BINARY,
      roots: () => roots,
      settings: () => ({
        ...DEFAULTS,
        exclude: [...DEFAULTS.exclude],
        location: mkdtempSync(join(tmpdir(), "us-idx-")),
      }),
    });
    await daemon.start();
    try {
      for (const deadline = Date.now() + 5000; ; ) {
        const { repos } = await daemon.request("index/status", {});
        if (repos.every((repo) => repo.tree === "ready")) break;
        assert.ok(Date.now() < deadline, "fixture workspace not indexed after 5 s");
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
      const examples = allExamples();
      assert.equal(examples.length, 21, "16 operator rows and 5 worked examples");
      const withoutResults: string[] = [];
      for (const [index, text] of examples.entries()) {
        const { query } = await daemon.request("query/parse", { text, cursor: text.length });
        assert.deepEqual(query.diagnostics, [], `${text}: diagnostics`);
        const result = await daemon.request("search/start", { searchId: `e${index}`, text });
        if (result.total === 0) withoutResults.push(text);
      }
      const unexpected = withoutResults.filter((text) => !NEEDS_LATER_MILESTONE.has(text));
      assert.deepEqual(unexpected, [], "examples that should find results but found none");
    } finally {
      await daemon.stop();
    }
  },
);
