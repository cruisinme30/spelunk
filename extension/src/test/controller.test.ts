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
  IndexStatusResult,
  OpenTarget,
  ParsedQuery,
  ParseResult,
  ResultItem,
  RpcRequests,
  SearchResult,
  UiSettings,
} from "../protocol.gen";
import { RpcError } from "../jsonRpc";
import { ErrorCodes } from "../protocol.gen";
import { makeRoot } from "../roots";
import { parseWebviewMessage } from "../webviewMessages";
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
  const previews: boolean[] = [];
  const shown: string[] = [];
  const contextKeys: Record<string, boolean> = {};
  let closedPanels = 0;
  const ui: Ui = {
    post: (type, payload) => void posted.push({ type, payload } as PostedMessage),
    openTarget: (target, where, preview) => {
      opened.push({ target, where });
      previews.push(preview);
      return Promise.resolve();
    },
    hidePanel: () => {
      closedPanels++;
    },
    openHelp: () => {},
    openSettings: () => {},
    restartDaemon: () => {},
    setContext: (key, value) => void (contextKeys[key] = value),
    saveState: () => {},
    showOpened: (position, total) => void shown.push(`${position} of ${total}`),
  };
  /** The payloads of every posted message of `type`, oldest first. */
  const payloads = <T extends keyof HostToWebview>(type: T) =>
    posted.filter((message) => message.type === type).map((message) => message.payload as HostToWebview[T]);
  return { ui, posted, payloads, opened, previews, shown, contextKeys, closedPanels: () => closedPanels };
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
  ref?: string;
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

/** Lets queued callbacks (such as a re-run search) run before the test continues. */
const nextTurn = () => new Promise((resolve) => setImmediate(resolve));

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
    assert.equal(host.closedPanels(), 0, "opening to the side keeps the panel open");
    await controller.handle({ v: MESSAGE_VERSION, type: "result.open", payload: { ref, where: "current" } });
    assert.equal(host.closedPanels(), 1, "closeOnOpen closes the panel");

    await controller.step(1);
    assert.equal(host.opened.length, 3, "F4 opens a result without the panel");
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
  // @covers setting:ui.recentQueries
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

/** The since: suggestion for the word at start..end, as the only completion. */
const since = (start: number, end: number) => [sinceCompletion(start, end)];

