// Host side of the search panel: turns panel messages into daemon calls and
// streams results back. It has no vscode import: extension.ts supplies the
// Ui, and tests drive the controller against the real daemon in plain Node.
import type { DaemonState } from "./daemon";
import { CancelSource, RpcError, errorMessage } from "./jsonRpc";
import {
  type Completion,
  ErrorCodes,
  type HostToWebview,
  type IndexState,
  type IndexStatusResult,
  type OpenTarget,
  type OpenWhere,
  type ParsedQuery,
  type PinnedQuery,
  type QueryChangedMessage,
  type ReplaceApplyMessage,
  type ReplacePlan,
  type ReplacePreviewMessage,
  type ResultItem,
  type RpcRequests,
  type SearchBatchParams,
  type UiSettings,
  type WatchMatchedParams,
} from "./protocol.gen";
import { checkReplace, type DocumentLines, type LineEdit } from "./replaceEdits";
import { pinnedEntry } from "./settings";
import { UnseenWatches, watchMessage } from "./watches";
import { type GuardedBy, messageGuard, type WebviewMessage } from "./webviewMessages";

/** Longer queries (a pasted file, say) are searched but not kept in the recent list, which lives in globalState. */
const MAX_REMEMBERED_QUERY_LENGTH = 1000;

/**
 * The box without the word the suggestions would replace, when the cursor
 * is in it; undefined when there are no suggestions or nothing else is left.
 * Offsets are UTF-16, like JavaScript strings.
 */
export function textWithoutCompletedWord(text: string, cursor: number, completions: Completion[]): string | undefined {
  const span = completions[0]?.insert.edits[0]?.span;
  if (!span || cursor < span.start || cursor > span.end) return undefined;
  const before = text.slice(0, span.start).trimEnd();
  const after = text.slice(span.end).trimStart();
  const rest = before && after ? `${before} ${after}` : before || after;
  return rest || undefined;
}

/** "1 match" / "3 matches". */
function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

/** The queries of the watched pins. */
function watchedOf(pinned: PinnedQuery[]): string[] {
  return pinned.filter((entry) => entry.watch).map((entry) => entry.query);
}

/** Whether a parsed query has errors; such a query is not searched. */
function hasErrors(query: ParsedQuery): boolean {
  return query.diagnostics.some((diagnostic) => diagnostic.severity === "error");
}

/** Whether a repo's index is still being built. */
function isIndexing(state: IndexState): boolean {
  return state === "queued" || state === "indexing";
}

/** What the controller needs from the daemon: requests, streamed batches, and its state. */
export interface Backend {
  request<M extends keyof RpcRequests>(
    method: M,
    params: RpcRequests[M][0],
    cancel?: CancelSource,
  ): Promise<RpcRequests[M][1]>;
  on(event: "batch", listener: (batch: SearchBatchParams) => void): unknown;
  on(event: "progress", listener: (progress: IndexStatusResult) => void): unknown;
  on(event: "watch", listener: (match: WatchMatchedParams) => void): unknown;
  on(event: "state", listener: (state: DaemonState) => void): unknown;
}

/** The pop-up button that opens a watch's query. */
export const SHOW_MATCHES = "Show";
/** The pop-up button that stops a watch. */
export const STOP_WATCHING = "Stop watching";

/** What the controller needs from VS Code. */
export interface Ui {
  post<T extends keyof HostToWebview>(type: T, payload: HostToWebview[T]): void;
  /**
   * Opens a resolved result; `preview` opens it in a preview editor that the next result replaces. Resolves to
   * whether it opened anything: a commit or a file at a revision can't open without its result item.
   */
  openTarget(target: OpenTarget, where: OpenWhere, preview: boolean, item?: ResultItem): Promise<boolean>;
  hidePanel(): void;
  openHelp(): void;
  openSettings(): void;
  restartDaemon(): void;
  setContext(key: string, value: boolean): void;
  saveState(state: PersistedState): void;
  /** Saves the pinned queries to spelunk.ui.pinnedQueries; resolves once the setting says so, and never rejects. */
  savePinned(pinned: PinnedQuery[]): Promise<void>;
  /** Saves spelunk.results.contextLines; the settings change then runs the query again. */
  saveContextLines(lines: number): void;
  /** Says which result is open, `position` of `total` (1-based), once one was opened from search or F4. */
  showOpened(position: number, total: number): void;
  /** Reads a file as the editor has it, unsaved changes included; undefined when it can't. */
  readDocument(file: string): Promise<DocumentLines | undefined>;
  /** Applies edits as one undoable edit, leaving the files unsaved; resolves to whether it was applied. */
  applyEdits(edits: LineEdit[], label: string): Promise<boolean>;
  /** Saves files the editor has open. */
  saveFiles(files: string[]): void;
  /** Pops up what a watch found; resolves to the button clicked, if any. */
  showWatchMatch(message: string): Promise<typeof SHOW_MATCHES | typeof STOP_WATCHING | undefined>;
  /** Opens the search panel on `query`, which then runs. */
  showQuery(query: string): void;
}

