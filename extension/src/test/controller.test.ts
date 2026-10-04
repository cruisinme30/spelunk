// Host side of Contract 2. The round-trip test drives the real daemon binary;
// the others use a scripted backend to pin down ordering rules.
// @covers msg:query.changed msg:parse.result msg:search.batch msg:search.done msg:result.select msg:preview.result msg:result.open msg:panel.close
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { type Backend, SearchController, textWithoutCompletedWord, type Ui, type WebviewMessage } from "../controller";
import { Daemon } from "../daemon";
import type { HostToWebview, OpenTarget, ParsedQuery, UiSettings } from "../protocol.gen";
import { makeRoot } from "../roots";
import { DEFAULTS } from "../settings";

const DAEMON_BINARY = process.env.UNIFIED_SEARCH_DAEMON ?? join(__dirname, "../../daemon/bin/unified-search-daemon");

const TEST_UI_SETTINGS: UiSettings = {
  typingDelayMs: 0,
  openTrigger: "doubleClick",
  preview: true,
  showParsedQuery: true,
  caseSensitive: false,
};

interface Posted {
  type: keyof HostToWebview;
  payload: any;
}

/** A Ui that records everything the controller does. */
function recordingUi() {
  const posted: Posted[] = [];
  const opened: { target: OpenTarget; where: string }[] = [];
  const contextKeys: Record<string, boolean> = {};
  let closedPanels = 0;
  const ui: Ui = {
    post: (type, payload) => void posted.push({ type, payload }),
    openTarget: async (target, where) => void opened.push({ target, where }),
    hidePanel: () => void closedPanels++,
    openHelp: () => undefined,
    openSettings: () => undefined,
    restartDaemon: () => undefined,
    setContext: (key, value) => void (contextKeys[key] = value),
    saveState: () => undefined,
  };
  return { ui, posted, opened, contextKeys, closedPanels: () => closedPanels };
}

function newController(backend: Backend, ui: Ui, recentLimit = 20, closeOnOpen = true) {
  return new SearchController(backend, ui, {
    recentLimit: () => recentLimit,
    closeOnOpen: () => closeOnOpen,
    uiSettings: () => TEST_UI_SETTINGS,
  });
}

const emptyQuery = (raw: string): ParsedQuery => ({
  version: 1,
  raw,
  root: null,
  globals: { case: null, count: null, type: null },
  mode: "workingTree",
  diagnostics: [],
});

/** A backend whose query/parse answers immediately, except for texts in `slow`, which wait for release(). */
function scriptedBackend(slow = new Set<string>()) {
  const waiting: (() => void)[] = [];
  const backend: Backend = {
    on: () => undefined,
    request: async (method, params: any): Promise<any> => {
      if (method !== "query/parse") throw new Error(`unexpected request ${method}`);
      if (slow.has(params.text)) await new Promise<void>((resolve) => waiting.push(resolve));
      return { query: emptyQuery(params.text), completions: [] };
    },
  };
  return { backend, release: () => waiting.splice(0).forEach((resolve) => resolve()) };
}

