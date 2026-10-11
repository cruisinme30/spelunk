// An operator suggestion shows the daemon's title for the operator above the
// panel's one-line explanation of it (webview/src/operators.ts): the two
// lines must say different things.
// @covers screen:operator-suggestions
import assert from "node:assert/strict";
import { test } from "node:test";
import { fullName, OPERATOR_GROUPS } from "../../../webview/src/operators";
import { newTestDaemon, SKIP_WITHOUT_DAEMON as skip } from "./realDaemon";

/** The longest a title may be, in words: it names the operator, and the line below explains it. */
const MAX_TITLE_WORDS = 3;

test("each operator suggestion's title names the operator without repeating the line under it", { skip }, async () => {
  const daemon = newTestDaemon([]);
  await daemon.start();
  try {
    const entries = OPERATOR_GROUPS.flatMap((group) => group.entries).filter((entry) => entry.operator);
    assert.equal(entries.length, 21, "every operator");
    for (const entry of entries) {
      const name = fullName(entry.operator ?? "f");
      const { completions } = await daemon.request("query/parse", { text: name, cursor: name.length });
      const suggestion = completions.find((completion) => completion.label === `${name}:`);
      assert.ok(suggestion, `${name}: is suggested for "${name}"`);
      const title = suggestion.detail;
      assert.ok(title.split(/\s+/).length <= MAX_TITLE_WORDS, `${name}: title "${title}" is a name, not a sentence`);
      assert.ok(
        !entry.summary.toLowerCase().includes(title.toLowerCase()),
        `${name}: "${entry.summary}" repeats the title "${title}"`,
      );
    }
  } finally {
    await daemon.stop();
  }
});