/** Survives window reloads (globalState): the last query and recent queries. */
export interface PersistedState {
  text: string;
  recent: string[];
}

/** Settings the controller reads each time it needs them, so changes apply at once. */
export interface ControllerOptions {
  recentLimit(): number;
  /** spelunk.ui.pinnedQueries, checked. */
  pinnedQueries(): PinnedQuery[];
  closeOnOpen(): boolean;
  /** spelunk.ui.previewContextLines: lines above and below the match in a file preview. */
  previewContextLines(): number;
  uiSettings(): UiSettings;
  /** The absolute paths of the files open in editor tabs, for is:open. */
  openFiles(): string[];
  /** spelunk.watch.notify: whether a watch's new commits pop up. */
  watchNotify(): boolean;
  /** What query watches found that hasn't been looked at; the caller keeps it, to save it. None found when left out. */
  unseen?: UnseenWatches;
}

/** A query/parse answer, plus the text to search while a word is being completed. */
interface ParsedForSearch {
  query: ParsedQuery;
  completions: Completion[];
  /** Set when the search leaves out the word being completed. */
  searchText: string | undefined;
}

/** The search whose results the panel shows. Load more fetches further pages of it. */
interface RunningSearch {
  id: string;
  /** The query.changed seq it answers; the webview drops results for older ones. */
  seq: number;
  text: string;
  /** The files open in the editor when it started; Load more keeps them, so pages agree. */
  openFiles: string[];
  cancel: CancelSource;
  /** The commits a watch found for this query since it last ran, which the panel marks New on every page. */
  newCommits: string[];
  /** Set when its first page failed, so the daemon coming back runs it again. */
  failed?: boolean;
}

/** The help and welcome pages' messages, which extension.ts handles. */
const fromHelpOrWelcome = messageGuard(["help.try", "welcome.choose", "welcome.shortcut", "welcome.start"]);
type HelpOrWelcomeMessage = GuardedBy<typeof fromHelpOrWelcome>;
/** The empty box's messages about recent and pinned queries, which handleQueryList handles. */
const fromQueryList = messageGuard(["recent.remove", "pinned.save", "pinned.remove", "pinned.watch"]);
type QueryListMessage = GuardedBy<typeof fromQueryList>;
/** The Replace row's messages, which handleReplace handles. */
const fromReplaceRow = messageGuard(["replace.preview", "replace.apply", "replace.save"]);
type ReplaceMessage = GuardedBy<typeof fromReplaceRow>;

/** Turns panel messages into daemon calls; one per window. */
export class SearchController {
  /** Openable results of the current search, for F4 / Shift+F4. */
  results: ResultItem[] = [];
  /** The results `results` last replaced, which the panel shows until the next search's first batch. */
  private replacedResults: ResultItem[] = [];
  private state: PersistedState;
  /** The newest query.changed seq; answers to older ones are dropped. */
  private latestSeq = 0;
  private search: RunningSearch | undefined;
  /** Searches started so far; numbers the search ids ("s1", "s2", …). */
  private searchesStarted = 0;
  /** Position in `results` for F4 stepping; -1 before the first step. */
  private stepIndex = -1;
  /** The search whose search.done was posted last. */
  private finishedSearchId: string | undefined;
  /** Repos whose working-tree index is ready, from the last index/progress. */
  private readyRepos = new Set<string>();
  /** The last index/progress, which a panel opened later needs: the daemon only reports changes. */
  private lastProgress: IndexStatusResult | undefined;
  /** The files the last replace changed, which Save saves. */
  private replacedFiles: string[] = [];
  /**
   * Whether each query text searches history (and so can be watched), as
   * the daemon parsed it. Forgotten when the daemon restarts or finishes
   * indexing, either of which can change how it parses a query.
   */
  private readonly searchesHistoryByText = new Map<string, boolean>();
  /** watch.state posts started so far; one that finishes after a later one started isn't posted. */
  private watchStatePosts = 0;
  /** The last change to the pinned list; the next waits for it, so neither overwrites the other. */
  private pinnedUpdate: Promise<void> = Promise.resolve();

