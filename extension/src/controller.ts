// Host side of the search panel: turns panel messages into daemon calls and
// streams results back. It has no vscode import: extension.ts supplies the
// Ui, and tests drive the controller against the real daemon in plain Node.
import { CancelSource, RpcError } from "./jsonRpc";
import {
  type Completion,
  type Envelope,
  ErrorCodes,
  type HostToWebview,
  type IndexStatusResult,
  type OpenTarget,
  type OpenWhere,
  type ParsedQuery,
  type QueryChangedMsg as QueryChangedMessage,
  type ResultItem,
  type RpcRequests,
  type SearchBatchParams,
  type UiSettings,
  type WebviewToHost,
} from "./protocol.gen";

/** Lines of context above and below the match in a file preview. */
const DEFAULT_PREVIEW_CONTEXT_LINES = 7;

/** The `v` of every message between the extension host and a webview. */
export const MESSAGE_VERSION: Envelope["v"] = 1;

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

/** Whether a parsed query has errors; such a query is not searched. */
function hasErrors(query: ParsedQuery): boolean {
  return query.diagnostics.some((diagnostic) => diagnostic.severity === "error");
}

/** What the controller needs from the daemon: requests plus streamed batches. */
export interface Backend {
  request<M extends keyof RpcRequests>(
    method: M,
    params: RpcRequests[M][0],
    cancel?: CancelSource,
  ): Promise<RpcRequests[M][1]>;
  on(event: "batch", listener: (batch: SearchBatchParams) => void): unknown;
  on(event: "progress", listener: (progress: IndexStatusResult) => void): unknown;
}

/** What the controller needs from VS Code. */
export interface Ui {
  post<T extends keyof HostToWebview>(type: T, payload: HostToWebview[T]): void;
  openTarget(target: OpenTarget, where: OpenWhere, item?: ResultItem): Promise<void>;
  hidePanel(): void;
  openHelp(): void;
  openSettings(): void;
  restartDaemon(): void;
  setContext(key: string, value: boolean): void;
  saveState(state: PersistedState): void;
}

/** Survives window reloads (globalState): the last query and recent queries. */
export interface PersistedState {
  text: string;
  recent: string[];
}

/** Settings the controller reads each time it needs them, so changes apply at once. */
export interface ControllerOptions {
  recentLimit(): number;
  closeOnOpen(): boolean;
  uiSettings(): UiSettings;
  previewContextLines?: number;
}

/** A message from the search panel webview, discriminated by `type`. */
export type WebviewMessage = {
  [K in keyof WebviewToHost]: { v: typeof MESSAGE_VERSION; type: K; payload: WebviewToHost[K] };
}[keyof WebviewToHost];

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
  cancel: CancelSource;
}

/** Turns panel messages into daemon calls; one per window. */
export class SearchController {
  /** Openable results of the current search, for F4 / Shift+F4. */
  results: ResultItem[] = [];
  private state: PersistedState;
  /** The newest query.changed seq; answers to older ones are dropped. */
  private latestSeq = 0;
  private search: RunningSearch | undefined;
  /** Searches started so far; numbers the search ids ("s1", "s2", …). */
  private searchesStarted = 0;
  /** Position in `results` for F4 stepping; -1 before the first step. */
  private stepIndex = -1;
  /** Repos whose working-tree index is ready, from the last index/progress. */
  private readyRepos = new Set<string>();

  /** Listens for the backend's batches and indexing progress; `initial` is the state saved last session. */
  constructor(
    private readonly backend: Backend,
    private readonly ui: Ui,
    private readonly options: ControllerOptions,
    initial?: PersistedState,
  ) {
    this.state = initial ?? { text: "", recent: [] };
    backend.on("batch", (batch) => {
      this.onBatch(batch);
    });
    backend.on("progress", (progress) => {
      this.onIndexProgress(progress);
    });
  }

  /** The query box text, as last reported by the panel. */
  get text(): string {
    return this.state.text;
  }

  /** Sends the box text, recent queries and settings to a (re)opened panel. A given query replaces the text. */
  restore(query?: string): void {
    if (query !== undefined) this.state.text = query;
    this.ui.post("state.restore", {
      text: this.state.text,
      recent: this.state.recent,
      settings: this.options.uiSettings(),
    });
  }

