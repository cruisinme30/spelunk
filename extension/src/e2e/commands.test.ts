// End-to-end tests in a real VS Code window, run by scripts/e2e.mjs: each
// command the extension contributes, on the fixture workspace. VS Code
// calls run() once the extension under test is loaded. A failed assertion
// rejects, and VS Code exits with an error.
import assert from "node:assert/strict";
import * as vscode from "vscode";
import type { TestState } from "../extension";

/** How long a step may take: the first search waits for the fixture workspace to be indexed. */
const TIMEOUT_MS = 30_000;
const POLL_MS = 100;

const sleep = (ms: number) =>
  new Promise<void>((resolve) => {
    setTimeout(resolve, ms);
  });

async function testState(): Promise<TestState> {
  const state = await vscode.commands.executeCommand<TestState | undefined>("unifiedSearch._testState");
  assert.ok(state, "unifiedSearch._testState is registered (UNIFIED_SEARCH_TEST=1)");
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
  await vscode.commands.executeCommand("unifiedSearch.open", { query });
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
      await vscode.commands.executeCommand("unifiedSearch.nextResult");
      const first = await until("the first result's editor", (state) => state.editor !== undefined);
      await vscode.commands.executeCommand("unifiedSearch.nextResult");
      const second = await until("the second result's editor", (state) => !sameEditor(state, first));
      await vscode.commands.executeCommand("unifiedSearch.prevResult");
      await until("the first result again", (state) => sameEditor(state, first));
      assert.notDeepEqual(second.editor, first.editor);
    },
  ],
  [
    "Open Help shows the search guide",
    async () => {
      // @covers command:openHelp
      await vscode.commands.executeCommand("unifiedSearch.openHelp");
      await untilTab("Unified Search · Help");
    },
  ],
  [
    "Show Welcome shows the welcome page",
    async () => {
      // @covers command:showWelcome
      await vscode.commands.executeCommand("unifiedSearch.showWelcome");
      await untilTab("Welcome · Unified Search");
    },
  ],
  [
    "Rebuild Index rebuilds every repo and search keeps working",
    async () => {
      // @covers command:rebuildIndex
      const picked = vscode.commands.executeCommand("unifiedSearch.rebuildIndex");
      await sleep(1000); // the repo picker opens; "All repos" is first
      await vscode.commands.executeCommand("workbench.action.acceptSelectedQuickOpenItem");
      await picked;
      await searchFor("retry");
    },
  ],
  [
    "Restart Search restarts the daemon and search keeps working",
    async () => {
      // @covers command:restartDaemon
      await vscode.commands.executeCommand("unifiedSearch.restartDaemon");
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
