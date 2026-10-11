// SearchPanel ties the webview together: it owns the state, reacts to the
// user and to host messages, and calls the render functions.
//
// The panel is a pure view: it never parses. Fix-its, completions, the Aa / ab / .*
// toggles, the repo menu and the facet row all edit the query text, then send an
// ordinary query.changed.
import { element, wrapIndex } from "./format";
import { setBoxText } from "./boxText";
import { type HostMessage, loadDraft, loadOutlineOpen, loadRecentWidth, onHostMessage, saveDraft, send } from "./host";
import { EmptyBox } from "./emptyBox";
import { createLayout, type Layout } from "./layout";
import { pathScope, scopedRepo, topLevelOperators } from "./parsedQuery";
import type {
  Completion,
  Fix,
  OpenWhere,
  ParseResultMessage,
  PreviewResultMessage,
  SearchBatchMessage,
  SearchDoneMessage,
  StateRestoreMessage,
  WatchStateMessage,
} from "./protocol.gen";
import {
  applyEdits,
  type Edited,
  isRegexPressed,
  isWordPressed,
  scopeToRepo,
  setOrder,
  toggleCase,
  toggleFacet,
  toggleRegex,
  toggleWord,
} from "./queryEdit";
import {
  type FooterMode,
  renderBanners,
  renderCaseToggle,
  renderChips,
  renderDiagnostics,
  renderFooter,
  renderIndexStatus,
} from "./render/chrome";
import {
  completionsVisible,
  GLIMPSE_LINES,
  pointAtActiveOption,
  renderCompletions,
  type ResultsGlimpse,
} from "./render/completions";
import { renderEmptyState } from "./render/emptyState";
import { renderFacets } from "./render/facets";
import { joinOrder } from "./render/order";
import { applyRemoteHosts } from "./render/menuContext";
import { RepoMenu } from "./render/repoMenu";
import { previewOf, renderPreview } from "./render/preview";
import { ResultsView, summaryText } from "./render/results";
import { ReplaceBox } from "./replace";
import { ResultSelection } from "./resultSelection";
import { createViewState, hasErrors, type ViewState } from "./state";
import { applyCodeSettings, SyntaxPainter } from "./syntax";

/** Cheat-sheet snippets that put the cursor between a pair: "|", /|/, (|). */
const PAIRED_SNIPPETS = new Set(['""', "//", "()", 'struct:""']);

/** The search panel's controller in the webview: one per page. */
export class SearchPanel {
  private readonly state: ViewState = createViewState();
  private readonly layout: Layout;
  private readonly emptyBox: EmptyBox;
  private readonly replace: ReplaceBox;
  private readonly repoMenu: RepoMenu;
  private readonly selection: ResultSelection;
  private readonly summary = element("span", { class: "summary", "data-testid": "summary" });
  private results: ResultsView | undefined;
  /** The theme's syntax colors, which color the code of results and previews. */
  private readonly painter = new SyntaxPainter();
  private debounceTimer: ReturnType<typeof setTimeout> | undefined;
  /** Set while the panel itself edits the box, so its input event isn't taken for typing. */
  private editingBox = false;

  /** Builds the panel's skeleton into `root`; start() brings it to life. */
  constructor(root: HTMLElement) {
    this.layout = createLayout(root);
    this.emptyBox = new EmptyBox(this.state, this.layout, this);
    this.replace = new ReplaceBox(this.layout, {
      searchedText: () => this.searchedText(),
      typingDelayMs: () => this.state.ui.typingDelayMs,
      onPreviewChanged: (lines) => {
        this.results?.showReplacements(lines);
        this.renderPreview();
      },
      onFocusChanged: () => {
        this.renderFooter();
      },
    });
    this.selection = new ResultSelection(this.state, {
      results: () => this.results,
      onSelected: () => {
        this.pointAtActiveOption();
      },
    });
    this.repoMenu = new RepoMenu(this.layout, {
      repos: () => this.state.repos,
      selected: () => scopedRepo(this.state.parsed),
      onPick: (repoName) => {
        if (this.editQuery((text) => scopeToRepo(text, this.state.parsed, repoName))) this.layout.input.focus();
      },
    });
  }

  /** The query the results on screen are for, while there are results of a query to replace. */
  private searchedText(): string | undefined {
    if (!this.results || hasErrors(this.state) || !this.state.parsed?.root) return undefined;
    return this.state.searchText ?? this.state.parsed.raw;
  }

