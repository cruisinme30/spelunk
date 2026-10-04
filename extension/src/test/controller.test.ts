// The search panel's host side. The round-trip test drives the real daemon binary;
// the others use a scripted backend to pin down ordering rules.
// @covers msg:query.changed msg:parse.result msg:search.batch msg:search.done msg:result.select msg:preview.result msg:result.open
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import {
  type Backend,
  type ControllerOptions,
  MESSAGE_VERSION,
  type PersistedState,
  SearchController,
  textWithoutCompletedWord,
  type Ui,
  type WebviewMessage,
} from "../controller";
import type {
  Completion,
  HostToWebview,
  IndexState,
  OpenTarget,
  ParsedQuery,
  ParseResult,
  RpcRequests,
  SearchResult,
  UiSettings,
} from "../protocol.gen";
import { makeRoot } from "../roots";
import { newTestDaemon, SKIP_WITHOUT_DAEMON as skip, untilIndexed } from "./realDaemon";

const TEST_UI_SETTINGS: UiSettings = {
  typingDelayMs: 0,
  openTrigger: "doubleClick",
  preview: true,
  showParsedQuery: true,
  caseSensitive: false,
};

/** What search/start answers in the scripted backends: no results. */
const NO_RESULTS: SearchResult = { total: 0, truncated: false, hidden: [], ms: 0 };

/** A message the controller posted to the panel. */
type PostedMessage = { [K in keyof HostToWebview]: { type: K; payload: HostToWebview[K] } }[keyof HostToWebview];

/** A Ui that records everything the controller does. */
function recordingUi() {
  const posted: PostedMessage[] = [];
  const opened: { target: OpenTarget; where: string }[] = [];
  const contextKeys: Record<string, boolean> = {};
  let closedPanels = 0;
  const ui: Ui = {
    post: (type, payload) => void posted.push({ type, payload } as PostedMessage),
    openTarget: (target, where) => {
      opened.push({ target, where });
      return Promise.resolve();
    },
    hidePanel: () => void closedPanels++,
    openHelp: () => undefined,
    openSettings: () => undefined,
    restartDaemon: () => undefined,
    setContext: (key, value) => void (contextKeys[key] = value),
    saveState: () => undefined,
  };
  /** The payloads of every posted message of `type`, oldest first. */
  const payloads = <T extends keyof HostToWebview>(type: T) =>
    posted.filter((message) => message.type === type).map((message) => message.payload as HostToWebview[T]);
  return { ui, posted, payloads, opened, contextKeys, closedPanels: () => closedPanels };
}

function newController(backend: Backend, ui: Ui, options: Partial<ControllerOptions> = {}, initial?: PersistedState) {
  const defaults: ControllerOptions = {
    recentLimit: () => 20,
    closeOnOpen: () => true,
    uiSettings: () => TEST_UI_SETTINGS,
  };
  return new SearchController(backend, ui, { ...defaults, ...options }, initial);
}

/** A query as the daemon parses `raw`: empty (no root) unless it is searchable text. */
const parsedQuery = (raw: string, searchable = false): ParsedQuery => ({
  version: 1,
  raw,
  root: searchable
    ? { kind: "text", value: raw, match: "literal", termIndex: 0, span: { start: 0, end: raw.length } }
    : null,
  globals: { case: null, count: null, type: null },
  mode: "workingTree",
  diagnostics: [],
});

/** An operator suggestion that replaces `start`..`end` with "since:". */
const sinceCompletion = (start: number, end: number): Completion => ({
  label: "since:",
  detail: "",
  group: "operator",
  insert: { title: "", edits: [{ span: { start, end }, newText: "since:" }] },
});

/** The params of the scripted backends' requests: query/parse's text, search/start's text and searchId. */
interface ScriptedParams {
  text: string;
  searchId?: string;
}

/**
 * A Backend whose requests `answer` handles, and which keeps the listeners
 * the controller registers so a test can play batches and progress.
 */
function scriptedBackend(answer: (method: keyof RpcRequests, params: ScriptedParams) => unknown) {
  const listeners = new Map<string, (payload: unknown) => void>();
  const backend: Backend = {
    on: (event: string, listener: (payload: never) => void) =>
      listeners.set(event, listener as (payload: unknown) => void),
    request: async (method, params) => (await answer(method, params as ScriptedParams)) as never,
  };
  return { backend, listeners };
}

/** A backend whose query/parse answers at once, except for texts in `slow`, which wait for release(). */
function parseOnlyBackend(slow = new Set<string>()) {
  const waiting: (() => void)[] = [];
  const { backend } = scriptedBackend(async (method, { text }): Promise<ParseResult> => {
    if (method !== "query/parse") throw new Error(`unexpected request ${method}`);
    if (slow.has(text)) await new Promise<void>((resolve) => waiting.push(resolve));
    return { query: parsedQuery(text), completions: [] };
  });
  const release = () => {
    for (const resolve of waiting.splice(0)) resolve();
  };
  return { backend, release };
}

const queryChanged = (text: string, seq: number, asTyped?: true): WebviewMessage => ({
  v: MESSAGE_VERSION,
  type: "query.changed",
  payload: { text, cursor: text.length, seq, ...(asTyped ? { asTyped } : {}) },
});

