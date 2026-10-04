// Host side of Contract 2 against the real daemon binary: a query typed
// in the webview round-trips to results, preview and open.
// @covers msg:query.changed msg:parse.result msg:search.batch msg:search.done msg:result.select msg:preview.result msg:result.open msg:panel.close
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { Daemon } from "../src/daemon";
import { SearchController, type Ui } from "../src/controller";
import type { OpenTarget } from "../src/protocol.gen";
import { makeRoot } from "../src/roots";
import { DEFAULTS } from "../src/settings";

const binary = process.env.UNIFIED_SEARCH_DAEMON ?? join(__dirname, "../../daemon/bin/unified-search-daemon");

function fakeUi() {
  const posted: { type: string; payload: any }[] = [];
  const opened: { target: OpenTarget; where: string }[] = [];
  const ctx: Record<string, boolean> = {};
  let hidden = 0;
  const ui: Ui = {
    post: (type, payload) => void posted.push({ type, payload }),
    openTarget: async (target, where) => void opened.push({ target, where }),
    hidePanel: () => void hidden++,
    openHelp: () => undefined,
    openSettings: () => undefined,
    restartDaemon: () => undefined,
    setContext: (k, v) => void (ctx[k] = v),
    saveState: () => undefined,
  };
  return { ui, posted, opened, ctx, hidden: () => hidden };
}

test("query -> results -> preview -> open, through the real daemon", { skip: !existsSync(binary) && "daemon not built" }, async () => {
  const dir = mkdtempSync(join(tmpdir(), "us-ws-"));
  writeFileSync(join(dir, "client.py"), "class C:\n    def go(self):\n        self.retry_policy.run()\n");
  const d = new Daemon({ binary, roots: () => [makeRoot(dir, "payments-api")], settings: () => ({ ...DEFAULTS, location: dir }) });
  await d.start();
  const f = fakeUi();
  const c = new SearchController(d, f.ui, { recentLimit: () => 20, closeOnOpen: () => true, uiSettings: () => ({ typingDelayMs: 0, openTrigger: "doubleClick", preview: true, showParsedQuery: true, caseSensitive: false }) });

  await c.handle({ v: 1, type: "query.changed", payload: { text: "retry_policy", cursor: 12, seq: 1 } });
  const types = f.posted.map((p) => p.type);
  assert.deepEqual(types, ["parse.result", "search.batch", "search.done"]);
  const batch = f.posted[1].payload;
  assert.equal(batch.seq, 1);
  assert.equal(batch.items.length, 1);
  assert.equal(f.posted[2].payload.total, 1);
  assert.equal(f.ctx["unifiedSearch.hasResults"], true);

  const ref = batch.items[0].ref;
  await c.handle({ v: 1, type: "result.select", payload: { ref } });
  const pv = f.posted.at(-1)!;
  assert.equal(pv.type, "preview.result");
  assert.equal(pv.payload.preview.focusLine, 3);

  await c.handle({ v: 1, type: "result.open", payload: { ref, where: "side" } });
  assert.equal(f.opened.length, 1);
  assert.deepEqual(f.opened[0], { target: { path: join(dir, "client.py"), line: 3, column: 14, length: 12 }, where: "side" });
  assert.equal(f.hidden(), 1, "closeOnOpen hides the panel");

  // F4 steps through the last results without the panel.
  await c.step(1);
  assert.equal(f.opened.length, 2);
  await d.stop();
});

test("an older seq never overwrites a newer query", async () => {
  const f = fakeUi();
  let resolveOld!: () => void;
  const backend = {
    on: () => undefined,
    request: async (method: string, params: any): Promise<any> => {
      if (method === "query/parse") {
        if (params.text === "old") await new Promise<void>((r) => (resolveOld = r));
        return { query: { version: 1, raw: params.text, root: null, globals: { case: null, count: null, type: null }, mode: "workingTree", diagnostics: [] }, completions: [] };
      }
      throw new Error("unexpected " + method);
    },
  };
  const c = new SearchController(backend as any, f.ui, { recentLimit: () => 5, closeOnOpen: () => true, uiSettings: () => ({ typingDelayMs: 0, openTrigger: "doubleClick", preview: true, showParsedQuery: true, caseSensitive: false }) });
  const old = c.handle({ v: 1, type: "query.changed", payload: { text: "old", cursor: 3, seq: 1 } });
  await c.handle({ v: 1, type: "query.changed", payload: { text: "new", cursor: 3, seq: 2 } });
  resolveOld();
  await old;
  assert.deepEqual(f.posted.map((p) => p.payload.seq), [2]);
});

test("recent queries are deduplicated and capped", async () => {
  const f = fakeUi();
  const backend = { on: () => undefined, request: async () => ({}) };
  const c = new SearchController(backend as any, f.ui, { recentLimit: () => 2, closeOnOpen: () => false, uiSettings: () => ({ typingDelayMs: 0, openTrigger: "doubleClick", preview: true, showParsedQuery: true, caseSensitive: false }) }, { text: "a", recent: ["b", "a"] });
  c.rememberQuery();
  c.restore("c");
  c.rememberQuery();
  const restore = f.posted.filter((p) => p.type === "state.restore").at(-1)!;
  assert.equal(restore.payload.text, "c");
  c.restore();
  assert.deepEqual(f.posted.at(-1)!.payload.recent, ["c", "a"]);
});
