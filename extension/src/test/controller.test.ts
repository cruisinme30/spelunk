// The search panel's host side. The round-trip test drives the real daemon binary;
// the others use a scripted backend to pin down ordering rules.
// @covers msg:query.changed msg:parse.result msg:search.batch msg:search.done msg:result.select msg:preview.result msg:result.open
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import {
  type Backend,
  type ControllerOptions,
  type PersistedState,
  SearchController,
  type STOP_WATCHING,
  textWithoutCompletedWord,
  type Ui,
} from "../controller";
import type {
  Completion,
  HostToWebview,
  IndexState,
  IndexStatusResult,
  OpenTarget,
  ParsedQuery,
  ParseResult,
  PinnedQuery,
  ReplacePlan,
  ResultItem,
  RpcRequests,
  SearchResult,
  UiSettings,
  WatchMatchedParams,
} from "../protocol.gen";
import { RpcError } from "../jsonRpc";
import { ErrorCodes } from "../protocol.gen";
import { makeRoot } from "../roots";
import type { DocumentLines } from "../replaceEdits";
import { MESSAGE_VERSION, parseWebviewMessage, type WebviewMessage } from "../webviewMessages";
import { newTestDaemon, SKIP_WITHOUT_DAEMON as skip, untilIndexed, waitFor } from "./realDaemon";

const TEST_UI_SETTINGS: UiSettings = {
  typingDelayMs: 0,
  openTrigger: "doubleClick",
  preview: true,
  showParsedQuery: true,
  caseSensitive: "off",
  wholeWord: false,
  order: "best",
  contextLines: 0,
};

/** What search/start answers in the scripted backends: no results. */
const NO_RESULTS: SearchResult = { total: 0, truncated: false, hidden: [], ms: 0 };

/** A message the controller posted to the panel. */
type PostedMessage = { [K in keyof HostToWebview]: { type: K; payload: HostToWebview[K] } }[keyof HostToWebview];

/** A Ui that records everything the controller does. */
function recordingUi() {
  const posted: PostedMessage[] = [];
  const opened: { target: OpenTarget; where: string }[] = [];
  const openedItems: (ResultItem | undefined)[] = [];
  const previews: boolean[] = [];
  const shown: string[] = [];
  const contextKeys: Record<string, boolean> = {};
  const errors: string[] = [];
  let closedPanels = 0;
  const ui: Ui = {
    post: (type, payload) => void posted.push({ type, payload } as PostedMessage),
    openTarget: (target, where, preview, item) => {
      opened.push({ target, where });
      openedItems.push(item);
      previews.push(preview);
      return Promise.resolve(true);
    },
    hidePanel: () => {
      closedPanels++;
    },
    openHelp: () => {},
    openSettings: () => {},
    restartDaemon: () => {},
    setContext: (key, value) => void (contextKeys[key] = value),
    saveState: () => {},
    savePinned: () => Promise.resolve(),
    saveContextLines: () => {},
    showOpened: (position, total) => void shown.push(`${position} of ${total}`),
    readDocument: () => Promise.resolve(undefined as DocumentLines | undefined),
    applyEdits: () => Promise.resolve(true),
    saveFiles: () => {},
    showWatchMatch: () => Promise.resolve(undefined as typeof STOP_WATCHING | undefined),
    showQuery: () => {},
    showError: (message) => void errors.push(message),
  };
  /** The payloads of every posted message of `type`, oldest first. */
  const payloads = <T extends keyof HostToWebview>(type: T) =>
    posted.filter((message) => message.type === type).map((message) => message.payload as HostToWebview[T]);
  return {
    ui,
    posted,
    payloads,
    opened,
    openedItems,
    previews,
    shown,
    contextKeys,
    errors,
    closedPanels: () => closedPanels,
  };
}

