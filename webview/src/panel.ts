// SearchPanel ties the webview together: it owns the state, reacts to the
// user and to host messages, and calls the render functions.
//
// The panel is a pure view: it never parses. Fix-its, completions and the Aa / .*
// toggles all edit the query text, then send an ordinary query.changed.
import { clamp, element, plural, wrapIndex } from "./format";
import { type HostMessage, loadDraft, onHostMessage, saveDraft, send } from "./host";
import { createLayout, type Layout, REPO_MENU_ANCHOR_CLASS } from "./layout";
import { pathScope, scopedRepo } from "./parsedQuery";
import type {
  Completion,
  Fix,
  OpenWhere,
  ParseResultMsg as ParseResultMessage,
  PreviewResultMsg as PreviewResultMessage,
  SearchBatchMsg as SearchBatchMessage,
  SearchDoneMsg as SearchDoneMessage,
  StateRestoreMsg as StateRestoreMessage,
} from "./protocol.gen";
import { applyEdits, isCasePressed, isRegexPressed, scopeToRepo, toggleCase, toggleRegex } from "./queryEdit";
import {
  type FooterMode,
  renderBanners,
  renderChips,
  renderDiagnostics,
  renderFooter,
  renderIndexStatus,
} from "./render/chrome";
import { completionsVisible, renderCompletions, type ResultsGlimpse } from "./render/completions";
import { renderEmptyState } from "./render/emptyState";
import { renderRepoMenu } from "./render/repoMenu";
import { renderPreview } from "./render/preview";
import { ResultsView } from "./render/results";
import { createViewState, hasErrors, type ViewState } from "./state";

/** Cheat-sheet snippets that put the cursor between a pair: "|", /|/, (|). */
const PAIRED_SNIPPETS = new Set(['""', "//", "()"]);
/** Holding ↓ shouldn't request a preview for every row it passes. */
const PREVIEW_DEBOUNCE_MS = 30;

/** The search panel's controller in the webview: one per page. */
export class SearchPanel {
  private readonly state: ViewState = createViewState();
  private readonly layout: Layout;
  private readonly summary = element("span", { class: "summary", "data-testid": "summary" });
  private results: ResultsView | undefined;
  private debounceTimer: ReturnType<typeof setTimeout> | undefined;
  private previewTimer: ReturnType<typeof setTimeout> | undefined;

  /** Builds the panel's skeleton into `root`; start() brings it to life. */
  constructor(root: HTMLElement) {
    this.layout = createLayout(root);
  }

  /** Binds events, restores the draft and tells the host the panel is ready. */
  start(): void {
    this.bindEvents();
    onHostMessage((message) => {
      this.onHostMessage(message);
    });
    renderIndexStatus(this.layout, this.state);
    this.showEmptyState();
    const draft = loadDraft();
    if (draft) this.layout.input.value = draft;
    send("ready", {});
    this.layout.input.focus();
  }

  // ------------------------------------------------------------ query text