  /** What query watches found that hasn't been looked at. */
  private readonly watches: UnseenWatches;

  /** Listens for the backend's batches, indexing progress and watches; `initial` is the state saved last session. */
  constructor(
    private readonly backend: Backend,
    private readonly ui: Ui,
    private readonly options: ControllerOptions,
    initial?: PersistedState,
  ) {
    this.state = initial ?? { text: "", recent: [] };
    this.watches = options.unseen ?? new UnseenWatches(undefined);
    backend.on("batch", (batch) => {
      this.onBatch(batch);
    });
    backend.on("progress", (progress) => {
      this.onIndexProgress(progress);
    });
    backend.on("watch", (match) => {
      void this.onWatchMatched(match);
    });
    backend.on("state", (state) => {
      if (state === "ok") this.onDaemonReady();
    });
  }

  /** The query box text, as last reported by the panel. */
  get text(): string {
    return this.state.text;
  }

  /** The text of the search whose results `results` holds, once that search has finished. */
  get finishedSearchText(): string | undefined {
    const search = this.search;
    return search && search.id === this.finishedSearchId ? search.text : undefined;
  }

  /**
   * Sends the box text, recent and pinned queries, settings and each repo's indexing to a
   * (re)opened panel. A given query replaces the text.
   */
  restore(query?: string): void {
    if (query !== undefined) this.state.text = query;
    this.ui.post("state.restore", {
      text: this.state.text,
      recent: this.state.recent,
      pinned: this.options.pinnedQueries(),
      settings: this.options.uiSettings(),
    });
    if (this.lastProgress) this.ui.post("index.status", this.lastProgress);
    void this.postWatchState();
  }

  /**
   * Sends changed settings and pinned queries to the open panel, leaving
   * the box's text alone: the text here lags what is being typed by the
   * typing delay, and sending it would undo the newest keystrokes.
   */
  settingsChanged(): void {
    this.ui.post("state.restore", {
      recent: this.state.recent,
      pinned: this.options.pinnedQueries(),
      settings: this.options.uiSettings(),
    });
    void this.postWatchState();
  }

  /** Handles one message from the search panel. */
  async handle(message: WebviewMessage): Promise<void> {
    if (fromHelpOrWelcome(message)) return;
    if (fromQueryList(message)) await this.handleQueryList(message);
    else await (fromReplaceRow(message) ? this.handleReplace(message) : this.handleSearch(message));
  }

  /** Handles the search panel's messages other than the Replace row's and the query list's. */
  private async handleSearch(
    message: Exclude<WebviewMessage, HelpOrWelcomeMessage | ReplaceMessage | QueryListMessage>,
  ): Promise<void> {
    switch (message.type) {
      case "ready": {
        // A new webview counts seq from zero again, so a search still running
        // for the old one could pass for one of its own: it is dropped.
        this.latestSeq = 0;
        this.search?.cancel.cancel();
        this.restore();
        return;
      }
      case "query.changed": {
        await this.onQueryChanged(message.payload);
        return;
      }
      case "result.select": {
        await this.onSelect(message.payload.ref);
        return;
      }
      case "result.open": {
        await this.onOpen(message.payload.ref, message.payload.where);
        return;
      }
      case "results.more": {
        await this.onLoadMore(message.payload.searchId, message.payload.cursor);
        return;
      }
      case "panel.close": {
        this.rememberQuery();
        this.ui.hidePanel();
        return;
      }
      case "help.open": {
        this.ui.openHelp();
        return;
      }
      case "settings.open": {
        this.ui.openSettings();
        return;
      }
      case "daemon.restart": {
        this.ui.restartDaemon();
        return;
      }
      case "context.set": {
        this.setContextLines(message.payload.lines);
        return;
      }
    }
  }