function newController(backend: Backend, ui: Ui, options: Partial<ControllerOptions> = {}, initial?: PersistedState) {
  const defaults: ControllerOptions = {
    recentLimit: () => 20,
    pinnedQueries: () => [],
    closeOnOpen: () => true,
    previewContextLines: () => 7,
    uiSettings: () => TEST_UI_SETTINGS,
    openFiles: () => [],
    watchNotify: () => true,
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
  openFiles?: string[];
  contextLines?: number;
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

test("a result at a revision opens there, and compare is a message the panel may send", { skip }, async () => {
  // @covers msg:result.open
  const workspace = mkdtempSync(join(tmpdir(), "us-ws-"));
  const git = (...args: string[]) =>
    execFileSync("git", ["-C", workspace, ...args], { encoding: "utf8", stdio: "pipe" }).trim();
  git("init", "--quiet");
  git("config", "user.email", "me@example.com");
  git("config", "user.name", "Me");
  git("config", "commit.gpgsign", "false");
  writeFileSync(join(workspace, "client.py"), "retry_policy = 1\n");
  git("add", "--all");
  git("commit", "--quiet", "-m", "First");
  git("tag", "v1.0.0");
  writeFileSync(join(workspace, "client.py"), "retry_policy = 2\n");
  const daemon = newTestDaemon([makeRoot(workspace, "payments-api")]);
  await daemon.start();
  try {
    await untilIndexed(daemon);
    const host = recordingUi();
    const controller = newController(daemon, host.ui);
    await controller.handle(queryChanged("at:v1.0.0 type:code retry_policy", 1));
    await waitFor("the search to finish", () => host.payloads("search.done").length > 0);
    const [done] = host.payloads("search.done");
    assert.equal(done?.revisions?.[0]?.sha, git("rev-parse", "v1.0.0"), "search.done names the commit read");
    const item = host.payloads("search.batch")[0]?.items[0];
    assert.ok(item?.kind === "line", "a code result");
    assert.equal(item.sinceRevision, "changed", "client.py changed since v1.0.0");

    const message = parseWebviewMessage({
      v: MESSAGE_VERSION,
      type: "result.open",
      payload: { ref: item.ref, where: "compare" },
    });
    assert.ok(message, "compare is a valid place to open a result");
    await controller.handle(message);
    assert.deepEqual(host.opened[0], {
      target: {
        path: join(workspace, "client.py"),
        line: 1,
        column: 1,
        length: 12,
        revision: git("rev-parse", "v1.0.0"),
        revisionLabel: "v1.0.0",
      },
      where: "compare",
    });
    const { text } = await daemon.request("revision/text", { ref: item.ref });
    assert.equal(text, "retry_policy = 1\n", "revision/text is the file as it was");
  } finally {
    await daemon.stop();
  }
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
    assert.equal(host.contextKeys["spelunk.hasResults"], true);

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

test("lowering the recent-query limit drops the queries past it, and 0 drops them all", () => {
  // @covers setting:ui.recentQueries
  const host = recordingUi();
  const saved: PersistedState[] = [];
  const ui = { ...host.ui, saveState: (state: PersistedState) => saved.push(structuredClone(state)) };
  let limit = 20;
  const controller = newController(
    parseOnlyBackend().backend,
    ui,
    { recentLimit: () => limit },
    {
      text: "",
      recent: ["a", "b", "c"],
    },
  );
  controller.restore();
  assert.deepEqual(host.payloads("state.restore").at(-1)?.recent, ["a", "b", "c"]);
  assert.equal(saved.length, 0, "nothing to drop, nothing saved");
  limit = 2;
  controller.settingsChanged();
  assert.deepEqual(host.payloads("state.restore").at(-1)?.recent, ["a", "b"]);
  limit = 0;
  controller.restore();
  assert.deepEqual(host.payloads("state.restore").at(-1)?.recent, []);
  assert.deepEqual(
    saved.map((state) => state.recent),
    [["a", "b"], []],
    "the shorter list is saved, so a reload keeps it",
  );
});

test("a settings change sends the settings and pins, but not the box's text, which lags the typing", async () => {
  const host = recordingUi();
  const controller = newController(parseOnlyBackend().backend, host.ui, { pinnedQueries: () => [{ query: "retry" }] });
  await controller.handle(queryChanged("timeout", 1));
  controller.settingsChanged();
  const sent = host.payloads("state.restore").at(-1);
  assert.equal(sent?.text, undefined);
  assert.deepEqual(sent?.pinned, [{ query: "retry" }]);
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

test("removing a recent query takes it off the list and keeps it off", async () => {
  // @covers msg:recent.remove
  const saved: PersistedState[] = [];
  const host = recordingUi();
  const ui = { ...host.ui, saveState: (state: PersistedState) => saved.push(structuredClone(state)) };
  const controller = newController(parseOnlyBackend().backend, ui, {}, { text: "", recent: ["c", "b", "a"] });
  const remove = (query: string) =>
    controller.handle({ v: MESSAGE_VERSION, type: "recent.remove", payload: { query } });
  await remove("b");
  await remove("not there");
  controller.restore();
  assert.deepEqual(host.payloads("state.restore").at(-1)?.recent, ["c", "a"]);
  assert.deepEqual(
    saved.map((state) => state.recent),
    [["c", "a"]],
    "saved once, and not again for a query that isn't listed",
  );
});

test("pinning adds a query at the end, renames it when pinned already, and unpinning removes it", async () => {
  // @covers msg:pinned.save
  // @covers msg:pinned.remove
  // @covers setting:ui.pinnedQueries
  let pinned: PinnedQuery[] = [{ query: "sym:RetryPolicy" }];
  const saves: PinnedQuery[][] = [];
  const host = recordingUi();
  const ui = {
    ...host.ui,
    savePinned: (list: PinnedQuery[]) => {
      saves.push(list);
      pinned = list;
      return Promise.resolve();
    },
  };
  const controller = newController(parseOnlyBackend().backend, ui, { pinnedQueries: () => structuredClone(pinned) });
  const send = (message: WebviewMessage) => controller.handle(message);
  const save = (payload: PinnedQuery) => send({ v: MESSAGE_VERSION, type: "pinned.save", payload });
  const remove = (query: string) => send({ v: MESSAGE_VERSION, type: "pinned.remove", payload: { query } });

  await save({ query: "  since:2w timeout " });
  assert.deepEqual(pinned, [{ query: "sym:RetryPolicy" }, { query: "since:2w timeout" }], "trimmed, at the end");
  await save({ query: "sym:RetryPolicy", name: " Retry policy " });
  assert.deepEqual(pinned[0], { query: "sym:RetryPolicy", name: "Retry policy" }, "renamed in place");
  await save({ query: "sym:RetryPolicy", name: "Retry policy" });
  await save({ query: "   " });
  await remove("not pinned");
  assert.equal(saves.length, 2, "nothing saved when nothing changes");
  await save({ query: "sym:RetryPolicy", name: "" });
  assert.deepEqual(pinned[0], { query: "sym:RetryPolicy" }, "a blank name clears it");
  await remove("since:2w timeout");
  assert.deepEqual(pinned, [{ query: "sym:RetryPolicy" }]);

  controller.restore();
  assert.deepEqual(host.payloads("state.restore").at(-1)?.pinned, [{ query: "sym:RetryPolicy" }]);
});

test("changes to the pinned list made at once each keep the others", async () => {
  // @covers msg:pinned.save msg:pinned.watch msg:pinned.remove setting:ui.pinnedQueries
  let pinned: PinnedQuery[] = [{ query: "msg:revert" }, { query: "author:jason" }, { query: "sym:RetryPolicy" }];
  const host = recordingUi();
  // Like a settings write: the list changes a turn after it is saved.
  const ui: Ui = {
    ...host.ui,
    savePinned: async (list) => {
      await nextTurn();
      pinned = list;
    },
  };
  const { backend } = watchBackend();
  const controller = newController(backend, ui, { pinnedQueries: () => structuredClone(pinned) });
  const watch = (query: string) =>
    controller.handle({ v: MESSAGE_VERSION, type: "pinned.watch", payload: { query, watch: true } });

  await Promise.all([
    watch("msg:revert"),
    watch("author:jason"),
    controller.handle({ v: MESSAGE_VERSION, type: "pinned.save", payload: { query: "since:2w timeout" } }),
    controller.handle({ v: MESSAGE_VERSION, type: "pinned.remove", payload: { query: "sym:RetryPolicy" } }),
  ]);
  assert.deepEqual(pinned, [
    { query: "msg:revert", watch: true },
    { query: "author:jason", watch: true },
    { query: "since:2w timeout" },
  ]);
});

test("the Context buttons save spelunk.results.contextLines when it changes", async () => {
  // @covers msg:context.set
  let contextLines = 0;
  const saves: number[] = [];
  const host = recordingUi();
  const ui = {
    ...host.ui,
    saveContextLines: (lines: number) => {
      saves.push(lines);
      contextLines = lines;
    },
  };
  const controller = newController(parseOnlyBackend().backend, ui, {
    uiSettings: () => ({ ...TEST_UI_SETTINGS, contextLines }),
  });
  const set = (payload: unknown) => {
    const message = parseWebviewMessage({ v: MESSAGE_VERSION, type: "context.set", payload });
    assert.ok(message, `context.set ${JSON.stringify(payload)} is valid`);
    return controller.handle(message);
  };
  await set({ lines: 2 });
  await set({ lines: 2 });
  await set({ lines: 3 });
  await set({ lines: 0 });
  assert.deepEqual(saves, [2, 3, 0], "saved only when it changes");
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

/** Where one of THREE_RESULTS opens: its path and line. */
function openThreeResults(ref: string | undefined) {
  const result = THREE_RESULTS.find((item) => item.ref === ref);
  return { path: `/work/${result?.path ?? ""}`, line: result?.line ?? 1, column: 1, length: 5 };
}

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
        return openThreeResults(ref);
      }
      case "initialize":
      case "shutdown":
      case "preview/get":
      case "index/status":
      case "index/rebuild":
      case "replace/plan":
      case "revision/text": {
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

test("a result still on screen while its search runs again opens with its item", async () => {
  const host = recordingUi();
  const scripted = scriptedBackend((method, { text, searchId, ref }) => {
    if (method === "query/parse") return { query: parsedQuery(text, true), completions: [] };
    if (method === "open/resolve") return openThreeResults(ref);
    if (text === "retry") {
      scripted.listeners.get("batch")?.({ searchId, items: THREE_RESULTS });
      return { ...NO_RESULTS, total: THREE_RESULTS.length };
    }
    return new Promise(() => {}); // the new search hasn't sent its first batch
  });
  const controller = newController(scripted.backend, host.ui);
  await controller.handle(queryChanged("retry", 1));
  void controller.handle(queryChanged("retry again", 2));
  await new Promise((resolve) => setImmediate(resolve));
  await controller.handle({ v: MESSAGE_VERSION, type: "result.open", payload: { ref: "ref-1", where: "current" } });
  assert.equal(host.openedItems[0], THREE_RESULTS[1]);
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
    repos: [{ repoId: "r1", name: "spelunk", tree: "ready", history: "ready" }],
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
    { v: MESSAGE_VERSION, type: "recent.remove", payload: {} },
    { v: MESSAGE_VERSION, type: "pinned.save", payload: { name: "x" } },
    { v: MESSAGE_VERSION, type: "pinned.save", payload: { query: "x", name: 5 } },
    { v: MESSAGE_VERSION, type: "pinned.remove", payload: { query: null } },
    { v: MESSAGE_VERSION, type: "pinned.watch", payload: { query: "msg:revert" } },
    { v: MESSAGE_VERSION, type: "pinned.watch", payload: { query: "msg:revert", watch: "yes" } },
    { v: MESSAGE_VERSION, type: "pinned.watch", payload: { query: 3, watch: true } },
    { v: MESSAGE_VERSION, type: "context.set", payload: {} },
    { v: MESSAGE_VERSION, type: "context.set", payload: { lines: 4 } },
    { v: MESSAGE_VERSION, type: "context.set", payload: { lines: -1 } },
    { v: MESSAGE_VERSION, type: "context.set", payload: { lines: 1.5 } },
    { v: MESSAGE_VERSION, type: "context.set", payload: { lines: "2" } },
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

test("searches and parses carry the open files, and Load more keeps the search's own", async () => {
  // @covers op:is
  let open = ["/work/a.py"];
  const sent: string[] = [];
  const { backend } = scriptedBackend((method, { text, openFiles }) => {
    sent.push(`${method} ${(openFiles ?? []).join(",")}`);
    if (method === "query/parse") return { query: parsedQuery(text, true), completions: [] };
    return { ...NO_RESULTS, nextCursor: "c" };
  });
  const controller = newController(backend, recordingUi().ui, { openFiles: () => open });
  await controller.handle(queryChanged("is:open retry", 1));
  open = ["/work/b.py"];
  await controller.handle({ v: MESSAGE_VERSION, type: "results.more", payload: { searchId: "s1", cursor: "c" } });
  assert.deepEqual(sent, ["query/parse /work/a.py", "search/start /work/a.py", "search/start /work/a.py"]);
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

test("a search that failed while the daemon was down runs again once it is back", async () => {
  const host = recordingUi();
  let down = true;
  const { backend, listeners } = scriptedBackend((method, { text }) => {
    if (method === "query/parse") return { query: parsedQuery(text, true), completions: [] };
    if (down) throw new Error("search daemon not ready");
    return NO_RESULTS;
  });
  const controller = newController(backend, host.ui);
  await controller.handle(queryChanged("retry", 1));
  assert.equal(host.payloads("search.done").at(-1)?.error, "search daemon not ready");
  down = false;
  listeners.get("state")?.("ok");
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(host.payloads("search.done").at(-1), { seq: 1, searchId: "s2", ...NO_RESULTS });
  listeners.get("state")?.("ok"); // a search that worked isn't run again
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(host.payloads("search.done").length, 2);
});

test("the preview shows as many lines around its match as ui.previewContextLines says", async () => {
  // @covers setting:ui.previewContextLines
  const asked: unknown[] = [];
  const { backend } = scriptedBackend((_method, params) => {
    asked.push(params.contextLines);
    return { ref: "r", preview: null, stale: true };
  });
  let lines = 3;
  const controller = newController(backend, recordingUi().ui, { previewContextLines: () => lines });
  await controller.handle({ v: MESSAGE_VERSION, type: "result.select", payload: { ref: "a" } });
  lines = 12; // read again on every preview, so a change applies at once
  await controller.handle({ v: MESSAGE_VERSION, type: "result.select", payload: { ref: "b" } });
  assert.deepEqual(asked, [3, 12]);
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
  assert.deepEqual(host.errors, [], "a stale result is greyed out, not reported");
});

test("a result the editor can't open says why, and neither closes the panel nor counts as opened", async () => {
  const host = recordingUi();
  const ui: Ui = {
    ...host.ui,
    openTarget: () => Promise.reject(new Error("cannot open /work/blob.bin: binary file")),
  };
  const { backend } = scriptedBackend((method) => {
    if (method === "open/resolve") return { path: "/work/blob.bin", line: 1, column: 1 };
    throw new Error(`unexpected request ${method}`);
  });
  const controller = newController(backend, ui);
  await controller.handle({ v: MESSAGE_VERSION, type: "result.open", payload: { ref: "r1", where: "current" } });
  assert.deepEqual(host.errors, ["Spelunk couldn't open that result: cannot open /work/blob.bin: binary file"]);
  assert.equal(host.closedPanels(), 0);
  assert.deepEqual(host.shown, []);

  const down = newController(scriptedBackend(() => Promise.reject(new Error("search daemon is stopped"))).backend, ui);
  await down.handle({ v: MESSAGE_VERSION, type: "result.open", payload: { ref: "r1", where: "current" } });
  assert.match(host.errors.at(-1) ?? "", /search daemon is stopped/);
});

test("a pasted query too long to keep is not saved for the next session, though the box keeps it", async () => {
  const host = recordingUi();
  const saved: PersistedState[] = [];
  const ui = { ...host.ui, saveState: (state: PersistedState) => saved.push(structuredClone(state)) };
  const controller = newController(parseOnlyBackend().backend, ui, {}, { text: "", recent: ["short"] });
  await controller.handle(queryChanged("retry", 1));
  await controller.handle(queryChanged("x".repeat(5_000_000), 2));
  assert.deepEqual(saved, [
    { text: "retry", recent: ["short"] },
    { text: "", recent: ["short"] },
  ]);
  assert.equal(controller.text.length, 5_000_000, "this session's box still holds it");
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

/**
 * A backend for watch tests: query/parse reads texts starting msg: or author:
 * as history queries, search/start finds nothing, and `fire` plays a
 * watch/matched announcement.
 */
function watchBackend() {
  const searched: string[] = [];
  const { backend, listeners } = scriptedBackend((method, { text }) => {
    if (method === "search/start") {
      searched.push(text);
      return NO_RESULTS;
    }
    const history = /^(msg|author):/.test(text);
    return { query: { ...parsedQuery(text, true), mode: history ? "history" : "workingTree" }, completions: [] };
  });
  const fire = async (match: WatchMatchedParams) => {
    listeners.get("watch")?.(match);
    await nextTurn();
    await nextTurn();
  };
  return { backend, fire, searched };
}

/** An announcement of commits b2 and a1 (newest first) for msg:revert in payments-api. */
const REVERTS: WatchMatchedParams = {
  query: "msg:revert",
  repoId: "r1",
  repoName: "payments-api",
  total: 2,
  commits: [
    { sha: "b2", subject: "Revert ledger batching", author: "Jason Kim" },
    { sha: "a1", subject: "Revert refunds retry", author: "Marta Ruiz" },
  ],
};

test("a watch's new commits count on its pin, pop up, and are marked New when its query runs", async () => {
  // @covers screen:query-watches msg:watch.state
  const host = recordingUi();
  const popups: string[] = [];
  const ui: Ui = {
    ...host.ui,
    showWatchMatch: (message) => (popups.push(message), Promise.resolve(undefined as typeof STOP_WATCHING | undefined)),
  };
  const pinned: PinnedQuery[] = [
    { query: "msg:revert", name: "Payments reverts", watch: true },
    { query: "sym:RetryPolicy" },
  ];
  const { backend, fire } = watchBackend();
  const controller = newController(backend, ui, { pinnedQueries: () => structuredClone(pinned) });
  controller.restore();
  await nextTurn();
  assert.deepEqual(host.payloads("watch.state").at(-1), { watchable: ["msg:revert"], unseen: [] });

  await fire(REVERTS);
  assert.deepEqual(popups, [
    "2 new commits match “Payments reverts” in payments-api: “Revert ledger batching” and 1 more.",
  ]);
  assert.deepEqual(host.payloads("watch.state").at(-1)?.unseen, [
    { query: "msg:revert", count: 2, shas: ["b2", "a1"] },
  ]);

  await controller.handle(queryChanged("msg:revert", 1));
  assert.deepEqual(host.payloads("search.done").at(-1)?.newCommits, ["b2", "a1"], "the results mark them New");
  await nextTurn();
  assert.deepEqual(host.payloads("watch.state").at(-1)?.unseen, [], "running the query clears the count");

  await controller.handle(queryChanged("msg:revert", 2));
  assert.equal(host.payloads("search.done").at(-1)?.newCommits, undefined, "seen now");
});

test("Load more keeps marking the new commits; another query never takes them", async () => {
  // @covers screen:query-watches
  const host = recordingUi();
  const { backend, fire } = watchBackend();
  const pinned = [{ query: "msg:revert", watch: true }];
  const controller = newController(backend, host.ui, { pinnedQueries: () => structuredClone(pinned) });
  await fire(REVERTS);
  await controller.handle(queryChanged("msg:revert author:jason", 1));
  assert.equal(host.payloads("search.done").at(-1)?.newCommits, undefined, "a different query");
  await controller.handle(queryChanged("msg:revert", 2));
  const done = host.payloads("search.done").at(-1);
  assert.ok(done);
  assert.deepEqual(done.newCommits, ["b2", "a1"]);
  await controller.handle({
    v: MESSAGE_VERSION,
    type: "results.more",
    payload: { searchId: done.searchId, cursor: "500" },
  });
  assert.deepEqual(host.payloads("search.done").at(-1)?.newCommits, ["b2", "a1"], "the next page is marked too");
});

test("Show opens the watched query, and Stop watching unwatches it and forgets what it found", async () => {
  // @covers screen:query-watches
  let pinned: PinnedQuery[] = [{ query: "msg:revert", name: "Payments reverts", watch: true }];
  const host = recordingUi();
  const shown: string[] = [];
  let answer: "Show" | "Stop watching" = "Show";
  const ui: Ui = {
    ...host.ui,
    showWatchMatch: () => Promise.resolve(answer),
    showQuery: (query) => void shown.push(query),
    savePinned: (list) => ((pinned = list), Promise.resolve()),
  };
  const { backend, fire } = watchBackend();
  const controller = newController(backend, ui, { pinnedQueries: () => structuredClone(pinned) });
  await fire(REVERTS);
  assert.deepEqual(shown, ["msg:revert"]);

  answer = "Stop watching";
  await fire({ ...REVERTS, commits: [{ sha: "c3", subject: "Revert again", author: "Jane Doe" }], total: 1 });
  assert.deepEqual(pinned, [{ query: "msg:revert", name: "Payments reverts" }], "unwatched, name kept");
  controller.restore();
  await nextTurn();
  assert.deepEqual(host.payloads("watch.state").at(-1)?.unseen, [], "what it found is forgotten");
});

test("an announcement for a query no longer watched is ignored, and watch.notify off only counts", async () => {
  // @covers screen:query-watches setting:watch.notify
  const host = recordingUi();
  const popups: string[] = [];
  const ui: Ui = {
    ...host.ui,
    showWatchMatch: (message) => (popups.push(message), Promise.resolve(undefined as typeof STOP_WATCHING | undefined)),
  };
  const { backend, fire } = watchBackend();
  let pinned: PinnedQuery[] = [{ query: "msg:revert" }];
  const controller = newController(backend, ui, {
    pinnedQueries: () => structuredClone(pinned),
    watchNotify: () => false,
  });
  await fire(REVERTS);
  assert.deepEqual(host.payloads("watch.state"), [], "not watched: nothing recorded");
  pinned = [{ query: "msg:revert", watch: true }];
  await fire(REVERTS);
  assert.deepEqual(popups, [], "no pop-up with watch.notify off");
  assert.deepEqual(host.payloads("watch.state").at(-1)?.unseen, [
    { query: "msg:revert", count: 2, shas: ["b2", "a1"] },
  ]);
  await fire(REVERTS);
  assert.equal(
    host.payloads("watch.state").at(-1)?.unseen[0]?.count,
    2,
    "the same commits again are not counted twice",
  );
  controller.restore();
});

test("whether a pin can be watched is asked again once the daemon restarts or finishes indexing", async () => {
  // @covers msg:watch.state
  const parsed: string[] = [];
  const { backend, listeners } = scriptedBackend((_method, { text }) => {
    parsed.push(text);
    return { query: { ...parsedQuery(text, true), mode: "history" }, completions: [] };
  });
  const host = recordingUi();
  const controller = newController(backend, host.ui, { pinnedQueries: () => [{ query: "msg:revert" }] });
  const restored = async () => {
    controller.restore();
    await nextTurn();
  };
  const progress = (history: IndexState) => {
    listeners.get("progress")?.({ repos: [{ repoId: "r1", name: "r", tree: "ready", history }] });
  };

  await restored();
  await restored();
  assert.equal(parsed.length, 1, "remembered");
  progress("indexing");
  await restored();
  assert.equal(parsed.length, 1, "still indexing: still remembered");
  progress("ready");
  await restored();
  assert.equal(parsed.length, 2, "asked again once indexing finished");
  listeners.get("state")?.("ok");
  await restored();
  assert.equal(parsed.length, 3, "asked again after a restart");
});

test("a watch.state that the pins changed under while it was asked about is dropped", async () => {
  // @covers msg:watch.state
  const waiting: (() => void)[] = [];
  const { backend } = scriptedBackend(async (_method, { text }) => {
    if (text === "msg:slow") await new Promise<void>((resolve) => waiting.push(resolve));
    return { query: { ...parsedQuery(text, true), mode: "history" }, completions: [] };
  });
  const host = recordingUi();
  let pinned: PinnedQuery[] = [{ query: "msg:slow" }];
  const controller = newController(backend, host.ui, { pinnedQueries: () => structuredClone(pinned) });
  controller.restore();
  await nextTurn();
  pinned = [{ query: "msg:revert" }];
  controller.restore();
  await nextTurn();
  for (const resolve of waiting.splice(0)) resolve();
  await nextTurn();
  assert.deepEqual(host.payloads("watch.state"), [{ watchable: ["msg:revert"], unseen: [] }]);
});

test("only a pinned query that searches history can be watched, and renaming keeps its watch", async () => {
  // @covers msg:pinned.watch screen:query-watches
  let pinned: PinnedQuery[] = [{ query: "sym:RetryPolicy" }, { query: "msg:revert" }];
  const saves: PinnedQuery[][] = [];
  const host = recordingUi();
  const ui: Ui = { ...host.ui, savePinned: (list) => (saves.push(list), (pinned = list), Promise.resolve()) };
  const { backend } = watchBackend();
  const controller = newController(backend, ui, { pinnedQueries: () => structuredClone(pinned) });
  const watch = (query: string, on: boolean) =>
    controller.handle({ v: MESSAGE_VERSION, type: "pinned.watch", payload: { query, watch: on } });

  await watch("sym:RetryPolicy", true);
  await watch("not pinned", true);
  assert.equal(saves.length, 0, "a query over current files, or one not pinned, can't be watched");
  await watch("msg:revert", true);
  assert.deepEqual(pinned[1], { query: "msg:revert", watch: true });
  await watch("msg:revert", true);
  assert.equal(saves.length, 1, "already watched: nothing saved");
  await controller.handle({
    v: MESSAGE_VERSION,
    type: "pinned.save",
    payload: { query: "msg:revert", name: "Reverts" },
  });
  assert.deepEqual(pinned[1], { query: "msg:revert", name: "Reverts", watch: true }, "renaming keeps the watch");
  await watch("msg:revert", false);
  assert.deepEqual(pinned[1], { query: "msg:revert", name: "Reverts" });
});

test("a watched query announces new commits through the real daemon", { skip }, async () => {
  // @covers screen:query-watches
  const root = mkdtempSync(join(tmpdir(), "us-watch-"));
  const git = (...args: string[]) => execFileSync("git", ["-C", root, ...args], { stdio: "pipe" });
  git("init", "--quiet");
  git("config", "user.email", "me@example.com");
  git("config", "user.name", "Me");
  git("config", "commit.gpgsign", "false");
  writeFileSync(join(root, "ledger.py"), "batch = True\n");
  git("add", "--all");
  git("commit", "--quiet", "-m", "Batch ledger writes");
  const daemon = newTestDaemon([makeRoot(root, "payments-api")], { watches: () => ["msg:revert"] });
  const matches: WatchMatchedParams[] = [];
  daemon.on("watch", (match) => void matches.push(match));
  await daemon.start();
  try {
    await waitFor("history read", async () => {
      const { repos } = await daemon.request("index/status", {});
      return repos.every((repo) => repo.history === "ready");
    });
    writeFileSync(join(root, "ledger.py"), "batch = False\n");
    git("add", "--all");
    git(
      "-c",
      "user.name=Jason Kim",
      "-c",
      "user.email=jason@example.com",
      "commit",
      "--quiet",
      "-m",
      "Revert ledger batching",
    );
    await waitFor("a watch/matched announcement", () => matches.length > 0, 20_000);
    const [first] = matches;
    assert.ok(first);
    assert.equal(first.query, "msg:revert");
    assert.equal(first.repoName, "payments-api");
    assert.deepEqual(
      first.commits.map((commit) => [commit.subject, commit.author]),
      [["Revert ledger batching", "Jason Kim"]],
    );
    daemon.setWatches([]);
    writeFileSync(join(root, "ledger.py"), "batch = 2\n");
    git("add", "--all");
    git(
      "-c",
      "user.name=Jason Kim",
      "-c",
      "user.email=jason@example.com",
      "commit",
      "--quiet",
      "-m",
      "Revert it again",
    );
    await waitFor("the history to catch up", async () => {
      const { query } = await daemon.request("query/parse", { text: "msg:again", cursor: 9 });
      return query.root !== null;
    });
    await new Promise((resolve) => setTimeout(resolve, 7000)); // past the next HEAD poll
    assert.equal(matches.length, 1, "nothing is announced once no query is watched");
  } finally {
    await daemon.stop();
  }
});

/** A plan of one match, in `path`. */
const planAt = (path: string): ReplacePlan => ({
  matches: 1,
  truncated: false,
  files: [{ repoId: "r1", path, file: `/work/${path}`, lines: [{ line: 1, text: "retry()", edits: [] }] }],
});

test("Replace all applies nothing when the matches moved since the preview, though their count is the same", async () => {
  const host = recordingUi();
  let path = "a.py";
  const { backend } = scriptedBackend((method) => {
    if (method === "replace/plan") return planAt(path);
    throw new Error(`unexpected request ${method}`);
  });
  const controller = newController(backend, host.ui);
  const message = { text: "retry", replacement: "again" };
  await controller.handle({ v: MESSAGE_VERSION, type: "replace.preview", payload: { seq: 1, ...message } });
  path = "b.py"; // a pull deleted a.py's match and added one in b.py
  await controller.handle({ v: MESSAGE_VERSION, type: "replace.apply", payload: { seq: 2, ...message, matches: 1 } });
  assert.equal(host.payloads("replace.done").at(-1)?.changed, true);
  assert.equal(host.payloads("replace.plan").at(-1)?.plan?.files[0]?.path, "b.py", "the new preview");
});