  /**
   * Reports the box to the host: after the typing delay, or now if
   * `immediate`. `asTyped` asks for the word at the cursor to be searched
   * too, even though suggestions are offered for it.
   */
  private queryChanged(immediate = false, asTyped = false): void {
    clearTimeout(this.debounceTimer);
    const report = () => {
      const { input } = this.layout;
      this.state.seq++;
      saveDraft(input.value);
      send("query.changed", {
        text: input.value,
        cursor: input.selectionStart ?? input.value.length,
        seq: this.state.seq,
        ...(asTyped ? { asTyped } : {}),
      });
    };
    if (immediate || this.state.ui.typingDelayMs <= 0) report();
    else this.debounceTimer = setTimeout(report, this.state.ui.typingDelayMs);
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

  /** Inserts a cheat-sheet snippet at the cursor, separated from the word before it. */
  private insertAtCursor(snippet: string): void {
    const { input } = this.layout;
    const position = input.selectionStart ?? input.value.length;
    const before = input.value.slice(0, position);
    const separator = before && !before.endsWith(" ") && !snippet.startsWith(" ") ? " " : "";
    const cursor = before.length + separator.length + snippet.length - (PAIRED_SNIPPETS.has(snippet) ? 1 : 0);
    input.value = before + separator + snippet + input.value.slice(position);
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
    input.addEventListener("keydown", (event) => {
      this.onKeyDown(event);
    });
    input.addEventListener("focus", () => {
      this.renderCompletions();
    });
    // On blur, wait a tick: activeElement is still the input during the event.
    input.addEventListener("blur", () =>
      setTimeout(() => {
        this.renderCompletions();
      }, 0),
    );

    caseButton.addEventListener("click", () => {
      const edited = toggleCase(input.value, this.state.parsed, this.state.ui.caseSensitive);
      this.setQuery(edited.text, edited.cursor);
    });
    regexButton.addEventListener("click", () => {
      const edited = toggleRegex(input.value, this.state.parsed);
      this.setQuery(edited.text, edited.cursor);
    });
    reposButton.addEventListener("click", () => {
      this.toggleRepoMenu();
    });
    // A click anywhere else closes the repo menu.
    document.addEventListener("mousedown", (event) => {
      const target = event.target as HTMLElement;
      if (!target.closest(`.${REPO_MENU_ANCHOR_CLASS}`)) this.toggleRepoMenu(false);
    });

    body.addEventListener("click", (event) => {
      const ref = rowReference(event);
      if (!ref) return;
      this.select(ref);
      if (this.state.ui.openTrigger === "singleClick") this.open(ref, event);
    });
    body.addEventListener("dblclick", (event) => {
      const ref = rowReference(event);
      if (ref && this.state.ui.openTrigger === "doubleClick") this.open(ref, event);
    });
  }

  /** Opens or closes the repo menu (or sets it with `open`). */
  private toggleRepoMenu(open = this.layout.repoMenu.hidden): void {
    const { repoMenu, reposButton, input } = this.layout;
    repoMenu.hidden = !open;
    reposButton.setAttribute("aria-expanded", String(open));
    if (!open) return;
    renderRepoMenu(repoMenu, this.state.repos, scopedRepo(this.state.parsed), {
      onPick: (repoName) => {
        this.toggleRepoMenu(false);
        const edited = scopeToRepo(input.value, this.state.parsed, repoName);
        this.setQuery(edited.text, edited.cursor);
        input.focus();
      },
    });
  }

  /** ⌘ or Ctrl opens to the side. */
  private open(ref: string, event: MouseEvent | KeyboardEvent): void {
    const where: OpenWhere = event.metaKey || event.ctrlKey ? "side" : "current";
    send("result.open", { ref, where });
  }

  private get completionsVisible(): boolean {
    return completionsVisible(this.state, this.layout.input);
  }

  /** Closes the suggestions and searches the box exactly as typed (Esc or ↵ while suggesting). */
  private searchAsTyped(): void {
    this.state.completionsOpen = false;
    this.renderCompletions();
    this.renderFooter();
    this.queryChanged(true, true);
  }

  private onKeyDown(event: KeyboardEvent): void {
    const handled = this.handleKey(event);
    if (handled) event.preventDefault();
  }

  /** Returns whether the key was handled (and its default should be prevented). */
  private handleKey(event: KeyboardEvent): boolean {
    const modifier = event.metaKey || event.ctrlKey;
    switch (event.key) {
      case "?": {
        if (this.completionsVisible) {
          send("help.open", {}); // every operator, explained
          return true;
        }
        if (this.layout.input.value) return false; // a literal ? in a query
        this.state.sheetOpen = !this.state.sheetOpen;
        this.showEmptyState();
        return true;
      }
      case ".": {
        if (!modifier) return false;
        this.applyFirstFix();
        return true;
      }
      case "Tab": {
        if (!this.completionsVisible) return false;
        this.acceptCompletion(this.state.completionIndex);
        return true;
      }
      case "Escape": {
        this.onEscape();
        return true;
      }
      case "ArrowDown":
      case "ArrowUp": {
        this.onArrow(event.key === "ArrowDown" ? 1 : -1);
        return true;
      }
      case "Enter": {
        this.onEnter(event, modifier);
        return true;
      }
      default: {
        return false;
      }
    }
  }

  /** ⌘. applies the first fix of the first diagnostic that has one. */
  private applyFirstFix(): void {
    const firstFix = this.state.parsed?.diagnostics.find((diagnostic) => diagnostic.fixes.length)?.fixes[0];
    if (firstFix) this.applyFix(firstFix);
  }

  /** Esc closes the suggestions first, keeping the word as typed, then the panel. */
  private onEscape(): void {
    if (!this.layout.repoMenu.hidden) this.toggleRepoMenu(false);
    else if (this.completionsVisible) this.searchAsTyped();
    else send("panel.close", {});
  }

  /** ↑↓ move through suggestions when they are open, otherwise through rows. */
  private onArrow(step: 1 | -1): void {
    if (this.completionsVisible) {
      this.state.completionIndex = wrapIndex(this.state.completionIndex, step, this.state.completions.length);
      this.renderCompletions();
    } else if (this.layout.input.value) {
      this.moveResultSelection(step);
    } else {
      this.moveRecentSelection(step);
    }
  }

  /** Enter searches as typed while suggesting (Tab accepts), runs a recent query, or opens the selected result. */
  private onEnter(event: KeyboardEvent, modifier: boolean): void {
    if (this.completionsVisible && !modifier) this.searchAsTyped();
    else if (!this.layout.input.value) this.runRecent(this.state.recent[this.state.recentIndex]);
    else if (this.state.selectedRef) this.open(this.state.selectedRef, event);
  }

  private runRecent(query: string | undefined): void {
    if (query) this.setQuery(query);
  }

  private moveRecentSelection(step: 1 | -1): void {
    const rows = [...this.layout.body.querySelectorAll<HTMLElement>("[data-recent]")];
    if (rows.length === 0) return;
    this.state.recentIndex = wrapIndex(this.state.recentIndex, step, rows.length);
    for (const [index, row] of rows.entries()) row.classList.toggle("selected", index === this.state.recentIndex);
  }

  /** Results don't wrap: ↓ on the last row stays there. */
  private moveResultSelection(step: 1 | -1): void {
    const rows = this.results?.rows() ?? [];
    if (rows.length === 0) return;
    const current = rows.findIndex((row) => row.dataset["ref"] === this.state.selectedRef);
    const next = current === -1 ? 0 : clamp(current + step, 0, rows.length - 1);
    const ref = rows[next]?.dataset["ref"];
    if (ref) this.select(ref);
  }

  /** Highlights a row and asks the host for its preview. */
  private select(ref: string): void {
    if (this.state.selectedRef === ref) return;
    this.state.selectedRef = ref;
    for (const row of this.results?.rows() ?? []) {
      const selected = row.dataset["ref"] === ref;
      row.classList.toggle("selected", selected);
      row.setAttribute("aria-selected", String(selected));
      if (selected) {
        row.scrollIntoView({ block: "nearest" });
        this.layout.input.setAttribute("aria-activedescendant", row.id);
      }
    }
    clearTimeout(this.previewTimer);
    this.previewTimer = setTimeout(() => {
      send("result.select", { ref });
    }, PREVIEW_DEBOUNCE_MS);
  }

  // ------------------------------------------------------------ host messages

  private onHostMessage(message: HostMessage): void {
    const state = this.state;
    switch (message.type) {
      case "state.restore": {
        this.onRestore(message.payload);
        return;
      }
      case "focus": {
        this.layout.input.focus();
        this.layout.input.select();
        return;
      }
      case "parse.result": {
        if (message.payload.seq === state.seq) this.onParsed(message.payload);
        return;
      }
      case "search.batch": {
        this.onSearchBatch(message.payload);
        return;
      }
      case "search.done": {
        if (this.isCurrent(message.payload.seq, message.payload.searchId)) this.onSearchDone(message.payload);
        return;
      }
      case "preview.result": {
        this.onPreviewResult(message.payload);
        return;
      }
      case "index.status": {
        state.repos = message.payload.repos;
        renderIndexStatus(this.layout, state);
        this.renderBanners();
        return;
      }
      case "banner": {
        state.banner = message.payload.state === "ok" ? undefined : message.payload;
        this.renderBanners();
        return;
      }
      case "welcome.state": {
        return; // for the welcome page
      }
    }
  }

  /** A batch of results for the current search: appended, and the first one selected. */
  private onSearchBatch({ seq, searchId, items }: SearchBatchMessage): void {
    if (!this.isCurrent(seq, searchId)) return;
    this.resultsFor(searchId).append(items);
    this.renderSummary();
    this.selectFirstRow();
    if (this.completionsVisible) this.renderCompletions(); // the results glimpse
  }

  /** The preview of the selected result; a stale result is greyed out in the list too. */
  private onPreviewResult(preview: PreviewResultMessage): void {
    if (preview.ref !== this.state.selectedRef) return;
    this.state.preview = preview;
    this.state.showHiddenFiles = false;
    this.renderPreview();
    if (preview.stale) this.results?.markStale(preview.ref);
  }

  /** The host's saved box text, recent queries and settings: on opening, and after settings change. */
  private onRestore({ text, recent, settings }: StateRestoreMessage): void {
    const { input } = this.layout;
    if (settings) this.state.ui = settings;
    this.state.recent = recent;
    if (text !== input.value) {
      input.value = text;
      input.setSelectionRange(text.length, text.length);
    }
    if (input.value) this.queryChanged(true);
    else this.showEmptyState();
    this.renderFooter();
    input.focus();
  }

  /** A batch or done message belongs to the newest query, or extends the search on screen (Load more). */
  private isCurrent(seq: number, searchId: string): boolean {
    return seq === this.state.seq || searchId === this.state.searchId;
  }

  /** The daemon's reading of the box: updates the toggles, suggestions, diagnostics and chips. */
  private onParsed({ query, completions, searchText }: ParseResultMessage): void {
    const state = this.state;
    state.parsed = query;
    state.completions = completions;
    state.searchText = searchText;
    if (state.completionIndex >= completions.length) state.completionIndex = 0;
    const errors = hasErrors(state);
    if (!errors && query.root) state.lastGoodText = query.raw;

    const { caseButton, regexButton, shell, input } = this.layout;
    caseButton.setAttribute("aria-pressed", String(isCasePressed(query, state.ui.caseSensitive)));
    regexButton.setAttribute("aria-pressed", String(isRegexPressed(query)));
    shell.classList.toggle("has-errors", errors);
    input.setAttribute("aria-invalid", String(errors));

    this.renderCompletions();
    renderIndexStatus(this.layout, state); // the repos button names the scoped repo
    renderDiagnostics(this.layout, state, (fix) => {
      this.applyFix(fix);
    });
    this.results?.showLastGood(errors && state.lastGoodText ? state.lastGoodText : undefined);
    renderChips(this.layout, state, this.summary);
    this.renderSummary();
    if (!query.root && !errors) this.showEmptyState();
    this.renderFooter();
  }

  /** The results view for `searchId`, starting a fresh one the first time an id is seen. */
  private resultsFor(searchId: string): ResultsView {
    if (this.results && searchId === this.state.searchId) return this.results;
    const state = this.state;
    state.searchId = searchId;
    state.done = undefined;
    state.selectedRef = "";
    state.preview = undefined;
    const handlers = {
      onApplyFix: (fix: Fix) => {
        this.applyFix(fix);
      },
      // Load more fetches the next page of this same search; its batches carry the same searchId.
      onLoadMore: (id: string, cursor: string) => {
        send("results.more", { searchId: id, cursor });
      },
    };
    const errors = hasErrors(state);
    this.results = new ResultsView(this.layout.body, state, handlers, errors ? [] : pathScope(state.parsed));
    this.results.showLastGood(errors ? state.lastGoodText : undefined);
    return this.results;
  }

  private selectFirstRow(): void {
    if (this.state.selectedRef) return;
    const first = this.results?.rows()[0]?.dataset["ref"];
    if (first) this.select(first);
  }

  private onSearchDone(done: SearchDoneMessage): void {
    this.state.done = done;
    this.resultsFor(done.searchId).finish(done);
    this.renderSummary();
    if (this.completionsVisible) this.renderCompletions();
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
      onRunRecent: (query) => {
        this.runRecent(query);
      },
      onInsert: (snippet) => {
        this.insertAtCursor(snippet);
      },
    });
    this.renderFooter();
  }

  private renderCompletions(): void {
    const items = this.results?.items ?? [];
    const glimpse: ResultsGlimpse | undefined = this.results
      ? { total: this.state.done?.total ?? items.length, lines: items.filter((item) => item.kind === "line") }
      : undefined;
    renderCompletions(this.layout, this.state, glimpse, {
      onPick: (index) => {
        this.acceptCompletion(index);
      },
      onSearchAsTyped: () => {
        this.searchAsTyped();
      },
    });
    this.renderFooter();
  }

  private renderBanners(): void {
    renderBanners(this.layout, this.state, () => {
      send("daemon.restart", {});
    });
  }

  private renderFooter(): void {
    renderFooter(this.layout, this.footerMode(), this.state.parsed?.mode === "history");
  }

  /** What the panel shows right now, which picks the key hints. */
  private footerMode(): FooterMode {
    if (!this.layout.input.value) return "empty";
    if (this.completionsVisible) return this.state.completions[0]?.group === "operator" ? "operators" : "values";
    return hasErrors(this.state) ? "errors" : "results";
  }

  private renderPreview(): void {
    if (!this.results) return;
    const item = this.results.items.find((result) => result.ref === this.state.preview?.ref);
    renderPreview(this.results.preview, this.state, item?.repoId, {
      onOpen: (ref, where) => {
        send("result.open", { ref, where });
      },
      onShowHiddenFiles: () => {
        this.state.showHiddenFiles = true;
        this.renderPreview();
      },
    });
  }

  /** "3 file names · 5 code matches" or "4 commits in 3 repos", beside the chips. */
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

/** The ref of the result row an event happened in, if any. */
function rowReference(event: Event): string | undefined {
  return (event.target as HTMLElement).closest<HTMLElement>("[data-ref]")?.dataset["ref"];
}
