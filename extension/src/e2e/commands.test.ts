// End-to-end tests in a real VS Code window, run by scripts/e2e.mjs: each
// command the extension contributes, on the fixture workspace. VS Code
// calls run() once the extension under test is loaded. A failed assertion
// rejects, and VS Code exits with an error.
import assert from "node:assert/strict";
import * as vscode from "vscode";
import type { TestState } from "../extension";
import { MESSAGE_VERSION } from "../webviewMessages";
import { setTimeout as sleep } from "node:timers/promises";

/** How long a step may take: the first search waits for the fixture workspace to be indexed. */
const TIMEOUT_MS = 30_000;
const POLL_MS = 100;

async function testState(): Promise<TestState> {
  const state = await vscode.commands.executeCommand<TestState | undefined>("spelunk._testState");
  assert.ok(state, "spelunk._testState is registered (SPELUNK_TEST=1)");
  return state;
}

/** Reads a value until `done` holds for it, failing after TIMEOUT_MS. */
async function poll<T>(what: string, read: () => T | Promise<T>, done: (value: T) => boolean): Promise<T> {
  const deadline = Date.now() + TIMEOUT_MS;
  for (;;) {
    const value = await read();
    if (done(value)) return value;
    if (Date.now() > deadline) assert.fail(`timed out waiting for ${what}: ${JSON.stringify(value)}`);
    await sleep(POLL_MS);
  }
}

/** Polls the extension's state until `done` holds. */
const until = (what: string, done: (state: TestState) => boolean) => poll(what, testState, done);

/** Whether an editor tab with this label is open. */
function hasTab(label: string): boolean {
  return vscode.window.tabGroups.all.some((group) => group.tabs.some((tab) => tab.label === label));
}

/** Waits until an editor tab with this label is open. */
async function untilTab(label: string): Promise<void> {
  await poll(`a tab named ${label}`, () => hasTab(label), Boolean);
}

/** Opens the panel on `query` and waits for its results. */
async function searchFor(query: string): Promise<TestState> {
  await vscode.commands.executeCommand("spelunk.open", { query });
  return until(
    `results for ${query}`,
    (state) => state.panelOpen && state.searched === query && state.results.length > 0,
  );
}