  /** Handles the empty box's messages: removing a recent query, and pinning, naming and watching. */
  private async handleQueryList(message: QueryListMessage): Promise<void> {
    switch (message.type) {
      case "recent.remove": {
        this.removeRecent(message.payload.query);
        return;
      }
      case "pinned.save": {
        await this.pin(message.payload);
        return;
      }
      case "pinned.remove": {
        await this.unpin(message.payload.query);
        return;
      }
      case "pinned.watch": {
        await this.setWatch(message.payload.query, message.payload.watch);
        return;
      }
    }
  }

  /** Handles the Replace row's messages. */
  private async handleReplace(message: ReplaceMessage): Promise<void> {
    switch (message.type) {
      case "replace.preview": {
        await this.onReplacePreview(message.payload);
        return;
      }
      case "replace.apply": {
        await this.onReplaceApply(message.payload);
        return;
      }
      case "replace.save": {
        this.ui.saveFiles(this.replacedFiles);
        return;
      }
    }
  }

  /** F4 / Shift+F4: opens the next or previous result without the panel. */
  async step(direction: 1 | -1): Promise<void> {
    if (this.results.length === 0) return;
    this.stepIndex = (this.stepIndex + direction + this.results.length) % this.results.length;
    const result = this.results[this.stepIndex];
    if (result && (await this.openRef(result.ref, "current"))) {
      this.ui.showOpened(this.stepIndex + 1, this.results.length);
    }
  }

  /** Puts the current query at the top of the recent list. */
  rememberQuery(): void {
    const text = this.state.text.trim();
    const limit = this.options.recentLimit();
    if (!text || text.length > MAX_REMEMBERED_QUERY_LENGTH || !(limit > 0)) return;
    this.state.recent = [text, ...this.state.recent.filter((query) => query !== text)].slice(0, limit);
    this.ui.saveState(this.state);
  }

  /** Takes a query off the recent list. */
  private removeRecent(query: string): void {
    const recent = this.state.recent.filter((kept) => kept !== query);
    if (recent.length === this.state.recent.length) return;
    this.state.recent = recent;
    this.ui.saveState(this.state);
  }

  /**
   * Pins a query at the end of the pinned list, or renames it if it is
   * pinned already, keeping its watch. A blank name is none.
   */
  private pin({ query, name }: PinnedQuery): Promise<void> {
    return this.updatePinned((pinned) => {
      const index = pinned.findIndex((kept) => kept.query === query.trim());
      const entry = pinnedEntry(query, name, pinned[index]?.watch);
      if (!entry) return;
      if (index === -1) pinned.push(entry);
      else if (pinned[index]?.name === entry.name) return;
      else pinned[index] = entry;
      return pinned;
    });
  }

  /**
   * Starts or stops watching a pinned query for new commits. Only a query
   * that searches history can be watched; stopping forgets what it found.
   */
  private async setWatch(query: string, watch: boolean): Promise<void> {
    if (watch && !(await this.searchesHistory(query))) return;
    await this.updatePinned((pinned) => {
      const index = pinned.findIndex((entry) => entry.query === query);
      const entry = pinned[index];
      if (!entry || (entry.watch ?? false) === watch) return;
      pinned[index] = pinnedEntry(entry.query, entry.name, watch) ?? entry;
      this.watches.keepOnly(watchedOf(pinned));
      return pinned;
    });
  }

  /** Saves spelunk.results.contextLines, unless it already says `lines`. */
  private setContextLines(lines: number): void {
    if (lines !== this.options.uiSettings().contextLines) this.ui.saveContextLines(lines);
  }

  /** Unpins a query, forgetting what its watch found. */
  private unpin(query: string): Promise<void> {
    return this.updatePinned((pinned) => {
      const kept = pinned.filter((entry) => entry.query !== query);
      if (kept.length === pinned.length) return;
      this.watches.keepOnly(watchedOf(kept));
      return kept;
    });
  }

  /**
   * Changes the pinned list and saves it. Changes run one at a time, each
   * reading the list the one before saved: panel messages aren't awaited
   * one by one, so two changes reading the list at once would each save
   * their own copy, and the second would undo the first. `change` returns
   * the new list, or undefined to leave it as it is.
   */
  private updatePinned(change: (pinned: PinnedQuery[]) => PinnedQuery[] | undefined): Promise<void> {
    const update = this.pinnedUpdate.then(async () => {
      const pinned = change(this.options.pinnedQueries());
      if (pinned) await this.ui.savePinned(pinned);
    });
    this.pinnedUpdate = update.catch(() => {}); // the caller hears of a failure; the next change still runs
    return update;
  }