test("a query round-trips to results, preview and open through the real daemon", { skip }, async () => {
  const workspace = mkdtempSync(join(tmpdir(), "us-ws-"));
  writeFileSync(join(workspace, "client.py"), "class C:\n    def go(self):\n        self.retry_policy.run()\n");
  const daemon = newTestDaemon([makeRoot(workspace, "payments-api")]);
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
    const [batch] = host.payloads("search.batch");
    const [done] = host.payloads("search.done");
    assert.ok(batch && done);
    assert.equal(batch.seq, 1);
    assert.equal(batch.items.length, 1);
    assert.equal(done.total, 1);
    assert.equal(host.contextKeys["unifiedSearch.hasResults"], true);

    const ref = batch.items[0]?.ref ?? "";
    await controller.handle({ v: MESSAGE_VERSION, type: "result.select", payload: { ref } });
    const preview = host.payloads("preview.result").at(-1)?.preview;
    assert.equal(preview?.kind === "file" && preview.focusLine, 3);

    await controller.handle({ v: MESSAGE_VERSION, type: "result.open", payload: { ref, where: "side" } });
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
});

test("an older seq never overwrites a newer query", async () => {
  const host = recordingUi();
  const { backend, release } = parseOnlyBackend(new Set(["old"]));
  const controller = newController(backend, host.ui);
  const older = controller.handle(queryChanged("old", 1));
  await controller.handle(queryChanged("new", 2));
  release();
  await older;
  assert.deepEqual(
    host.payloads("parse.result").map((parsed) => parsed.seq),
    [2],
  );
});

test("a reopened panel's first query is answered even though its seq starts again at 1", async () => {
  const host = recordingUi();
  const controller = newController(parseOnlyBackend().backend, host.ui);
  await controller.handle(queryChanged("first panel", 7));
  await controller.handle({ v: MESSAGE_VERSION, type: "ready", payload: {} }); // a new webview
  await controller.handle(queryChanged("second panel", 1));
  const parsed = host.payloads("parse.result").map((message) => message.query.raw);
  assert.deepEqual(parsed, ["first panel", "second panel"]);
});

test("recent queries are deduplicated, newest first, and capped", () => {
  const host = recordingUi();
  const controller = newController(
    parseOnlyBackend().backend,
    host.ui,
    { recentLimit: () => 2, closeOnOpen: () => false },
    { text: "a", recent: ["b", "a"] },
  );
  controller.rememberQuery();
  controller.restore("c");
  controller.rememberQuery();
  controller.restore();
  assert.deepEqual(host.payloads("state.restore").at(-1)?.recent, ["c", "a"]);
});

test("closing the panel hides it and remembers the query", async () => {
  // @covers msg:panel.close
  const host = recordingUi();
  const controller = newController(parseOnlyBackend().backend, host.ui);
  await controller.handle(queryChanged("timeout", 1));
  await controller.handle({ v: MESSAGE_VERSION, type: "panel.close", payload: {} });
  assert.equal(host.closedPanels(), 1);
  controller.restore();
  assert.deepEqual(host.payloads("state.restore").at(-1)?.recent, ["timeout"]);
});

test("a search typed while a repo was indexing runs again when the repo is ready", async () => {
  const host = recordingUi();
  const searches: string[] = [];
  const { backend, listeners } = scriptedBackend((method, { text, searchId }) => {
    if (method === "query/parse") return { query: parsedQuery(text, true), completions: [] };
    searches.push(searchId ?? "");
    return NO_RESULTS;
  });
  const controller = newController(backend, host.ui);
  const progress = (tree: IndexState) => {
    listeners.get("progress")?.({ repos: [{ repoId: "r1", name: "r", tree, history: "off" }] });
  };
  const nextTurn = () => new Promise((resolve) => setImmediate(resolve));

  progress("indexing");
  await controller.handle(queryChanged("retry", 1));
  assert.equal(searches.length, 1);

  progress("indexing");
  assert.equal(searches.length, 1, "no re-run while still indexing");
  progress("ready");
  await nextTurn();
  assert.equal(searches.length, 2, "re-run once the repo is ready");
  progress("ready");
  await nextTurn();
  assert.equal(searches.length, 2, "no re-run when nothing new became ready");
});

test("the word being completed is left out of the text to search", () => {
  const since = (start: number, end: number) => [sinceCompletion(start, end)];
  assert.equal(textWithoutCompletedWord("timeout s", 9, since(8, 9)), "timeout");
  assert.equal(textWithoutCompletedWord("author:ja timeout", 9, since(0, 9)), "timeout");
  assert.equal(textWithoutCompletedWord("a s b", 3, since(2, 3)), "a b");
  assert.equal(textWithoutCompletedWord("s", 1, since(0, 1)), undefined, "nothing else to search");
  assert.equal(textWithoutCompletedWord("timeout s", 3, since(8, 9)), undefined, "the cursor is elsewhere");
  assert.equal(textWithoutCompletedWord("timeout", 7, []), undefined, "no suggestions");
});

test("while a word is being completed the search runs without it, and Esc searches it as typed", async () => {
  // @covers screen:operator-suggestions
  const host = recordingUi();
  const searched: string[] = [];
  const { backend } = scriptedBackend((method, { text }) => {
    if (method === "query/parse") {
      const completions = text.endsWith(" s") ? [sinceCompletion(text.length - 1, text.length)] : [];
      return { query: parsedQuery(text, true), completions };
    }
    searched.push(text);
    return NO_RESULTS;
  });
  const controller = newController(backend, host.ui);
  await controller.handle(queryChanged("timeout s", 1));
  assert.deepEqual(searched, ["timeout"]);
  assert.equal(host.payloads("parse.result")[0]?.searchText, "timeout");

  await controller.handle(queryChanged("timeout s", 2, true));
  assert.deepEqual(searched, ["timeout", "timeout s"]);
  assert.equal(host.payloads("parse.result").at(-1)?.searchText, undefined);
});
