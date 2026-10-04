// SearchPanel ties the webview together: it owns the state, reacts to the
// user and to host messages, and calls the render functions.
//
// The panel is a pure view (Contract 2). Fix-its, completions and the Aa / .*
// toggles all edit the query text, then send an ordinary query.changed.
import { type HostMessage, loadDraft, onHostMessage, saveDraft, send } from "./host";
import { createLayout, type Layout } from "./layout";
import type { Completion, Fix, ParsedQuery, SearchDoneMsg } from "./protocol.gen";
import { applyEdits, isCasePressed, isRegexPressed, toggleCase, toggleRegex } from "./queryEdit";
import {
  type FooterMode,
  renderBanner,
  renderChips,
  renderCompletions,
  renderDiagnostics,
  renderFooter,
  renderIndexStatus,
} from "./render/chrome";
import { renderEmptyState } from "./render/emptyState";
import { renderPreview } from "./render/preview";
import { ResultsView, rowId } from "./render/results";
import { createViewState, hasErrors, type ViewState } from "./state";
import { el, plural } from "./format";

/** Snippets inserted from the cheat sheet that put the cursor between a pair. */
const PAIRED_SNIPPETS = new Set(['""', "//", "()"]);

export class SearchPanel {
  private readonly state: ViewState = createViewState();
  private readonly layout: Layout;
  private readonly summary = el("span", { class: "summary", "data-testid": "summary" });
  private results?: ResultsView;
  private debounceTimer?: ReturnType<typeof setTimeout>;
  private selectTimer?: ReturnType<typeof setTimeout>;

  constructor(root: HTMLElement) {
    this.layout = createLayout(root);
  }

  start(): void {
    this.bindEvents();
    onHostMessage((message) => this.onHostMessage(message));
    renderIndexStatus(this.layout, this.state);
    this.showEmptyState();
    const draft = loadDraft();
    if (draft) this.layout.input.value = draft;
    send("ready", {});
    this.layout.input.focus();
  }

  // ------------------------------------------------------------ query text

  /** Reports the box to the host, after the typing delay unless `immediate`. */
  private queryChanged(immediate = false): void {
    clearTimeout(this.debounceTimer);
    const fire = () => {
      const { input } = this.layout;
      this.state.seq++;
      saveDraft(input.value);
      send("query.changed", {
        text: input.value,
        cursor: input.selectionStart ?? input.value.length,
        seq: this.state.seq,
      });
    };
    if (immediate || this.state.ui.typingDelayMs <= 0) fire();
    else this.debounceTimer = setTimeout(fire, this.state.ui.typingDelayMs);
  }

  private setQuery(text: string, cursor = text.length): void {
    const { input } = this.layout;
    input.value = text;
    input.setSelectionRange(cursor, cursor);
    this.state.completionsOpen = false;
    this.renderCompletions();
    this.queryChanged(true);
    if (!text) this.showEmptyState();
  }

  private applyFix(fix: Fix): void {
    const edited = applyEdits(this.layout.input.value, fix.edits);
    this.setQuery(edited.text, edited.cursor);
    this.layout.input.focus();
  }

  private acceptCompletion(index: number): void {
    const completion: Completion | undefined = this.state.completions[index];
    if (!completion) return;
    this.state.completionIndex = index;
    this.applyFix(completion.insert);
    this.state.completionsOpen = true; // keep suggesting values after an operator
  }

  private insertAtCursor(snippet: string): void {
    const { input } = this.layout;
    const position = input.selectionStart ?? input.value.length;
    const before = input.value.slice(0, position);
    const pad = before && !before.endsWith(" ") && !snippet.startsWith(" ") ? " " : "";
    const cursor = before.length + pad.length + snippet.length - (PAIRED_SNIPPETS.has(snippet) ? 1 : 0);
    input.value = before + pad + snippet + input.value.slice(position);
    input.focus();
    input.setSelectionRange(cursor, cursor);
    this.state.completionsOpen = true;
    this.state.sheetOpen = false;
    this.queryChanged(true);
  }

  // ------------------------------------------------------------ user input

  private bindEvents(): void {
    const { input, caseButton, regexButton, reposButton, body } = this.layout;
    input.addEventListener("input", () => {
      this.state.completionsOpen = true;
      this.state.completionIndex = 0;
      this.state.sheetOpen = false;
      this.queryChanged();
      if (!input.value) this.showEmptyState();
    });
    input.addEventListener("keydown", (event) => this.onKeyDown(event));
    input.addEventListener("focus", () => this.renderCompletions());
    input.addEventListener("blur", () => setTimeout(() => this.renderCompletions(), 0));

    caseButton.addEventListener("click", () => {
      const edited = toggleCase(input.value, this.state.parsed, this.state.ui.caseSensitive);
      this.setQuery(edited.text, edited.cursor);
    });
    regexButton.addEventListener("click", () => {
      const edited = toggleRegex(input.value, this.state.parsed);
      this.setQuery(edited.text, edited.cursor);
    });
    reposButton.addEventListener("click", () => {
      const separator = input.value && !input.value.endsWith(" ") ? " " : "";
      this.setQuery(input.value + separator + "repo:");
      this.state.completionsOpen = true;
      input.focus();
    });

    body.addEventListener("click", (event) => {
      const row = (event.target as HTMLElement).closest<HTMLElement>("[data-ref]");
      if (!row?.dataset.ref) return;
      this.select(row.dataset.ref);
      if (this.state.ui.openTrigger === "singleClick") this.open(row.dataset.ref, event);
    });
    body.addEventListener("dblclick", (event) => {
      const row = (event.target as HTMLElement).closest<HTMLElement>("[data-ref]");
      if (row?.dataset.ref && this.state.ui.openTrigger === "doubleClick") this.open(row.dataset.ref, event);
    });
  }