  /** Binds events, restores the draft and tells the host the panel is ready. */
  start(): void {
    this.bindEvents();
    onHostMessage((message) => {
      if (message.type === "syntax.lines") this.painter.receive(message.payload);
      else if (message.type === "remotes.state") applyRemoteHosts(this.state, this.results, message.payload);
      else this.onHostMessage(message);
    });
    renderIndexStatus(this.layout, this.state);
    this.state.recentWidth = loadRecentWidth();
    this.state.outlineOpen = loadOutlineOpen();
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
      this.debounceTimer = undefined;
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

  /** Puts `text` in the box, with the cursor at `cursor`, and searches it at once. */
  setQuery(text: string, cursor = text.length): void {
    const { input } = this.layout;
    input.select();
    this.typeIntoBox(text);
    input.setSelectionRange(cursor, cursor);
    this.state.completionsOpen = false;
    this.renderCompletions();
    this.queryChanged(true);
    if (!text) this.showEmptyState();
  }

  /**
   * Replaces the box's selection with `text` the way typing would, so ⌘Z
   * undoes a fix-it, completion or toggle; assigning input.value would wipe
   * the box's undo history instead.
   */
  private typeIntoBox(text: string): void {
    const { input } = this.layout;
    const start = input.selectionStart ?? 0;
    const end = input.selectionEnd ?? start;
    const expected = input.value.slice(0, start) + text + input.value.slice(end);
    if (!text && start === end) return; // "delete" on a caret would erase the character before it
    input.focus();
    this.editingBox = true;
    try {
      // eslint-disable-next-line @typescript-eslint/no-deprecated -- still the only edit that joins the undo stack
      document.execCommand(text ? "insertText" : "delete", false, text);
    } finally {
      this.editingBox = false;
    }
    if (input.value !== expected) input.value = expected; // execCommand unavailable
  }

  /**
   * Whether edits worked out for `basis` (the text the daemon parsed or
   * searched) still fit the box. Spans index that text, so after another
   * keystroke they would cut the wrong characters; the box is reported now
   * instead, and fresh edits arrive with its parse.
   */
  private editsFit(basis: string | undefined): boolean {
    if (this.layout.input.value === basis) return true;
    if (this.debounceTimer !== undefined) this.queryChanged(true);
    return false;
  }

  /** Puts `edit` of the box's text in the box, if edits worked out for `basis` still fit it; says whether it did. */
  private editQuery(edit: (text: string) => Edited, basis: string | undefined = this.parsedText): boolean {
    if (!this.editsFit(basis)) return false;
    const edited = edit(this.layout.input.value);
    this.setQuery(edited.text, edited.cursor);
    return true;
  }

  /** Applies a fix-it, completion or note's undo, worked out for `basis` (by default, the parsed text). */
  private applyFix(fix: Fix, basis = this.state.parsed?.raw): void {
    if (this.editQuery((text) => applyEdits(text, fix.edits), basis)) this.layout.input.focus();
  }

  private acceptCompletion(index: number): void {
    const completion: Completion | undefined = this.state.completions[index];
    if (!completion) return;
    this.state.completionIndex = index;
    this.applyFix(completion.insert);
    this.state.completionsOpen = true; // keep suggesting values after an operator
  }

  /** Inserts a cheat-sheet snippet at the cursor, separated from the word before it. */
  insertAtCursor(snippet: string): void {
    const { input } = this.layout;
    const position = input.selectionStart ?? input.value.length;
    const before = input.value.slice(0, position);
    const separator = before && !before.endsWith(" ") && !snippet.startsWith(" ") ? " " : "";
    const cursor = before.length + separator.length + snippet.length - (PAIRED_SNIPPETS.has(snippet) ? 1 : 0);
    input.setSelectionRange(position, position);
    this.typeIntoBox(separator + snippet);
    input.setSelectionRange(cursor, cursor);
    this.state.completionsOpen = true;
    this.queryChanged(true);
  }

  // ------------------------------------------------------------ user input

  private bindEvents(): void {
    const { input, caseButton, wordButton, regexButton, settingsButton, body } = this.layout;
    input.addEventListener("input", () => {
      if (this.editingBox) return;
      this.state.completionsOpen = true;
      this.state.completionIndex = 0;
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
      this.editQuery((text) => toggleCase(text, this.state.parsed, this.state.ui.caseSensitive));
    });
    wordButton.addEventListener("click", () => {
      this.editQuery((text) => toggleWord(text, this.state.parsed, this.state.ui.wholeWord));
    });
    regexButton.addEventListener("click", () => {
      this.editQuery((text) => toggleRegex(text, this.state.parsed));
    });
    settingsButton.addEventListener("click", () => {
      send("settings.open", {});
    });

    body.addEventListener("click", (event) => {
      const ref = rowReference(event);
      if (!ref) return;
      this.selection.select(ref);
      this.keepKeysInBox();
      if (this.state.ui.openTrigger === "singleClick") this.open(ref, event);
    });
    body.addEventListener("dblclick", (event) => {
      const ref = rowReference(event);
      if (ref && this.state.ui.openTrigger === "doubleClick") this.open(ref, event);
    });
  }

  /**
   * A result row isn't focusable, so a click on one leaves the focus on the
   * page; the box takes it back, so ↑↓, ↵ and Esc go on from the clicked
   * result (with the suggestions the click closed kept closed). Text dragged
   * over in a row stays selected, to be copied.
   */
  private keepKeysInBox(): void {
    if (document.getSelection()?.isCollapsed === false) return;
    this.state.completionsOpen = false;
    this.layout.input.focus({ preventScroll: true });
  }

  /** ⌘ or Ctrl opens to the side. */
  private open(ref: string, event: MouseEvent | KeyboardEvent): void {
    const where: OpenWhere = event.metaKey || event.ctrlKey ? "side" : "current";
    send("result.open", { ref, where });
  }

  /** The text the newest parse is of; before any parse, the empty box. */
  private get parsedText(): string {
    return this.state.parsed?.raw ?? "";
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
    // While an input method composes (Japanese, Chinese, Korean…), Enter, Esc and the arrows are its keys.
    if (event.isComposing) return;
    const handled = this.handleKey(event);
    if (handled) event.preventDefault();
  }

  /**
   * ? while operators are suggested opens the help page; in an empty box it
   * brings the operator sheet into view, which a narrow panel stacks below
   * the recent queries. Returns false for a literal ?, as in a value: f:'s
   * globs and regexes and a /regex/ use it.
   */
  private showOperators(): boolean {
    if (this.completionsVisible && this.state.completions[0]?.group === "operator") {
      send("help.open", {}); // every operator, explained
      return true;
    }
    if (this.layout.input.value) return false;
    this.layout.body.querySelector(".sheet")?.scrollIntoView({ block: "nearest" });
    return true;
  }

  /** Returns whether the key was handled (and its default should be prevented). */
  private handleKey(event: KeyboardEvent): boolean {
    const modifier = event.metaKey || event.ctrlKey;
    switch (event.key) {
      case "?": {
        return this.showOperators();
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
      case "Backspace": {
        return this.emptyBox.removeSelected(event);
      }
      default: {
        return this.emptyBox.watchSelected(event);
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
    if (this.repoMenu.isOpen) this.repoMenu.toggle(false);
    else if (this.completionsVisible) this.searchAsTyped();
    else send("panel.close", {});
  }

  /** ↑↓ move through suggestions when they are open, otherwise through rows. */
  private onArrow(step: 1 | -1): void {
    if (this.completionsVisible) {
      this.state.completionIndex = wrapIndex(this.state.completionIndex, step, this.state.completions.length);
      this.renderCompletions();
    } else if (this.layout.input.value) {
      this.selection.move(step);
    } else {
      this.emptyBox.moveSelection(step);
    }
  }

  /** Enter searches as typed while suggesting (Tab accepts), runs a pinned or recent query, or opens the selected result. */
  private onEnter(event: KeyboardEvent, modifier: boolean): void {
    if (this.completionsVisible && !modifier) this.searchAsTyped();
    else if (!this.layout.input.value) this.emptyBox.runSelected();
    else if (this.state.selectedRef) this.open(this.state.selectedRef, event);
  }

  // ------------------------------------------------------------ host messages

  private onHostMessage(message: Exclude<HostMessage, { type: "syntax.lines" | "remotes.state" }>): void {
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
        this.onSearchDone(message.payload);
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
      case "watch.state": {
        this.onWatchState(message.payload);
        return;
      }
      case "welcome.state": {
        return; // for the welcome page
      }
      case "replace.plan": {
        this.replace.onPlan(message.payload);
        return;
      }
      case "replace.done": {
        this.replace.onDone(message.payload);
        return;
      }
    }
  }

  /** Which pinned queries can be watched and what each watch found: the empty box shows them. */
  private onWatchState({ watchable, unseen }: WatchStateMessage): void {
    this.state.watchable = new Set(watchable);
    this.state.unseen = new Map(unseen.map((entry) => [entry.query, entry]));
    // Redrawn only while it shows, and never under a name being typed.
    if (!this.layout.input.value && !this.results && this.state.naming === undefined) this.showEmptyState();
  }

  /** A batch of results for the current search: appended, and the first one selected. */
  private onSearchBatch({ seq, searchId, items }: SearchBatchMessage): void {
    if (!this.isCurrent(seq, searchId)) return;
    this.resultsFor(searchId).append(items);
    this.renderSummary();
    this.selection.selectFirst();
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

  /**
   * The host's saved box text, recent and pinned queries, and settings: on
   * opening, and, without the text, after settings change.
   */
  private onRestore({ text, seeded, recent, pinned, settings }: StateRestoreMessage): void {
    const { input } = this.layout;
    if (settings) this.state.ui = settings;
    applyCodeSettings(this.state.ui);
    this.state.recent = recent;
    this.state.pinned = pinned;
    // Saving a pin changes the settings, which sends this again: a name being typed stays as it is.
    if (this.state.naming !== undefined && pinned.some((entry) => entry.query === this.state.naming)) {
      this.emptyBox.clampIndex();
      return;
    }
    this.state.naming = undefined;
    // A shorter list mustn't leave ↵ pointing past its end.
    this.emptyBox.clampIndex();
    if (text !== undefined && text !== input.value) setBoxText(input, text, seeded === true);
    if (input.value) this.queryChanged(true);
    else this.showEmptyState();
    this.renderFooter();
    input.focus();
  }

  /**
   * A batch or done message belongs to the newest query, or extends the
   * search on screen (Load more, or results kept while the box has errors)
   * until a newer query replaces it.
   */
  private isCurrent(seq: number, searchId: string): boolean {
    return seq === this.state.seq || (searchId === this.state.searchId && !this.state.searchReplaced);
  }

  /** The daemon's reading of the box: updates the toggles, suggestions, diagnostics and chips. */
  private onParsed({ query, completions, searchText }: ParseResultMessage): void {
    const state = this.state;
    state.parsed = query;
    state.completions = completions;
    state.searchText = searchText;
    if (state.completionIndex >= completions.length) state.completionIndex = 0;
    const errors = hasErrors(state);
    if (!errors) state.searchReplaced = true; // the host searches this query instead

    const { caseButton, wordButton, regexButton, shell, input } = this.layout;
    renderCaseToggle(caseButton, query, state.ui.caseSensitive);
    wordButton.setAttribute("aria-pressed", String(isWordPressed(query, state.ui.wholeWord)));
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
    this.renderFacets();
    if (!query.root && !errors) this.showEmptyState();
    this.renderFooter();
  }

  /** The results view for `searchId`, starting a fresh one the first time an id is seen. */
  private resultsFor(searchId: string): ResultsView {
    if (this.results && searchId === this.state.searchId) return this.results;
    const state = this.state;
    state.searchId = searchId;
    state.searchReplaced = false;
    state.done = undefined;
    state.searchedAt = hasErrors(state) ? undefined : state.parsed?.globals.at;
    state.newCommits = new Set();
    state.expandedFacets.clear(); // +N shows every bucket of this search only
    this.selection.keepFrom(this.results);
    state.selectedRef = "";
    state.preview = undefined;
    this.layout.input.removeAttribute("aria-activedescendant"); // the old results' rows go
    // A note's undo indexes the text this search ran on, which may be the box without the word being completed.
    const searchedText = state.searchText ?? state.parsed?.raw;
    const handlers = {
      onApplyFix: (fix: Fix) => {
        this.applyFix(fix, searchedText);
      },
      // Load more fetches the next page of this same search; its batches carry the same searchId.
      onLoadMore: (id: string, cursor: string) => {
        send("results.more", { searchId: id, cursor });
      },
      // The host saves the setting, and the query runs again with it; the box keeps the keys meanwhile.
      onContextLines: (lines: number) => {
        send("context.set", { lines });
        this.layout.input.focus();
      },
      // An order typed in the query is rewritten there; otherwise the host saves the setting and searches again.
      onOrder: (base: "best" | "file" | "recent", descending: boolean) => {
        if (topLevelOperators(state.parsed, "order").length === 0)
          send("order.set", { order: joinOrder(base, descending) });
        else this.editQuery((text) => setOrder(text, state.parsed, joinOrder(base, descending, true)), searchedText);
        this.layout.input.focus();
      },
      replacementsFor: (item: { repoId: string; path: string; line: number }) =>
        this.replace.replacementsFor(item.repoId, item.path, item.line),
      paintCode: this.painter.paintCode.bind(this.painter),
    };
    const errors = hasErrors(state);
    // Named once its results show: a query whose results never came isn't the one on screen.
    if (!errors && state.parsed?.root) state.lastGoodText = state.parsed.raw;
    this.results = new ResultsView(this.layout.body, state, handlers, errors ? [] : pathScope(state.parsed));
    this.selection.watchPane(this.results.preview);
    this.results.showLastGood(errors ? state.lastGoodText : undefined);
    this.replace.refresh(); // a new search: preview replacing its matches
    return this.results;
  }

  private onSearchDone(done: SearchDoneMessage): void {
    if (!this.isCurrent(done.seq, done.searchId)) return;
    const results = this.resultsFor(done.searchId); // first: a search that found nothing starts its view here
    this.state.done = done;
    this.state.newCommits = new Set(done.newCommits);
    results.markNew();
    results.finish(done);
    this.selection.settle();
    if (!hasErrors(this.state)) renderChips(this.layout, this.state, this.summary); // the chip names the commit read
    this.renderSummary();
    this.renderFacets();
    if (this.completionsVisible) this.renderCompletions();
  }

  // ------------------------------------------------------------ rendering

  /** Shows the empty box: recent and pinned queries, and the operator sheet. */
  showEmptyState(): void {
    const state = this.state;
    state.searchId = "";
    state.selectedRef = "";
    state.preview = undefined;
    this.results = undefined;
    this.selection.forget();
    this.replace.refresh();
    this.layout.chips.replaceChildren();
    this.layout.facets.replaceChildren();
    this.layout.diagnostics.replaceChildren();
    renderEmptyState(this.layout.body, state, this.emptyBox.handlers());
    this.pointAtActiveOption(); // the old results' rows are gone
    this.renderFooter();
  }

  private renderCompletions(): void {
    const glimpse: ResultsGlimpse | undefined = this.results
      ? { total: this.state.done?.total ?? this.results.items.length, lines: this.results.firstLines(GLIMPSE_LINES) }
      : undefined;
    renderCompletions(this.layout, this.state, glimpse, {
      onPick: (index) => {
        this.acceptCompletion(index);
      },
      onSearchAsTyped: () => {
        this.searchAsTyped();
      },
    });
    this.pointAtActiveOption();
    this.renderFooter();
  }

  /** Tells screen readers which option ↑↓ is on (see pointAtActiveOption). */
  private pointAtActiveOption(): void {
    const row = this.state.selectedRef ? this.results?.rowFor(this.state.selectedRef) : undefined;
    pointAtActiveOption(this.layout.input, this.completionsVisible ? this.state.completionIndex : undefined, row);
  }

  private renderBanners(): void {
    renderBanners(this.layout, this.state, () => {
      send("daemon.restart", {});
    });
  }

  /** The key hints for what the panel shows now. */
  renderFooter(): void {
    const mode = this.footerMode();
    const emptyRow = mode === "empty" ? this.emptyBox.selectedRow() : undefined;
    renderFooter(this.layout, mode, this.state.parsed?.mode === "history", emptyRow);
  }

  /** What the panel shows right now, which picks the key hints. */
  private footerMode(): FooterMode {
    if (document.activeElement === this.layout.replaceRow.input) return "replace";
    if (!this.layout.input.value) return "empty";
    if (this.completionsVisible) return this.state.completions[0]?.group === "operator" ? "operators" : "values";
    return hasErrors(this.state) ? "errors" : "results";
  }

  private renderPreview(): void {
    if (!this.results) return;
    const item = this.state.preview ? this.results.itemFor(this.state.preview.ref) : undefined;
    renderPreview(this.results.preview, this.state, previewOf(this.state, item), {
      onOpen: (ref, where, position) => {
        send("result.open", position ? { ref, where, position } : { ref, where });
      },
      onShowHiddenFiles: () => {
        this.state.showHiddenFiles = true;
        this.renderPreview();
      },
      replacementsFor: (path, line) => (item ? this.replace.replacementsFor(item.repoId, path, line) : undefined),
      paint: (code, path, text) => {
        this.painter.paint(code, path, text);
      },
    });
  }

  /** The facet row; a click edits the query, and the search that follows redraws it. */
  private renderFacets(): void {
    renderFacets(this.layout.facets, this.state, {
      onToggle: (filter, exclude) => {
        if (this.editQuery((text) => toggleFacet(text, this.state.parsed, filter, exclude))) this.layout.input.focus();
      },
      onExpand: (field) => {
        this.state.expandedFacets.add(field);
        this.renderFacets();
      },
    });
  }

  /** "3 file names · 5 code matches" or "4 commits in 3 repos", beside the chips. */
  private renderSummary(): void {
    this.summary.textContent = summaryText(this.results, this.state.parsed?.mode === "history", this.state.done);
  }
}

/** The ref of the result row an event happened in, if any. */
function rowReference(event: Event): string | undefined {
  return (event.target as HTMLElement).closest<HTMLElement>("[data-ref]")?.dataset["ref"];
}
