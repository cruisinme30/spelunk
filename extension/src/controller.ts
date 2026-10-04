// Host side of Contract 2: turns webview messages into daemon calls and
// streams results back. No vscode import; extension.ts supplies the Ui.
import { CancelSource, ErrorCodes, RpcError } from "./jsonrpc";
import type {
  HostToWebview,
  OpenTarget,
  ResultItem,
  RpcRequests,
  SearchBatchParams,
  UiSettings,
  WebviewToHost,
} from "./protocol.gen";

export interface Backend {
  request<M extends keyof RpcRequests>(method: M, params: RpcRequests[M][0], cancel?: CancelSource): Promise<RpcRequests[M][1]>;
  on(event: "batch", cb: (p: SearchBatchParams) => void): unknown;
}

export interface Ui {
  post<T extends keyof HostToWebview>(type: T, payload: HostToWebview[T]): void;
  openTarget(target: OpenTarget, where: "current" | "side", item?: ResultItem): Promise<void>;
  hidePanel(): void;
  openHelp(): void;
  openSettings(): void;
  restartDaemon(): void;
  setContext(key: string, value: boolean): void;
  saveState(state: PersistedState): void;
}

export interface PersistedState {
  text: string;
  recent: string[];
}

export interface ControllerOptions {
  recentLimit(): number;
  closeOnOpen(): boolean;
  uiSettings(): UiSettings;
  contextLines?: number;
}

type Inbound = { [K in keyof WebviewToHost]: { v: 1; type: K; seq?: number; payload: WebviewToHost[K] } }[keyof WebviewToHost];

export class SearchController {
  private state: PersistedState;
  private latestSeq = 0;
  private search?: { id: string; seq: number; text: string; cancel: CancelSource };
  private searchCounter = 0;
  /** Openable results of the current search, for F4 / Shift+F4. */
  results: ResultItem[] = [];
  private cursor = -1;

  constructor(private backend: Backend, private ui: Ui, private opts: ControllerOptions, initial?: PersistedState) {
    this.state = initial ?? { text: "", recent: [] };
    backend.on("batch", (p) => this.onBatch(p));
  }

  get text(): string {
    return this.state.text;
  }

  /** Called when the panel (re)opens; an explicit query pre-fills the box. */
  restore(query?: string): void {
    if (query !== undefined) this.state.text = query;
    this.ui.post("state.restore", { text: this.state.text, recent: this.state.recent, settings: this.opts.uiSettings() });
  }

  async handle(msg: Inbound): Promise<void> {
    switch (msg.type) {
      case "ready":
        this.restore();
        return;
      case "query.changed":
        return this.onQueryChanged(msg.payload.text, msg.payload.cursor, msg.payload.seq);
      case "result.select":
        return this.onSelect(msg.payload.ref);
      case "result.open":
        return this.onOpen(msg.payload.ref, msg.payload.where);
      case "results.more":
        return this.onMore(msg.payload.searchId, msg.payload.cursor);
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

  private async onQueryChanged(text: string, cursor: number, seq: number): Promise<void> {
    if (seq < this.latestSeq) return;
    this.latestSeq = seq;
    this.state.text = text;
    this.ui.saveState(this.state);
    let parsed;
    try {
      parsed = await this.backend.request("query/parse", { text, cursor });
    } catch (e) {
      this.ui.post("search.done", { seq, searchId: "", total: 0, truncated: false, hidden: [], ms: 0, error: String(e) });
      return;
    }
    if (seq !== this.latestSeq) return;
    this.ui.post("parse.result", { seq, query: parsed.query, completions: parsed.completions });
    const hasErrors = parsed.query.diagnostics.some((d) => d.severity === "error");
    if (hasErrors) return; // keep showing the last good results (mock 13)
    this.search?.cancel.cancel();
    if (parsed.query.root === null) {
      this.search = undefined;
      this.setResults([]);
      return;
    }
    await this.runSearch(text, seq);
  }

  private async runSearch(text: string, seq: number, cursor?: string, searchId?: string): Promise<void> {
    const id = searchId ?? `s${++this.searchCounter}`;
    const cancel = new CancelSource();
    this.search = { id, seq, text, cancel };
    if (!cursor) this.setResults([]);
    try {
      const res = await this.backend.request("search/start", cursor ? { searchId: id, text, cursor } : { searchId: id, text }, cancel);
      if (this.search?.id !== id || cancel.cancelled) return;
      this.ui.post("search.done", { seq, searchId: id, ...res });
    } catch (e) {
      if (e instanceof RpcError && e.code === ErrorCodes.RequestCancelled) return;
      if (this.search?.id !== id) return;
      this.ui.post("search.done", { seq, searchId: id, total: 0, truncated: false, hidden: [], ms: 0, error: e instanceof Error ? e.message : String(e) });
    }
  }

  private onBatch(p: SearchBatchParams): void {
    const s = this.search;
    if (!s || p.searchId !== s.id || s.cancel.cancelled) return;
    this.results.push(...p.items);
    this.ui.setContext("unifiedSearch.hasResults", this.results.length > 0);
    this.ui.post("search.batch", { seq: s.seq, searchId: s.id, items: p.items });
  }

  private setResults(items: ResultItem[]): void {
    this.results = items;
    this.cursor = -1;
    this.ui.setContext("unifiedSearch.hasResults", items.length > 0);
  }

  private async onMore(searchId: string, cursor: string): Promise<void> {
    const s = this.search;
    if (!s || s.id !== searchId) return;
    await this.runSearch(s.text, s.seq, cursor, searchId);
  }

  private async onSelect(ref: string): Promise<void> {
    try {
      const preview = await this.backend.request("preview/get", { ref, contextLines: this.opts.contextLines ?? 7 });
      this.ui.post("preview.result", { ref, preview });
    } catch (e) {
      const stale = e instanceof RpcError && e.code === ErrorCodes.RefStale;
      this.ui.post("preview.result", { ref, preview: null, stale });
    }
  }

  private async onOpen(ref: string, where: "current" | "side"): Promise<void> {
    const idx = this.results.findIndex((r) => r.ref === ref);
    if (idx >= 0) this.cursor = idx;
    const ok = await this.openRef(ref, where);
    if (!ok) return;
    this.rememberQuery();
    if (this.opts.closeOnOpen()) this.ui.hidePanel();
  }

  private async openRef(ref: string, where: "current" | "side"): Promise<boolean> {
    try {
      const target = await this.backend.request("open/resolve", { ref });
      await this.ui.openTarget(target, where, this.results.find((r) => r.ref === ref));
      return true;
    } catch (e) {
      if (e instanceof RpcError && e.code === ErrorCodes.RefStale) this.ui.post("preview.result", { ref, preview: null, stale: true });
      return false;
    }
  }

  /** F4 / Shift+F4: step through the last result list without the panel. */
  async step(delta: 1 | -1): Promise<void> {
    const openable = this.results;
    if (!openable.length) return;
    this.cursor = (this.cursor + delta + openable.length) % openable.length;
    await this.openRef(openable[this.cursor].ref, "current");
  }

  rememberQuery(): void {
    const t = this.state.text.trim();
    const limit = this.opts.recentLimit();
    if (!t || limit <= 0) return;
    this.state.recent = [t, ...this.state.recent.filter((r) => r !== t)].slice(0, limit);
    this.ui.saveState(this.state);
  }
}
