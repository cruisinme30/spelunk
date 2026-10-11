// What Ask tells the provider stays true to the query language: the
// reference names every operator the daemon's guide lists, and every worked
// example parses cleanly with the real daemon.
import assert from "node:assert/strict";
import { test } from "node:test";
import { EXAMPLES, OPERATOR_REFERENCE, systemPrompt } from "../askPrompt";
import { newTestDaemon, SKIP_WITHOUT_DAEMON as skip } from "./realDaemon";

test(
  "the prompt's reference covers every operator in the guide, and every example parses cleanly",
  { skip },
  async () => {
    // @covers rpc:query/guide
    const daemon = newTestDaemon([]);
    await daemon.start();
    try {
      const { text: guide } = await daemon.request("query/guide", {});
      const inGuide = [...guide.matchAll(/^ {2}([a-z]+): /gm)].map((match) => match[1]);
      assert.ok(inGuide.length > 20, `the guide lists the operators: ${inGuide.join(", ")}`);
      const referenced = OPERATOR_REFERENCE.map(([operator]) => operator);
      assert.deepEqual([...referenced].sort(), [...inGuide].sort(), "one reference paragraph per operator");
      for (const [operator, use] of OPERATOR_REFERENCE) assert.ok(use.startsWith(`${operator}:`), operator);
      const prompt = systemPrompt({ guide, repos: [], teams: [] });
      for (const [, use] of OPERATOR_REFERENCE) assert.ok(prompt.includes(use));
      assert.ok(!prompt.includes("{{REFERENCE}}"));
      for (const [question, text] of EXAMPLES) {
        const { query } = await daemon.request("query/parse", { text, cursor: text.length });
        assert.deepEqual(query.diagnostics, [], `${question} → ${text}`);
      }
    } finally {
      await daemon.stop();
    }
  },
);