  /**
   * Tells the panel which pinned queries can be watched and what each watch
   * found. Asking the daemon takes a while, and the pins can change
   * meanwhile, so only the latest post goes out.
   */
  private async postWatchState(): Promise<void> {
    const post = ++this.watchStatePosts;
    const pinned = this.options.pinnedQueries();
    this.watches.keepOnly(watchedOf(pinned));
    const searchesHistory = await Promise.all(pinned.map((entry) => this.searchesHistory(entry.query)));
    if (post !== this.watchStatePosts) return;
    const watchable = pinned.filter((_entry, index) => searchesHistory[index]).map((entry) => entry.query);
    this.ui.post("watch.state", { watchable, unseen: this.watches.list() });
  }

  /** Whether a query searches history without errors, so it can be watched; remembered per text. */
  private async searchesHistory(text: string): Promise<boolean> {
    const known = this.searchesHistoryByText.get(text);
    if (known !== undefined) return known;
    try {
      const { query } = await this.backend.request("query/parse", { text, cursor: text.length });
      const history = query.mode === "history" && query.root !== null && !hasErrors(query);
      this.searchesHistoryByText.set(text, history);
      return history;
    } catch {
      return false; // the daemon is down; asked again next time
    }
  }

  /**
   * A watch found new commits: they count on the pin until its query runs,
   * and, unless spelunk.watch.notify is off, pop up with Show and Stop
   * watching. An announcement for a query no longer watched is ignored.
   */
  private async onWatchMatched(match: WatchMatchedParams): Promise<void> {
    const pin = this.options.pinnedQueries().find((entry) => entry.query === match.query && entry.watch);
    if (!pin) return;
    const found = this.watches.record(match);
    if (found.count === 0) return;
    void this.postWatchState();
    if (!this.options.watchNotify()) return;
    const choice = await this.ui.showWatchMatch(watchMessage(pin.name ?? pin.query, match.repoName, found));
    if (choice === SHOW_MATCHES) this.ui.showQuery(pin.query);
    else if (choice === STOP_WATCHING) await this.setWatch(pin.query, false);
  }

  /** The commits a watch found for `text` since it last ran, now that it runs again; empty for any other query. */
  private takeUnseen(text: string): string[] {
    const shas = this.watches.take(text.trim());
    if (shas.length > 0) void this.postWatchState();
    return shas;
  }

  /** Parses the box, posts the parse, then searches it unless it has errors or is empty. */
  private async onQueryChanged(change: QueryChangedMessage): Promise<void> {
    if (!(change.seq >= this.latestSeq)) return; // older, or not a number (which would poison latestSeq)
    this.latestSeq = change.seq;
    this.state.text = change.text;
    this.ui.saveState(this.state);
    const parsed = await this.parseForSearch(change);
    if (!parsed || change.seq !== this.latestSeq) return; // failed, or a newer keystroke arrived while parsing
    const { query, completions, searchText } = parsed;
    this.ui.post("parse.result", {
      seq: change.seq,
      query,
      completions,
      ...(searchText === undefined ? {} : { searchText }),
    });
    // With errors, the panel keeps showing the last good results.
    if (hasErrors(query)) return;
    this.search?.cancel.cancel();
    if (query.root === null) {
      this.search = undefined;
      this.setResults([]);
      return;
    }
    await this.startSearch(searchText ?? change.text, change.seq);
  }

  /** Parses the box; on failure posts the error as the search's outcome and returns undefined. */
  private async parseForSearch(change: QueryChangedMessage): Promise<ParsedForSearch | undefined> {
    const { text, cursor, seq } = change;
    try {
      const openFiles = this.options.openFiles();
      const { query, completions } = await this.backend.request("query/parse", { text, cursor, openFiles });
      // Esc or Enter on the suggestions asks to search the box exactly as typed.
      const searchText =
        change.asTyped === true ? undefined : await this.searchableWithoutWordBeingCompleted(text, cursor, completions);
      return { query, completions, searchText };
    } catch (error) {
      this.postSearchFailed(seq, "", error);
      return undefined;
    }
  }