/** Waits until the daemon has indexed every root, so results are complete. */
async function untilIndexed(daemon: Daemon): Promise<void> {
  const deadline = Date.now() + 5000;
  for (;;) {
    const { repos } = await daemon.request("index/status", {});
    if (repos.every((repo) => repo.tree === "ready")) return;
    if (Date.now() > deadline) throw new Error(`roots not indexed after 5s: ${JSON.stringify(repos)}`);
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

const queryChanged = (text: string, seq: number): WebviewMessage => ({
  v: 1,
  type: "query.changed",
  payload: { text, cursor: text.length, seq },
});

test(
  "a query round-trips to results, preview and open through the real daemon",
  { skip: !existsSync(DAEMON_BINARY) && "daemon not built" },
  async () => {
    const workspace = mkdtempSync(join(tmpdir(), "us-ws-"));
    writeFileSync(join(workspace, "client.py"), "class C:\n    def go(self):\n        self.retry_policy.run()\n");
    const daemon = new Daemon({
      binary: DAEMON_BINARY,
      roots: () => [makeRoot(workspace, "payments-api")],
      settings: () => ({
        ...DEFAULTS,
        exclude: [...DEFAULTS.exclude],
        location: mkdtempSync(join(tmpdir(), "us-idx-")),
      }),
    });
    await daemon.start();
    try {
      await untilIndexed(daemon);
      const host = recordingUi();
      const controller = newController(daemon, host.ui);

      await controller.handle(queryChanged("retry_policy", 1));
      assert.deepEqual(
        host.posted.map((message) => message.type),
        ["parse.result", "search.batch", "search.done"],
      );
      const batch = host.posted[1].payload;
      assert.equal(batch.seq, 1);
      assert.equal(batch.items.length, 1);
      assert.equal(host.posted[2].payload.total, 1);
      assert.equal(host.contextKeys["unifiedSearch.hasResults"], true);

      const ref = batch.items[0].ref;
      await controller.handle({ v: 1, type: "result.select", payload: { ref } });
      const preview = host.posted.at(-1)!;
      assert.equal(preview.type, "preview.result");
      assert.equal(preview.payload.preview.focusLine, 3);

      await controller.handle({ v: 1, type: "result.open", payload: { ref, where: "side" } });
      assert.deepEqual(host.opened[0], {
        target: { path: join(workspace, "client.py"), line: 3, column: 14, length: 12 },
        where: "side",
      });
      assert.equal(host.closedPanels(), 1, "closeOnOpen closes the panel");

      await controller.step(1);
      assert.equal(host.opened.length, 2, "F4 opens a result without the panel");
    } finally {
      await daemon.stop();
    }
  },
);

test("an older seq never overwrites a newer query", async () => {
  const host = recordingUi();
  const { backend, release } = scriptedBackend(new Set(["old"]));
  const controller = newController(backend, host.ui);
  const older = controller.handle(queryChanged("old", 1));
  await controller.handle(queryChanged("new", 2));
  release();
  await older;
  assert.deepEqual(
    host.posted.map((message) => message.payload.seq),
    [2],
  );
});

test("a reopened panel's first query is answered even though its seq starts again at 1", async () => {
  const host = recordingUi();
  const { backend } = scriptedBackend();
  const controller = newController(backend, host.ui);
  await controller.handle(queryChanged("first panel", 7));
  await controller.handle({ v: 1, type: "ready", payload: {} }); // a new webview
  await controller.handle(queryChanged("second panel", 1));
  const parsed = host.posted
    .filter((message) => message.type === "parse.result")
    .map((message) => message.payload.query.raw);
  assert.deepEqual(parsed, ["first panel", "second panel"]);
});

test("recent queries are deduplicated, newest first, and capped", async () => {
  const host = recordingUi();
  const backend: Backend = { on: () => undefined, request: async () => ({}) as never };
  const controller = new SearchController(
    backend,
    host.ui,
    { recentLimit: () => 2, closeOnOpen: () => false, uiSettings: () => TEST_UI_SETTINGS },
    { text: "a", recent: ["b", "a"] },
  );
  controller.rememberQuery();
  controller.restore("c");
  controller.rememberQuery();
  controller.restore();
  assert.deepEqual(host.posted.at(-1)!.payload.recent, ["c", "a"]);
});

test("a search typed while a repo was indexing runs again when the repo is ready", async () => {
  const host = recordingUi();
  const listeners = new Map<string, (payload: any) => void>();
  const searches: string[] = [];
  const backend: Backend = {
    on: (event: string, listener: (payload: any) => void) => listeners.set(event, listener),
    request: async (method, params: any): Promise<any> => {
      if (method === "query/parse") {
        const root = { kind: "text", span: { start: 0, end: params.text.length }, value: params.text };
        return { query: { ...emptyQuery(params.text), root }, completions: [] };
      }
      searches.push(params.searchId);
      return { total: 0, truncated: false, hidden: [], ms: 0 };
    },
  };
  const controller = newController(backend, host.ui);
  const progress = (tree: string) =>
    listeners.get("progress")!({ repos: [{ repoId: "r1", name: "r", tree, history: "off" }] });

  progress("indexing");
  await controller.handle(queryChanged("retry", 1));
  assert.equal(searches.length, 1);

  progress("indexing");
  assert.equal(searches.length, 1, "no re-run while still indexing");
  progress("ready");
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(searches.length, 2, "re-run once the repo is ready");
  progress("ready");
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(searches.length, 2, "no re-run when nothing new became ready");
});

test("the word being completed is left out of the text to search", () => {
  const operator = (start: number, end: number) => [
    {
      label: "since:",
      detail: "",
      group: "operator" as const,
      insert: { title: "", edits: [{ span: { start, end }, newText: "since:" }] },
    },
  ];
  assert.equal(textWithoutCompletedWord("timeout s", 9, operator(8, 9)), "timeout");
  assert.equal(textWithoutCompletedWord("author:ja timeout", 9, operator(0, 9)), "timeout");
  assert.equal(textWithoutCompletedWord("a s b", 3, operator(2, 3)), "a b");
  assert.equal(textWithoutCompletedWord("s", 1, operator(0, 1)), undefined, "nothing else to search");
  assert.equal(textWithoutCompletedWord("timeout s", 3, operator(8, 9)), undefined, "the cursor is elsewhere");
  assert.equal(textWithoutCompletedWord("timeout", 7, []), undefined, "no suggestions");
});

test("while a word is being completed the search runs without it, and Esc searches it as typed", async () => {
  // @covers mock:5
  const host = recordingUi();
  const searched: string[] = [];
  const backend: Backend = {
    on: () => undefined,
    request: async (method, params: any): Promise<any> => {
      if (method === "query/parse") {
        const root = { kind: "text", span: { start: 0, end: params.text.length }, value: params.text };
        const completions = params.text.endsWith(" s")
          ? [
              {
                label: "since:",
                detail: "",
                group: "operator",
                insert: {
                  title: "",
                  edits: [{ span: { start: params.text.length - 1, end: params.text.length }, newText: "since:" }],
                },
              },
            ]
          : [];
        return { query: { ...emptyQuery(params.text), root }, completions };
      }
      searched.push(params.text);
      return { total: 0, truncated: false, hidden: [], ms: 0 };
    },
  };
  const controller = newController(backend, host.ui);
  await controller.handle(queryChanged("timeout s", 1));
  assert.deepEqual(searched, ["timeout"]);
  assert.equal(host.posted.find((message) => message.type === "parse.result")?.payload.searchText, "timeout");

  await controller.handle({
    v: 1,
    type: "query.changed",
    payload: { text: "timeout s", cursor: 9, seq: 2, asTyped: true },
  });
  assert.deepEqual(searched, ["timeout", "timeout s"]);
  assert.equal(host.posted.filter((message) => message.type === "parse.result").at(-1)?.payload.searchText, undefined);
});