test("the word being completed is left out of the text to search", () => {
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

/** A code-line result. */
type LineResult = Extract<ResultItem, { kind: "line" }>;

/** Three code results for "retry", as one batch. */
const THREE_RESULTS = ["a.py", "b.py", "c.py"].map((path, index): LineResult => ({
  kind: "line",
  ref: `ref-${index}`,
  repoId: "r1",
  path,
  line: index + 1,
  text: "retry()",
  hits: [{ start: 0, end: 5, termIndex: 0 }],
}));

/** A backend that finds THREE_RESULTS for any query and opens each at its path. */
function threeResultsBackend() {
  const scripted = scriptedBackend((method, { text, searchId, ref }) => {
    switch (method) {
      case "query/parse": {
        return { query: parsedQuery(text, true), completions: [] };
      }
      case "search/start": {
        scripted.listeners.get("batch")?.({ searchId, items: THREE_RESULTS });
        return { ...NO_RESULTS, total: THREE_RESULTS.length };
      }
      case "open/resolve": {
        const result = THREE_RESULTS.find((item) => item.ref === ref);
        return {
          path: `/work/${result?.path ?? ""}`,
          line: result?.line ?? 1,
          column: 1,
          length: 5,
        };
      }
      case "initialize":
      case "shutdown":
      case "preview/get":
      case "index/status":
      case "index/rebuild": {
        throw new Error(`unexpected request ${method}`);
      }
    }
  });
  return scripted.backend;
}

test("opening a result says which of the results it is, and F4 steps through the rest", async () => {
  // @covers screen:opened-file
  const host = recordingUi();
  const controller = newController(threeResultsBackend(), host.ui);
  await controller.handle(queryChanged("retry", 1));
  await controller.handle({ v: MESSAGE_VERSION, type: "result.open", payload: { ref: "ref-1", where: "current" } });
  assert.deepEqual(host.opened[0]?.target, { path: "/work/b.py", line: 2, column: 1, length: 5 });
  assert.equal(host.closedPanels(), 1);

  await controller.step(1);
  await controller.step(1);
  await controller.step(-1);
  assert.deepEqual(
    host.opened.map((open) => open.target.path),
    ["/work/b.py", "/work/c.py", "/work/a.py", "/work/c.py"],
  );
  assert.deepEqual(host.shown, ["2 of 3", "3 of 3", "1 of 3", "3 of 3"]);

  controller.restore(); // ⌘P brings the list back with the same query
  assert.equal(host.payloads("state.restore").at(-1)?.text, "retry");
});

test("the finished search's text is known only once its search.done was posted", async () => {
  const host = recordingUi();
  const controller = newController(threeResultsBackend(), host.ui);
  assert.equal(controller.finishedSearchText, undefined);
  await controller.handle(queryChanged("retry", 1));
  assert.equal(controller.finishedSearchText, "retry");
  assert.equal(controller.results.length, 3);
});

test("with closeOnOpen off, opening a result leaves the panel open", async () => {
  // @covers setting:open.closeOnOpen
  const host = recordingUi();
  const controller = newController(threeResultsBackend(), host.ui, { closeOnOpen: () => false });
  await controller.handle(queryChanged("retry", 1));
  await controller.handle({ v: MESSAGE_VERSION, type: "result.open", payload: { ref: "ref-0", where: "current" } });
  assert.equal(host.opened.length, 1);
  assert.equal(host.closedPanels(), 0);
});

test("results open in a preview editor unless open.preview is off", async () => {
  // @covers setting:open.preview
  let preview = true;
  const host = recordingUi();
  const controller = newController(threeResultsBackend(), host.ui, {
    uiSettings: () => ({ ...TEST_UI_SETTINGS, preview }),
  });
  await controller.handle(queryChanged("retry", 1));
  await controller.step(1);
  preview = false; // read again on every open, so a change applies at once
  await controller.step(1);
  assert.deepEqual(host.previews, [true, false]);
});

test("a panel opened after indexing finished still gets each repo's state", () => {
  // The daemon reports indexing only when it changes, and the panel drops
  // messages while it is closed, so restore must resend the last report.
  const host = recordingUi();
  const { backend, listeners } = scriptedBackend(() => NO_RESULTS);
  const controller = newController(backend, host.ui);
  const status: IndexStatusResult = {
    repos: [{ repoId: "r1", name: "unified-search", tree: "ready", history: "ready" }],
  };
  listeners.get("progress")?.(status);
  controller.restore();
  assert.deepEqual(host.payloads("index.status").at(-1), status);
});

test("malformed panel messages are dropped at the boundary, and well-formed ones are normalised", () => {
  const query = (payload: object) => {
    const message = parseWebviewMessage({ v: MESSAGE_VERSION, type: "query.changed", payload });
    return message?.type === "query.changed" ? message.payload : undefined;
  };
  for (const raw of [
    null,
    "query.changed",
    [],
    { type: "ready", payload: {} },
    { v: 2, type: "ready", payload: {} },
    { v: MESSAGE_VERSION, type: "nope", payload: {} },
    { v: MESSAGE_VERSION, type: "toString", payload: {} },
    { v: MESSAGE_VERSION, type: "__proto__", payload: {} },
    { v: MESSAGE_VERSION, type: "ready" },
    { v: MESSAGE_VERSION, type: "ready", payload: [] },
    { v: MESSAGE_VERSION, type: "result.open", payload: { ref: "r" } },
    { v: MESSAGE_VERSION, type: "result.open", payload: { ref: "r", where: "elsewhere" } },
    { v: MESSAGE_VERSION, type: "result.select", payload: { ref: 7 } },
    { v: MESSAGE_VERSION, type: "results.more", payload: { searchId: "s1" } },
    { v: MESSAGE_VERSION, type: "help.try", payload: { query: null } },
  ]) {
    assert.equal(parseWebviewMessage(raw), undefined, JSON.stringify(raw));
  }
  for (const payload of [
    { text: 5, cursor: 0, seq: 1 },
    { text: "a", cursor: 0, seq: Number.NaN },
    { text: "a", cursor: 0, seq: "2" },
    { text: "a", cursor: 0, seq: -1 },
    { text: "a", cursor: 0, seq: 1.5 },
    { text: "a", cursor: Number.NaN, seq: 1 },
    { text: "a", seq: 1 },
  ]) {
    assert.equal(query(payload), undefined, JSON.stringify(payload));
  }
  assert.deepEqual(query({ text: "abc", cursor: 99, seq: 3, asTyped: "yes", extra: 1 }), {
    text: "abc",
    cursor: 3,
    seq: 3,
  });
  assert.equal(query({ text: "abc", cursor: -4, seq: 0 })?.cursor, 0);
  assert.deepEqual(
    parseWebviewMessage({
      v: MESSAGE_VERSION,
      type: "welcome.choose",
      payload: { preset: "everything", historyDepth: "6m", symbols: "no" },
    })?.payload,
    { historyDepth: "6m" },
    "only valid choices reach the user's settings",
  );
});

test("a query.changed whose seq is not a number is ignored and doesn't let older queries back in", async () => {
  const host = recordingUi();
  const controller = newController(parseOnlyBackend().backend, host.ui);
  await controller.handle(queryChanged("good", 2));
  const payload = { text: "bad", cursor: 3, seq: Number.NaN };
  await controller.handle({ v: MESSAGE_VERSION, type: "query.changed", payload });
  await controller.handle(queryChanged("older", 1));
  assert.deepEqual(
    host.payloads("parse.result").map((parsed) => parsed.query.raw),
    ["good"],
  );
});

test("a burst of keystrokes whose parses finish out of order only ever moves forward", async () => {
  const host = recordingUi();
  const releases: (() => void)[] = [];
  const { backend } = scriptedBackend(async (method, { text, searchId }) => {
    if (method === "query/parse") {
      await new Promise<void>((resolve) => releases.push(resolve));
      return { query: parsedQuery(text, true), completions: [] };
    }
    return { ...NO_RESULTS, total: Number(searchId?.slice(1)) };
  });
  const controller = newController(backend, host.ui);
  const keystrokes = Array.from({ length: 30 }, (_, index) =>
    controller.handle(queryChanged(`q${index + 1}`, index + 1)),
  );
  while (releases.length > 0) {
    releases.pop()?.(); // the newest keystroke's parse finishes first
    await nextTurn();
  }
  await Promise.all(keystrokes);
  assert.deepEqual(
    host.payloads("parse.result").map((parsed) => parsed.seq),
    [30],
  );
  assert.deepEqual(
    host.payloads("search.done").map((done) => done.seq),
    [30],
  );
  assert.equal(controller.text, "q30");
});

test("a search still running when a new webview says ready posts nothing to it", async () => {
  const host = recordingUi();
  let answer: (() => void) | undefined;
  const { backend, listeners } = scriptedBackend(async (method, { text }) => {
    if (method === "query/parse") return { query: parsedQuery(text, true), completions: [] };
    await new Promise<void>((resolve) => (answer = resolve));
    return NO_RESULTS;
  });
  const controller = newController(backend, host.ui);
  const searching = controller.handle(queryChanged("retry", 1));
  await nextTurn();
  await controller.handle({ v: MESSAGE_VERSION, type: "ready", payload: {} }); // the panel was closed and reopened
  listeners.get("batch")?.({ searchId: "s1", items: THREE_RESULTS });
  answer?.();
  await searching;
  assert.deepEqual(host.payloads("search.batch"), [], "seq 1 of the old webview would match the new one's seq 1");
  assert.deepEqual(host.payloads("search.done"), []);
  await controller.handle({ v: MESSAGE_VERSION, type: "results.more", payload: { searchId: "s1", cursor: "c" } });
  assert.deepEqual(host.payloads("search.done"), [], "load more cannot revive it");
});

test("load more for a search the query has moved on from is ignored", async () => {
  const host = recordingUi();
  const starts: (string | undefined)[] = [];
  const { backend } = scriptedBackend((method, { text, searchId }) => {
    if (method === "query/parse") return { query: parsedQuery(text, true), completions: [] };
    starts.push(searchId);
    return NO_RESULTS;
  });
  const controller = newController(backend, host.ui);
  await controller.handle(queryChanged("one", 1));
  await controller.handle(queryChanged("two", 2));
  await controller.handle({ v: MESSAGE_VERSION, type: "results.more", payload: { searchId: "s1", cursor: "c" } });
  assert.deepEqual(starts, ["s1", "s2"]);
});

test("a daemon that dies mid-search ends the search with its error, and the next query searches again", async () => {
  const host = recordingUi();
  let crash = true;
  const { backend } = scriptedBackend((method, { text }) => {
    if (method === "query/parse") return { query: parsedQuery(text, true), completions: [] };
    if (crash) throw new Error("daemon exited");
    return NO_RESULTS;
  });
  const controller = newController(backend, host.ui);
  await controller.handle(queryChanged("retry", 1));
  assert.equal(host.payloads("search.done").at(-1)?.error, "daemon exited");
  crash = false;
  await controller.handle(queryChanged("retry", 2));
  assert.deepEqual(host.payloads("search.done").at(-1), { seq: 2, searchId: "s2", ...NO_RESULTS });
});

test("opening a stale result greys it out and neither closes the panel nor remembers the query", async () => {
  // @covers failure:ref-stale
  const host = recordingUi();
  const { backend } = scriptedBackend(() => {
    throw new RpcError(ErrorCodes.RefStale, "gone");
  });
  const controller = newController(backend, host.ui);
  controller.restore("retry");
  await controller.handle({ v: MESSAGE_VERSION, type: "result.open", payload: { ref: "old", where: "current" } });
  assert.deepEqual(host.payloads("preview.result"), [{ ref: "old", preview: null, stale: true }]);
  assert.equal(host.closedPanels(), 0);
  assert.deepEqual(host.shown, []);
});

test("a pasted query too long for the recent list is searched but not remembered", () => {
  const host = recordingUi();
  const controller = newController(parseOnlyBackend().backend, host.ui, { recentLimit: () => Number.NaN });
  controller.restore("short");
  controller.rememberQuery();
  assert.deepEqual(host.payloads("state.restore").at(-1)?.recent, [], "a limit that isn't a number keeps nothing");
  const kept = newController(parseOnlyBackend().backend, host.ui, {}, { text: "", recent: ["short"] });
  kept.restore("x".repeat(5000));
  kept.rememberQuery();
  kept.restore();
  assert.deepEqual(host.payloads("state.restore").at(-1)?.recent, ["short"]);
});