  /**
   * While suggestions are offered for the word at the cursor, the search
   * leaves that half-typed word out, so the results stay on what is already
   * complete. Returns undefined to search the whole box: nothing is being
   * completed, or the rest would not be a query on its own.
   */
  private async searchableWithoutWordBeingCompleted(
    text: string,
    cursor: number,
    completions: Completion[],
  ): Promise<string | undefined> {
    const rest = textWithoutCompletedWord(text, cursor, completions);
    if (rest === undefined) return undefined;
    const { query } = await this.backend.request("query/parse", { text: rest, cursor: rest.length });
    return query.root !== null && !hasErrors(query) ? rest : undefined;
  }

  /** Starts a new search for `text`, replacing the results of the previous one. */
  private async startSearch(text: string, seq: number): Promise<void> {
    this.searchesStarted++;
    const search: RunningSearch = {
      id: `s${this.searchesStarted}`,
      seq,
      text,
      openFiles: this.options.openFiles(),
      cancel: new CancelSource(),
      newCommits: [],
    };
    this.search = search;
    this.setResults([]);
    await this.requestPage(search);
  }

  /** Load more: fetches the page after `pageCursor` of the search on screen, keeping its results. */
  private async onLoadMore(searchId: string, pageCursor: string): Promise<void> {
    const current = this.search;
    if (current?.id !== searchId || current.cancel.cancelled) return;
    const search: RunningSearch = { ...current, cancel: new CancelSource() };
    this.search = search;
    await this.requestPage(search, pageCursor);
  }

  /**
   * Asks the daemon for one page of `search` (its items arrive as batches)
   * and posts search.done, unless a newer search has replaced it meanwhile.
   */
  private async requestPage(search: RunningSearch, pageCursor?: string): Promise<void> {
    const { id: searchId, text, seq, openFiles, cancel } = search;
    const params =
      pageCursor === undefined ? { searchId, text, openFiles } : { searchId, text, openFiles, cursor: pageCursor };
    try {
      const result = await this.backend.request("search/start", params, cancel);
      if (this.search !== search || cancel.cancelled) return;
      this.finishedSearchId = searchId;
      if (pageCursor === undefined) search.newCommits = this.takeUnseen(text);
      const { newCommits } = search;
      this.ui.post("search.done", { seq, searchId, ...result, ...(newCommits.length > 0 ? { newCommits } : {}) });
    } catch (error) {
      if (error instanceof RpcError && error.code === ErrorCodes.RequestCancelled) return;
      if (this.search !== search) return;
      search.failed = pageCursor === undefined;
      this.postSearchFailed(seq, searchId, error);
    }
  }

  /**
   * Runs the current search again when a repo finishes indexing: a search
   * typed while it was indexing could not include its results. Once no repo
   * is indexing, which queries search history is asked again.
   */
  private onIndexProgress(progress: IndexStatusResult): void {
    this.lastProgress = progress;
    if (!progress.repos.some((repo) => isIndexing(repo.tree) || isIndexing(repo.history))) {
      this.searchesHistoryByText.clear();
    }
    const ready = progress.repos.filter((repo) => repo.tree === "ready").map((repo) => repo.repoId);
    const newlyReady = ready.some((id) => !this.readyRepos.has(id));
    this.readyRepos = new Set(ready);
    const search = this.search;
    if (!newlyReady || !search || search.cancel.cancelled) return;
    search.cancel.cancel();
    void this.startSearch(search.text, search.seq);
  }

  /**
   * The daemon is (back) up: which queries search history is asked again,
   * and a search that failed while it was down (it crashed, or hadn't
   * started yet) runs again, rather than showing that error until the
   * query is edited.
   */
  private onDaemonReady(): void {
    this.searchesHistoryByText.clear();
    const search = this.search;
    if (!search?.failed || search.cancel.cancelled) return;
    void this.startSearch(search.text, search.seq);
  }

  private postSearchFailed(seq: number, searchId: string, error: unknown): void {
    this.ui.post("search.done", {
      seq,
      searchId,
      total: 0,
      truncated: false,
      hidden: [],
      ms: 0,
      error: errorMessage(error),
    });
  }