const TESTS: [name: string, run: () => Promise<void>][] = [
  [
    "Open runs a query in the search panel",
    async () => {
      // @covers command:open
      const state = await searchFor("retry_policy");
      assert.equal(state.daemon, "ok");
    },
  ],
  [
    "Next and Previous Result open the results in turn",
    async () => {
      // @covers command:nextResult command:prevResult
      await searchFor("type:code timeout");
      await vscode.commands.executeCommand("spelunk.nextResult");
      const first = await until("the first result's editor", (state) => state.editor !== undefined);
      await vscode.commands.executeCommand("spelunk.nextResult");
      const second = await until("the second result's editor", (state) => !sameEditor(state, first));
      await vscode.commands.executeCommand("spelunk.prevResult");
      await until("the first result again", (state) => sameEditor(state, first));
      assert.notDeepEqual(second.editor, first.editor);
    },
  ],
  [
    "Opening a match across lines selects all of it",
    async () => {
      // @covers screen:multiline-regex
      // client.py lines 46-47: "for attempt in range(self.retry_policy.max_attempts):" then "try:".
      await searchFor(String.raw`type:code /max_attempts\):\n\s*try:/`);
      await vscode.commands.executeCommand("spelunk.nextResult");
      const { editor } = await until("the match's editor", (state) => state.editor?.endLine === 47);
      assert.ok(editor, "an editor is open");
      assert.ok(editor.path.endsWith("client.py"), "client.py is open");
      assert.deepEqual(
        { line: editor.line, column: editor.column, endLine: editor.endLine, endColumn: editor.endColumn },
        { line: 46, column: 48, endLine: 47, endColumn: 17 },
      );
    },
  ],
  [
    "Opening a struct: match selects all of it",
    async () => {
      // @covers screen:structural-search
      // webhooks.py lines 7-9: "return backoff(", the call's lambda, then "tries=event.max_tries)".
      await searchFor('struct:"backoff(:[call], tries=:[n])"');
      await vscode.commands.executeCommand("spelunk.nextResult");
      const { editor } = await until("the match's editor", (state) => state.editor?.endLine === 9);
      assert.ok(editor, "an editor is open");
      assert.ok(editor.path.endsWith("webhooks.py"), "webhooks.py is open");
      assert.deepEqual(
        { line: editor.line, column: editor.column, endLine: editor.endLine, endColumn: editor.endColumn },
        { line: 7, column: 12, endLine: 9, endColumn: 31 },
      );
    },
  ],
  [
    "Open Help shows the search guide",
    async () => {
      // @covers command:openHelp
      await vscode.commands.executeCommand("spelunk.openHelp");
      await untilTab("Spelunk · Help");
    },
  ],
  [
    "Show Welcome shows the welcome page",
    async () => {
      // @covers command:showWelcome
      await vscode.commands.executeCommand("spelunk.showWelcome");
      await untilTab("Welcome · Spelunk");
    },
  ],
  [
    "Rebuild Index rebuilds every repo and search keeps working",
    async () => {
      // @covers command:rebuildIndex
      const picked = vscode.commands.executeCommand("spelunk.rebuildIndex");
      await sleep(1000); // the repo picker opens; "All repos" is first
      await vscode.commands.executeCommand("workbench.action.acceptSelectedQuickOpenItem");
      await picked;
      await searchFor("retry");
    },
  ],
  [
    "The panel's Context buttons save the setting, and the results gain context lines",
    async () => {
      // @covers setting:results.contextLines msg:context.set
      const config = () => vscode.workspace.getConfiguration("spelunk");
      const before = await searchFor("type:code timeout");
      assert.equal(before.withContext, 0, "no context lines by default");
      await vscode.commands.executeCommand("spelunk._testPanelMessage", {
        v: MESSAGE_VERSION,
        type: "context.set",
        payload: { lines: 2 },
      });
      await poll(
        "the setting to be saved",
        () => config().get("results.contextLines"),
        (lines) => lines === 2,
      );
      await until("results with context lines", (state) => state.withContext > 0);
      await config().update("results.contextLines", undefined, vscode.ConfigurationTarget.Global);
      await until("results without them again", (state) => state.results.length > 0 && state.withContext === 0);
    },
  ],
  [
    "The panel's bell watches a pinned history query in the setting, and only a history query",
    async () => {
      // @covers msg:pinned.watch setting:ui.pinnedQueries
      const config = () => vscode.workspace.getConfiguration("spelunk");
      const global = vscode.ConfigurationTarget.Global;
      await config().update("ui.pinnedQueries", [{ query: "retry_policy" }, { query: "type:commit timeout" }], global);
      for (const query of ["retry_policy", "type:commit timeout"]) {
        await vscode.commands.executeCommand("spelunk._testPanelMessage", {
          v: MESSAGE_VERSION,
          type: "pinned.watch",
          payload: { query, watch: true },
        });
      }
      const saved = await poll(
        "the watch to be saved",
        () => config().get<{ query: string; watch?: boolean }[]>("ui.pinnedQueries") ?? [],
        (pinned) => pinned.some((entry) => entry.watch),
      );
      assert.deepEqual(saved, [{ query: "retry_policy" }, { query: "type:commit timeout", watch: true }]);
      await config().update("ui.pinnedQueries", undefined, global);
    },
  ],
  [
    "Restart Search restarts the daemon and search keeps working",
    async () => {
      // @covers command:restartDaemon
      await vscode.commands.executeCommand("spelunk.restartDaemon");
      await until("the daemon to be back", (state) => state.daemon === "ok");
      await searchFor("RetryPolicy");
    },
  ],
];

function sameEditor(state: TestState, other: TestState): boolean {
  return JSON.stringify(state.editor) === JSON.stringify(other.editor);
}

/** Runs every test in order, reports each, and rejects if any failed. */
export async function run(): Promise<void> {
  const failed: string[] = [];
  for (const [name, test] of TESTS) {
    try {
      await test();
      process.stdout.write(`ok - ${name}\n`);
    } catch (error) {
      failed.push(name);
      process.stderr.write(`not ok - ${name}\n${String(error)}\n`);
    }
  }
  if (failed.length > 0) throw new Error(`${failed.length} end-to-end test(s) failed: ${failed.join("; ")}`);
}
