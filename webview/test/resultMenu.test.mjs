// The result menu (board 53): VS Code draws it from what each result row
// says in data-vscode-context — the result's ref and kind, and which kind
// of Git host its repo is on, which names the link items.
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CODE_LINE_RESULT,
  FILE_NAME_RESULT,
  fromHost,
  lastSent,
  pageErrors,
  panelWithResults,
  useBrowser,
} from "./harness.mjs";

useBrowser();

/** Each result row's menu context, in order. */
async function contexts(page) {
  const rows = page.locator('[data-testid="result"]');
  return (await rows.evaluateAll((all) => all.map((row) => row.dataset["vscodeContext"]))).map((text) =>
    JSON.parse(text ?? "null"),
  );
}

test("each result row tells VS Code's menu which result it is, leaving out Cut, Copy and Paste", async (t) => {
  // @covers screen:share-result
  const page = await panelWithResults(t);
  assert.deepEqual(await contexts(page), [
    { webviewSection: "result", ref: FILE_NAME_RESULT.ref, resultKind: "file", preventDefaultContextMenuItems: true },
    { webviewSection: "result", ref: CODE_LINE_RESULT.ref, resultKind: "line", preventDefaultContextMenuItems: true },
  ]);
  // The query box keeps VS Code's own menu, to cut, copy and paste its text.
  assert.equal(await page.locator('[data-testid="query"]').getAttribute("data-vscode-context"), null);
  assert.deepEqual(pageErrors(page), []);
});

test("remotes.state names each repo's host on its rows, those shown and those still to come", async (t) => {
  // @covers msg:remotes.state screen:share-result
  const page = await panelWithResults(t);
  await fromHost(page, "remotes.state", { repos: [{ repoId: "r1", host: "github" }] });
  assert.deepEqual(
    (await contexts(page)).map((context) => context.remoteHost),
    ["github", "github"],
  );
  const { seq } = await lastSent(page, "query.changed");
  await fromHost(page, "search.batch", {
    seq,
    searchId: "s1",
    items: [{ ...CODE_LINE_RESULT, ref: "l2", repoId: "r2", line: 50 }],
  });
  await fromHost(page, "remotes.state", {
    repos: [
      { repoId: "r1", host: "gitlab" },
      { repoId: "r2", host: "none" },
    ],
  });
  assert.deepEqual(
    (await contexts(page)).map((context) => context.remoteHost),
    ["gitlab", "gitlab", "none"],
  );
  // A malformed message is dropped whole.
  await fromHost(page, "remotes.state", { repos: [{ repoId: 3 }] });
  assert.deepEqual(
    (await contexts(page)).map((context) => context.remoteHost),
    ["gitlab", "gitlab", "none"],
  );
  assert.deepEqual(pageErrors(page), []);
});