  /** Relays a batch of the current search to the panel and remembers its items for F4. */
  private onBatch(batch: SearchBatchParams): void {
    const search = this.search;
    if (batch.searchId !== search?.id || search.cancel.cancelled) return;
    this.results.push(...batch.items);
    this.ui.setContext("spelunk.hasResults", this.results.length > 0);
    this.ui.post("search.batch", { seq: search.seq, searchId: search.id, items: batch.items });
  }

  private setResults(items: ResultItem[]): void {
    // The panel shows the replaced results until the new search's first batch, so they can still be opened.
    if (this.results.length > 0) this.replacedResults = this.results;
    this.results = items;
    this.stepIndex = -1;
    this.ui.setContext("spelunk.hasResults", items.length > 0);
  }

  /** Every edit replacing the matches of `text` with `replacement` would make, from the daemon. */
  private planReplace(text: string, replacement: string): Promise<ReplacePlan> {
    return this.backend.request("replace/plan", { text, replacement, openFiles: this.options.openFiles() });
  }

  /** The Replace row's preview: what Replace all would change, or why it can't. */
  private async onReplacePreview({ seq, text, replacement }: ReplacePreviewMessage): Promise<void> {
    try {
      this.ui.post("replace.plan", { seq, plan: await this.planReplace(text, replacement) });
    } catch (error) {
      this.ui.post("replace.plan", { seq, error: errorMessage(error) });
    }
  }

  /**
   * Replace all: plans the replace again and applies it only if it still
   * has the matches the preview showed; otherwise the panel gets the new
   * preview instead. Files edited since they were indexed are skipped.
   */
  private async onReplaceApply({ seq, text, replacement, matches }: ReplaceApplyMessage): Promise<void> {
    const nothing = { seq, replaced: 0, files: 0, skipped: [] };
    try {
      const plan = await this.planReplace(text, replacement);
      if (plan.truncated || plan.matches !== matches) {
        this.ui.post("replace.plan", { seq, plan });
        this.ui.post("replace.done", { ...nothing, changed: true });
        return;
      }
      const checked = await checkReplace(plan, (file) => this.ui.readDocument(file));
      const label = `Replace ${plural(checked.edits.length, "match", "matches")}`;
      if (checked.edits.length > 0 && !(await this.ui.applyEdits(checked.edits, label))) {
        throw new Error("VS Code didn't apply the replace; nothing changed.");
      }
      this.replacedFiles = checked.files;
      this.rememberQuery();
      this.ui.post("replace.done", {
        seq,
        replaced: checked.edits.length,
        files: checked.files.length,
        skipped: checked.skipped,
      });
    } catch (error) {
      this.ui.post("replace.done", { ...nothing, error: errorMessage(error) });
    }
  }

  /** Fetches a result's preview; a stale ref gets an empty preview marked stale. */
  private async onSelect(ref: string): Promise<void> {
    try {
      const preview = await this.backend.request("preview/get", {
        ref,
        contextLines: this.options.previewContextLines(),
      });
      this.ui.post("preview.result", { ref, preview });
    } catch (error) {
      const stale = error instanceof RpcError && error.code === ErrorCodes.RefStale;
      this.ui.post("preview.result", { ref, preview: null, stale });
    }
  }

  /** Opens a result from the panel, remembers the query and, if set to, hides the panel. */
  private async onOpen(ref: string, where: OpenWhere): Promise<void> {
    const index = this.results.findIndex((result) => result.ref === ref);
    if (index !== -1) this.stepIndex = index;
    if (!(await this.openRef(ref, where))) return;
    if (index !== -1) this.ui.showOpened(index + 1, this.results.length);
    this.rememberQuery();
    // Opening to the side puts the file next to the results, so they stay.
    if (where !== "side" && this.options.closeOnOpen()) this.ui.hidePanel();
  }

  /** Opens a result; a stale ref greys its row out instead. Returns whether it opened. */
  private async openRef(ref: string, where: OpenWhere): Promise<boolean> {
    try {
      const target = await this.backend.request("open/resolve", { ref });
      const item =
        this.results.find((result) => result.ref === ref) ?? this.replacedResults.find((result) => result.ref === ref);
      return await this.ui.openTarget(target, where, this.options.uiSettings().preview, item);
    } catch (error) {
      if (error instanceof RpcError && error.code === ErrorCodes.RefStale) {
        this.ui.post("preview.result", { ref, preview: null, stale: true });
      }
      return false;
    }
  }
}
