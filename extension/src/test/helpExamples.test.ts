// Every example on the help page runs cleanly against the real daemon over
// the fixture workspace.
// @covers screen:help-page
import assert from "node:assert/strict";
import { readdirSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { allExamples } from "../../../webview/src/helpContent";
import { makeRoot } from "../roots";
import { newTestDaemon, SKIP_WITHOUT_DAEMON as skip, untilIndexed } from "./realDaemon";

const WORKSPACE = join(__dirname, "../../testdata/workspace");

/**
 * Examples that may find nothing over the fixture: its repos have no .git,
 * so history search (author:, message:) has no commits, and since: and until: on files
 * falls back to file times, which depend on when the fixture was checked out.
 */
const MAY_FIND_NOTHING = new Set([
  "since:2w timeout",
  "until:2w timeout",
  "author:jane timeout",
  'message:"fix flaky"',
  String.raw`author:jane since:30d f:_test\.py$ timeout`,
  'msg:"fix flaky" repo:web',
]);

test("every help example parses without diagnostics and finds results", { skip }, async () => {
  const roots = readdirSync(WORKSPACE, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => makeRoot(join(WORKSPACE, entry.name)));
  const daemon = newTestDaemon(roots);
  await daemon.start();
  try {
    await untilIndexed(daemon);
    const examples = allExamples();
    assert.equal(examples.length, 28, "23 operator rows and 5 worked examples");
    const withoutResults: string[] = [];
    for (const [index, text] of examples.entries()) {
      const { query } = await daemon.request("query/parse", { text, cursor: text.length });
      assert.deepEqual(query.diagnostics, [], `${text}: diagnostics`);
      const result = await daemon.request("search/start", { searchId: `example${index}`, text });
      if (result.total === 0) withoutResults.push(text);
    }
    const unexpected = withoutResults.filter((text) => !MAY_FIND_NOTHING.has(text));
    assert.deepEqual(unexpected, [], "examples that should find results but found none");
  } finally {
    await daemon.stop();
  }
});