  /** Handles one message from the search panel. */
  async handle(message: WebviewMessage): Promise<void> {
    switch (message.type) {
      case "ready": {
        // A new webview counts seq from zero again.
        this.latestSeq = 0;
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
      case "help.try": {
        // sent by the help page, which extension.ts routes to the open command
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
    }
  }

  /** F4 / Shift+F4: opens the next or previous result without the panel. */
  async step(direction: 1 | -1): Promise<void> {
    if (this.results.length === 0) return;
    this.stepIndex = (this.stepIndex + direction + this.results.length) % this.results.length;
    const result = this.results[this.stepIndex];
    if (result) await this.openRef(result.ref, "current");
  }

  /** Puts the current query at the top of the recent list. */
  rememberQuery(): void {
    const text = this.state.text.trim();
    const limit = this.options.recentLimit();
    if (!text || limit <= 0) return;
    this.state.recent = [text, ...this.state.recent.filter((query) => query !== text)].slice(0, limit);
    this.ui.saveState(this.state);
  }

  /** Parses the box, posts the parse, then searches it unless it has errors or is empty. */
  private async onQueryChanged(change: QueryChangedMessage): Promise<void> {
    if (change.seq < this.latestSeq) return;
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
      const { query, completions } = await this.backend.request("query/parse", { text, cursor });
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
    const search: RunningSearch = { id: `s${this.searchesStarted}`, seq, text, cancel: new CancelSource() };
    this.search = search;
    this.setResults([]);
    await this.requestPage(search);
  }

  /** Load more: fetches the page after `pageCursor` of the search on screen, keeping its results. */
  private async onLoadMore(searchId: string, pageCursor: string): Promise<void> {
    const current = this.search;
    if (current?.id !== searchId) return;
    const search: RunningSearch = { ...current, cancel: new CancelSource() };
    this.search = search;
    await this.requestPage(search, pageCursor);
  }

  /**
   * Asks the daemon for one page of `search` (its items arrive as batches)
   * and posts search.done, unless a newer search has replaced it meanwhile.
   */
  private async requestPage(search: RunningSearch, pageCursor?: string): Promise<void> {
    const { id: searchId, text, seq, cancel } = search;
    const params = pageCursor === undefined ? { searchId, text } : { searchId, text, cursor: pageCursor };
    try {
      const result = await this.backend.request("search/start", params, cancel);
      if (this.search !== search || cancel.cancelled) return;
      this.ui.post("search.done", { seq, searchId, ...result });
    } catch (error) {
      if (error instanceof RpcError && error.code === ErrorCodes.RequestCancelled) return;
      if (this.search === search) this.postSearchFailed(seq, searchId, error);
    }
  }

  /**
   * Runs the current search again when a repo finishes indexing: a search
   * typed while it was indexing could not include its results.
   */
  private onIndexProgress(progress: IndexStatusResult): void {
    const ready = progress.repos.filter((repo) => repo.tree === "ready").map((repo) => repo.repoId);
    const newlyReady = ready.some((id) => !this.readyRepos.has(id));
    this.readyRepos = new Set(ready);
    const search = this.search;
    if (!newlyReady || !search || search.cancel.cancelled) return;
    search.cancel.cancel();
    void this.startSearch(search.text, search.seq);
  }

  private postSearchFailed(seq: number, searchId: string, error: unknown): void {
    const message = error instanceof Error ? error.message : String(error);
    this.ui.post("search.done", { seq, searchId, total: 0, truncated: false, hidden: [], ms: 0, error: message });
  }

  /** Relays a batch of the current search to the panel and remembers its items for F4. */
  private onBatch(batch: SearchBatchParams): void {
    const search = this.search;
    if (batch.searchId !== search?.id || search.cancel.cancelled) return;
    this.results.push(...batch.items);
    this.ui.setContext("unifiedSearch.hasResults", this.results.length > 0);
    this.ui.post("search.batch", { seq: search.seq, searchId: search.id, items: batch.items });
  }

  private setResults(items: ResultItem[]): void {
    this.results = items;
    this.stepIndex = -1;
    this.ui.setContext("unifiedSearch.hasResults", items.length > 0);
  }

  /** Fetches a result's preview; a stale ref gets an empty preview marked stale. */
  private async onSelect(ref: string): Promise<void> {
    try {
      const contextLines = this.options.previewContextLines ?? DEFAULT_PREVIEW_CONTEXT_LINES;
      const preview = await this.backend.request("preview/get", { ref, contextLines });
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
    this.rememberQuery();
    if (this.options.closeOnOpen()) this.ui.hidePanel();
  }

  /** Opens a result; a stale ref greys its row out instead. Returns whether it opened. */
  private async openRef(ref: string, where: OpenWhere): Promise<boolean> {
    try {
      const target = await this.backend.request("open/resolve", { ref });
      await this.ui.openTarget(
        target,
        where,
        this.results.find((result) => result.ref === ref),
      );
      return true;
    } catch (error) {
      if (error instanceof RpcError && error.code === ErrorCodes.RefStale) {
        this.ui.post("preview.result", { ref, preview: null, stale: true });
      }
      return false;
    }
  }
}
