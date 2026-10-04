// Host side of Contract 2: turns panel messages into daemon calls and streams
// results back. No vscode import; extension.ts supplies the Ui, and tests
// drive it against the real daemon.
import { CancelSource, RpcError } from "./jsonRpc";
import {
  ErrorCodes,
  type HostToWebview,
  type IndexStatusResult,
  type OpenTarget,
  type OpenWhere,
  type ResultItem,
  type RpcRequests,
  type SearchBatchParams,
  type UiSettings,
  type WebviewToHost,
} from "./protocol.gen";

/** Lines of context around the match in a file preview (mock 1 shows ±7). */
const DEFAULT_PREVIEW_CONTEXT_LINES = 7;

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

export interface ControllerOptions {
  recentLimit(): number;
  closeOnOpen(): boolean;
  uiSettings(): UiSettings;
  previewContextLines?: number;
}

/** A Contract 2 message from the webview, discriminated by `type`. */
export type WebviewMessage = {
  [K in keyof WebviewToHost]: { v: 1; type: K; payload: WebviewToHost[K] };
}[keyof WebviewToHost];

interface RunningSearch {
  id: string;
  seq: number;
  text: string;
  cancel: CancelSource;
}

/** Turns panel messages into daemon calls; one per window. */
export class SearchController {
  /** Openable results of the current search, for F4 / Shift+F4. */
  results: ResultItem[] = [];
  private state: PersistedState;
  private latestSeq = 0;
  private search?: RunningSearch;
  private searchCount = 0;
  /** Position in `results` for F4 stepping; -1 before the first step. */
  private stepIndex = -1;
  /** Repos whose working-tree index is ready, from the last index/progress. */
  private readyRepos = new Set<string>();

  constructor(
    private readonly backend: Backend,
    private readonly ui: Ui,
    private readonly options: ControllerOptions,
    initial?: PersistedState,
  ) {
    this.state = initial ?? { text: "", recent: [] };
    backend.on("batch", (batch) => this.onBatch(batch));
    backend.on("progress", (progress) => this.onIndexProgress(progress));
  }

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

  async handle(message: WebviewMessage): Promise<void> {
    switch (message.type) {
      case "ready":
        // A new webview counts seq from zero again.
        this.latestSeq = 0;
        this.restore();
        return;
      case "query.changed":
        return this.onQueryChanged(message.payload.text, message.payload.cursor, message.payload.seq);
      case "result.select":
        return this.onSelect(message.payload.ref);
      case "result.open":
        return this.onOpen(message.payload.ref, message.payload.where);
      case "results.more":
        return this.onLoadMore(message.payload.searchId, message.payload.cursor);
      case "panel.close":
        this.rememberQuery();
        this.ui.hidePanel();
        return;
      case "help.open":
        this.ui.openHelp();
        return;
      case "settings.open":
        this.ui.openSettings();
        return;
      case "daemon.restart":
        this.ui.restartDaemon();
        return;
    }
  }

  /** F4 / Shift+F4: opens the next or previous result without the panel. */
  async step(direction: 1 | -1): Promise<void> {
    if (!this.results.length) return;
    this.stepIndex = (this.stepIndex + direction + this.results.length) % this.results.length;
    await this.openRef(this.results[this.stepIndex].ref, "current");
  }

  /** Puts the current query at the top of the recent list. */
  rememberQuery(): void {
    const text = this.state.text.trim();
    const limit = this.options.recentLimit();
    if (!text || limit <= 0) return;
    this.state.recent = [text, ...this.state.recent.filter((query) => query !== text)].slice(0, limit);
    this.ui.saveState(this.state);
  }

  private async onQueryChanged(text: string, cursor: number, seq: number): Promise<void> {
    if (seq < this.latestSeq) return;
    this.latestSeq = seq;
    this.state.text = text;
    this.ui.saveState(this.state);
    let parsed: RpcRequests["query/parse"][1];
    try {
      parsed = await this.backend.request("query/parse", { text, cursor });
    } catch (error) {
      this.postSearchFailed(seq, "", error);
      return;
    }
    if (seq !== this.latestSeq) return; // a newer keystroke arrived while parsing
    this.ui.post("parse.result", { seq, query: parsed.query, completions: parsed.completions });
    // With errors, keep showing the last good results (mock 13).
    if (parsed.query.diagnostics.some((diagnostic) => diagnostic.severity === "error")) return;
    this.search?.cancel.cancel();
    if (parsed.query.root === null) {
      this.search = undefined;
      this.setResults([]);
      return;
    }
    await this.runSearch(text, seq);
  }

  /** Starts a search, or fetches the page after `pageCursor` of the current one. */
  private async runSearch(text: string, seq: number, pageCursor?: string, searchId?: string): Promise<void> {
    const id = searchId ?? `s${++this.searchCount}`;
    const cancel = new CancelSource();
    this.search = { id, seq, text, cancel };
    if (!pageCursor) this.setResults([]);
    try {
      const params = pageCursor ? { searchId: id, text, cursor: pageCursor } : { searchId: id, text };
      const result = await this.backend.request("search/start", params, cancel);
      if (this.search?.id !== id || cancel.cancelled) return;
      this.ui.post("search.done", { seq, searchId: id, ...result });
    } catch (error) {
      if (error instanceof RpcError && error.code === ErrorCodes.RequestCancelled) return;
      if (this.search?.id === id) this.postSearchFailed(seq, id, error);
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
    void this.runSearch(search.text, search.seq);
  }

  private postSearchFailed(seq: number, searchId: string, error: unknown): void {
    const message = error instanceof Error ? error.message : String(error);
    this.ui.post("search.done", { seq, searchId, total: 0, truncated: false, hidden: [], ms: 0, error: message });
  }

  private onBatch(batch: SearchBatchParams): void {
    const search = this.search;
    if (!search || batch.searchId !== search.id || search.cancel.cancelled) return;
    this.results.push(...batch.items);
    this.ui.setContext("unifiedSearch.hasResults", this.results.length > 0);
    this.ui.post("search.batch", { seq: search.seq, searchId: search.id, items: batch.items });
  }

  private setResults(items: ResultItem[]): void {
    this.results = items;
    this.stepIndex = -1;
    this.ui.setContext("unifiedSearch.hasResults", items.length > 0);
  }

  private async onLoadMore(searchId: string, pageCursor: string): Promise<void> {
    const search = this.search;
    if (!search || search.id !== searchId) return;
    await this.runSearch(search.text, search.seq, pageCursor, searchId);
  }

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

  private async onOpen(ref: string, where: OpenWhere): Promise<void> {
    const index = this.results.findIndex((result) => result.ref === ref);
    if (index >= 0) this.stepIndex = index;
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
