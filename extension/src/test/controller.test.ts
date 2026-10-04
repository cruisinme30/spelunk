// Host side of Contract 2. The round-trip test drives the real daemon binary;
// the others use a scripted backend to pin down ordering rules.
// @covers msg:query.changed msg:parse.result msg:search.batch msg:search.done msg:result.select msg:preview.result msg:result.open msg:panel.close
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { type Backend, SearchController, type Ui, type WebviewMessage } from "../controller";
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
      settings: () => ({ ...DEFAULTS, exclude: [...DEFAULTS.exclude], location: workspace }),
    });
    await daemon.start();
    try {
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