  private open(ref: string, event: MouseEvent | KeyboardEvent): void {
    send("result.open", { ref, where: event.metaKey || event.ctrlKey ? "side" : "current" });
  }

  private onKeyDown(event: KeyboardEvent): void {
    const { input } = this.layout;
    const state = this.state;
    const modifier = event.metaKey || event.ctrlKey;
    const completionsVisible = state.completionsOpen && state.completions.length > 0;

    if (event.key === "?" && !input.value) {
      event.preventDefault();
      state.sheetOpen = !state.sheetOpen;
      this.showEmptyState();
    } else if (modifier && event.key === ".") {
      event.preventDefault();
      const firstFix = state.parsed?.diagnostics.find((d) => d.fixes.length)?.fixes[0];
      if (firstFix) this.applyFix(firstFix);
    } else if (event.key === "Tab" && completionsVisible) {
      event.preventDefault();
      this.acceptCompletion(state.completionIndex);
    } else if (event.key === "Escape") {
      event.preventDefault();
      if (completionsVisible) {
        state.completionsOpen = false;
        this.renderCompletions();
      } else {
        send("panel.close", {});
      }
    } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      if (completionsVisible) {
        state.completionIndex = (state.completionIndex + step + state.completions.length) % state.completions.length;
        this.renderCompletions();
      } else {
        this.moveSelection(step);
      }
    } else if (event.key === "Enter") {
      event.preventDefault();
      if (completionsVisible && !modifier) this.acceptCompletion(state.completionIndex);
      else if (!input.value) this.runRecent(state.recent[state.recentIndex]);
      else if (state.selectedRef) this.open(state.selectedRef, event);
    }
  }

  private runRecent(query: string | undefined): void {
    if (query) this.setQuery(query);
  }

  private moveSelection(step: number): void {
    if (!this.layout.input.value) {
      const rows = Array.from(this.layout.body.querySelectorAll<HTMLElement>("[data-recent]"));
      if (!rows.length) return;
      this.state.recentIndex = (this.state.recentIndex + step + rows.length) % rows.length;
      rows.forEach((row, i) => row.classList.toggle("selected", i === this.state.recentIndex));
      return;
    }
    const rows = this.results?.rows() ?? [];
    if (!rows.length) return;
    const current = rows.findIndex((row) => row.dataset.ref === this.state.selectedRef);
    const next = rows[Math.max(0, Math.min(rows.length - 1, current < 0 ? 0 : current + step))];
    if (next.dataset.ref) this.select(next.dataset.ref);
  }

  /** Highlights a row and asks the host for its preview (debounced for key repeat). */
  private select(ref: string): void {
    if (this.state.selectedRef === ref) return;
    this.state.selectedRef = ref;
    for (const row of this.results?.rows() ?? []) {
      const selected = row.dataset.ref === ref;
      row.classList.toggle("selected", selected);
      row.setAttribute("aria-selected", String(selected));
      if (selected) {
        row.scrollIntoView({ block: "nearest" });
        this.layout.input.setAttribute("aria-activedescendant", row.id);
      }
    }
    clearTimeout(this.selectTimer);
    this.selectTimer = setTimeout(() => send("result.select", { ref }), 30);
  }

  // ------------------------------------------------------------ host messages

  private onHostMessage(message: HostMessage): void {
    const state = this.state;
    switch (message.type) {
      case "state.restore": {
        const { input } = this.layout;
        if (message.payload.settings) state.ui = message.payload.settings;
        state.recent = message.payload.recent;
        if (message.payload.text !== input.value) {
          input.value = message.payload.text;
          input.setSelectionRange(input.value.length, input.value.length);
        }
        if (input.value) this.queryChanged(true);
        else this.showEmptyState();
        this.renderFooter();
        input.focus();
        return;
      }
      case "focus":
        this.layout.input.focus();
        this.layout.input.select();
        return;
      case "parse.result":
        if (message.payload.seq === state.seq) this.onParsed(message.payload.query, message.payload.completions);
        return;
      case "search.batch":
        if (!this.isCurrent(message.payload.seq, message.payload.searchId)) return;
        this.ensureResults(message.payload.searchId).append(message.payload.items);
        this.renderSummary();
        this.selectFirstRow();
        return;
      case "search.done":
        if (!this.isCurrent(message.payload.seq, message.payload.searchId)) return;
        this.onSearchDone(message.payload);
        return;
      case "preview.result":
        if (message.payload.ref !== state.selectedRef) return;
        state.preview = message.payload;
        state.showHiddenFiles = false;
        this.renderPreview();
        if (message.payload.stale) document.getElementById(rowId(message.payload.ref))?.classList.add("stale");
        return;
      case "index.status":
        state.repos = message.payload.repos;
        renderIndexStatus(this.layout, state);
        this.renderBanner();
        return;
      case "banner":
        state.banner = message.payload.state === "ok" ? undefined : message.payload;
        this.renderBanner();
        return;
    }
  }

  /** A batch or done message belongs to the newest query, or extends the search on screen. */
  private isCurrent(seq: number, searchId: string): boolean {
    return seq === this.state.seq || searchId === this.state.searchId;
  }

  private onParsed(query: ParsedQuery, completions: Completion[]): void {
    const state = this.state;
    state.parsed = query;
    state.completions = completions;
    if (state.completionIndex >= completions.length) state.completionIndex = 0;
    const errors = hasErrors(state);
    if (!errors && query.root) state.lastGoodText = query.raw;

    const { caseButton, regexButton, shell, input } = this.layout;
    caseButton.setAttribute("aria-pressed", String(isCasePressed(query, state.ui.caseSensitive)));
    regexButton.setAttribute("aria-pressed", String(isRegexPressed(query)));
    shell.classList.toggle("has-errors", errors);
    input.setAttribute("aria-invalid", String(errors));

    this.renderCompletions();
    renderDiagnostics(this.layout, state, (fix) => this.applyFix(fix));
    renderChips(this.layout, state, this.summary);
    this.renderSummary();
    if (!query.root && !errors) this.showEmptyState();
    this.renderFooter();
  }

  /** Starts a fresh result list the first time a search id is seen. */
  private ensureResults(searchId: string): ResultsView {
    if (this.results && searchId === this.state.searchId) return this.results;
    const state = this.state;
    state.searchId = searchId;
    state.done = undefined;
    state.selectedRef = "";
    state.preview = undefined;
    this.results = new ResultsView(
      this.layout.body,
      state,
      {
        onApplyFix: (fix) => this.applyFix(fix),
        onLoadMore: (id, cursor) => send("results.more", { searchId: id, cursor }),
      },
      hasErrors(state) ? state.lastGoodText : undefined,
    );
    return this.results;
  }

  private selectFirstRow(): void {
    if (this.state.selectedRef) return;
    const first = this.results?.rows()[0]?.dataset.ref;
    if (first) this.select(first);
  }

  private onSearchDone(done: SearchDoneMsg): void {
    this.state.done = done;
    this.ensureResults(done.searchId).finish(done);
    this.renderSummary();
  }

  // ------------------------------------------------------------ rendering

  private showEmptyState(): void {
    const state = this.state;
    state.searchId = "";
    state.selectedRef = "";
    state.preview = undefined;
    this.results = undefined;
    this.layout.chips.replaceChildren();
    this.layout.diagnostics.replaceChildren();
    renderEmptyState(this.layout.body, state, {
      onRunRecent: (query) => this.runRecent(query),
      onInsert: (snippet) => this.insertAtCursor(snippet),
    });
    this.renderFooter();
  }

  private renderCompletions(): void {
    renderCompletions(this.layout, this.state, (index) => this.acceptCompletion(index));
  }

  private renderBanner(): void {
    renderBanner(this.layout, this.state, () => send("daemon.restart", {}));
  }

  private renderFooter(): void {
    const mode: FooterMode = !this.layout.input.value ? "empty" : hasErrors(this.state) ? "errors" : "results";
    renderFooter(this.layout, mode, this.state.parsed?.mode === "history");
  }

  private renderPreview(): void {
    if (!this.results) return;
    const item = this.results.items.find((i) => i.ref === this.state.preview?.ref);
    renderPreview(this.results.preview, this.state, item?.repoId, {
      onOpen: (ref, where) => send("result.open", { ref, where }),
      onShowHiddenFiles: () => {
        this.state.showHiddenFiles = true;
        this.renderPreview();
      },
    });
  }

  /** "3 file names · 5 code matches" or "4 commits in 3 repos" beside the chips. */
  private renderSummary(): void {
    const counts = this.results?.counts();
    const parts: string[] = [];
    if (counts && this.state.parsed?.mode === "history") {
      parts.push(`${plural(counts.commit, "commit")} in ${plural(counts.repos, "repo")}`);
    } else if (counts) {
      if (counts.file) parts.push(plural(counts.file, "file name"));
      if (counts.symbol) parts.push(plural(counts.symbol, "definition"));
      if (counts.line) parts.push(plural(counts.line, "code match", "code matches"));
    }
    if (this.state.done && !this.results?.items.length) parts.push("0 results");
    if (this.state.done?.truncated) parts.push("truncated");
    this.summary.textContent = parts.join(" · ");
  }
}
