// The result list: sections for file names, definitions, code and commits,
// appended to as batches stream in, plus hidden-result notes and Load more.
import {
  button,
  codeLineRow,
  codeList,
  element,
  fileStat,
  highlight,
  plural,
  shortSha,
  termClass,
  timeAgo,
} from "../format";
import { operatorNodes, textNodes, textTerms } from "../parsedQuery";
import type { Fix, HiddenNote, ResultItem, SearchDoneMessage } from "../protocol.gen";
import { repoName, type ViewState } from "../state";

type ResultKind = ResultItem["kind"];
type ItemOf<K extends ResultKind> = Extract<ResultItem, { kind: K }>;

/** Changed files listed under a commit before "+N more". */
const COMMIT_FILES_SHOWN = 2;

const SINGULAR_UNIT: Record<HiddenNote["unit"], string> = {
  matches: "match",
  files: "file",
  commits: "commit",
  definitions: "definition",
};

/** What undoing a filter does, where "Show them" would mislead. */
const UNDO_LABEL: Partial<Record<HiddenNote["reason"], string>> = {
  // Undoing case:yes changes how text matches rather than showing hidden rows.
  case: "Ignore case",
  type: "Show code too",
};

/** One section of the list (file names, definitions, code or commits). */
interface Section {
  root: HTMLElement;
  list: HTMLElement;
  count: HTMLElement;
}

/** What the result list's buttons do. */
export interface ResultsHandlers {
  onApplyFix(fix: Fix): void;
  onLoadMore(searchId: string, cursor: string): void;
}

/** How many results of each kind are on screen, plus the files and repos they come from. */
export interface ResultCounts extends Record<ResultKind, number> {
  /** Distinct files among the code lines. */
  codeFiles: number;
  /** Distinct repos among all results. */
  repos: number;
}

/**
 * A DOM id for a result row. Refs are opaque, so they are encoded, never
 * parsed: every character but letters, digits and "-" becomes "_<hex code>_",
 * so two different refs never share an id.
 */
function rowId(ref: string): string {
  return "r-" + ref.replaceAll(/[^A-Za-z0-9-]/gu, (character) => `_${(character.codePointAt(0) ?? 0).toString(16)}_`);
}

/** Identifies a result's file across repos; code lines with the same key share a header. */
const fileKey = (item: ResultItem) => `${item.repoId}\0${"path" in item ? item.path : ""}`;

/** The file whose code lines are being appended, and the header that counts them. */
interface OpenCodeFile {
  key: string;
  /** The header's count, updated as the file's lines arrive. */
  countLabel: HTMLElement;
  lineCount: number;
}

/**
 * The result list and preview containers for one search. Rows are appended
 * per batch rather than re-rendered, so large result sets stream smoothly.
 */
export class ResultsView {
  /** Every result appended so far, in arrival order. */
  readonly items: ResultItem[] = [];
  readonly list: HTMLElement;
  readonly preview: HTMLElement;
  private readonly sections: Record<ResultKind, Section>;
  private readonly notes: HTMLElement;
  private readonly loadMore: HTMLElement;
  private readonly lastGood: HTMLElement;
  /** Results per kind so far; append() keeps it current, so counting never rescans `items`. */
  private readonly countByKind: Record<ResultKind, number> = { file: 0, line: 0, symbol: 0, commit: 0 };
  /** The files (fileKey) the code lines so far come from. */
  private readonly codeFileKeys = new Set<string>();
  /** The repos the results so far come from. */
  private readonly repoIds = new Set<string>();
  /** Set while code lines of one file arrive in a row, so they share its header. */
  private openCodeFile: OpenCodeFile | undefined;

  /** Replaces `body` with an empty list and preview; `scope` is the query's path patterns (pathScope). */
  constructor(
    body: HTMLElement,
    private readonly state: ViewState,
    private readonly handlers: ResultsHandlers,
    scope: string[] = [],
  ) {
    this.sections = {
      file: makeSection("File names", "section-files"),
      symbol: makeSection("Definitions", "section-symbols"),
      line: makeSection(scope.length > 0 ? "Code in matching paths" : "Code", "section-code"),
      commit: makeSection("Commits · newest first", "section-commits"),
    };
    if (scope.length > 0) this.sections.line.list.before(pathScopeNote(scope));
    this.notes = element("div", { class: "notes", "data-testid": "hidden-notes" });
    this.loadMore = element("div", { class: "more" });
    this.lastGood = element("div", { class: "last-good muted", "data-testid": "last-good", hidden: true });
    this.list = element(
      "div",
      { id: "results", role: "listbox", "aria-label": "Results", "data-testid": "results" },
      this.lastGood,
      this.sections.file.root,
      this.sections.symbol.root,
      this.sections.line.root,
      this.sections.commit.root,
      this.notes,
      this.loadMore,
    );
    this.preview = element("div", { id: "preview", "data-testid": "preview", "aria-live": "polite" });
    body.replaceChildren(element("div", { class: "split" }, this.list, this.preview));
  }

  /**
   * While the query box has errors, says which query the results on screen
   * are for; undefined hides the line again.
   */
  showLastGood(text: string | undefined): void {
    this.lastGood.hidden = text === undefined;
    this.lastGood.replaceChildren(
      ...(text === undefined ? [] : ["Showing results for the last query that worked ", element("code", {}, text)]),
    );
  }

  /** Every selectable row, in visual order. */
  rows(): HTMLElement[] {
    return [...this.list.querySelectorAll<HTMLElement>("[data-ref]")];
  }

  /** Adds a batch of results to their sections, skipping any of a kind this webview doesn't know. */
  append(items: ResultItem[]): void {
    for (const item of items) {
      if (!Object.hasOwn(this.sections, item.kind)) continue;
      this.items.push(item);
      this.countByKind[item.kind]++;
      this.repoIds.add(item.repoId);
      if (item.kind === "line") this.codeFileKeys.add(fileKey(item));
      const section = this.sections[item.kind];
      section.root.hidden = false;
      section.list.append(this.renderItem(item));
    }
    this.updateSectionCounts();
  }

  /** Greys out a row whose file or commit no longer exists. */
  markStale(ref: string): void {
    this.list.querySelector(`#${rowId(ref)}`)?.classList.add("stale");
  }

  /** How many results are on screen, by kind, file and repo. */
  counts(): ResultCounts {
    return { ...this.countByKind, codeFiles: this.codeFileKeys.size, repos: this.repoIds.size };
  }

  /** Renders hidden-result notes, Load more, an error, or the empty result. */
  finish(done: SearchDoneMessage): void {
    this.notes.replaceChildren(...done.hidden.filter((note) => note.count > 0).map((note) => this.renderNote(note)));
    this.loadMore.replaceChildren();
    // The daemon sends a cursor when there are more pages; Load more asks for the next one.
    if (done.nextCursor) {
      const cursor = done.nextCursor;
      this.loadMore.append(
        button(
          { class: "btn", "data-testid": "load-more" },
          () => {
            this.handlers.onLoadMore(done.searchId, cursor);
          },
          "Load more",
        ),
      );
    }
    if (done.error) {
      this.preview.replaceChildren(element("div", { class: "notice error" }, done.error));
    } else if (this.items.length === 0) {
      this.showNoResults(done.hidden.filter((note) => note.count > 0));
    }
    this.updateSectionCounts();
  }

  /**
   * "No symbol is named retrypolicy", with why when case:yes is on, above
   * the notes that say what each change would find.
   */
  private showNoResults(notes: HiddenNote[]): void {
    this.preview.replaceChildren();
    this.list.querySelector(".none")?.remove();
    const symbols = operatorNodes(this.state.parsed, "sym").map((node) => node.value);
    const title = symbols.length > 0 ? `No symbol is named ${symbols.join(" or ")}` : "No results";
    let hint = "Try fewer terms, or check the operators with ?";
    if (notes.some((note) => note.reason === "case")) hint = "case:yes is on, so capital letters have to match.";
    else if (notes.length > 0) hint = "Some results are hidden by filters below.";
    this.list.prepend(
      element(
        "div",
        { class: "none", "data-testid": "no-results" },
        element("span", { class: "strong" }, title),
        element("span", { class: "muted" }, hint),
      ),
    );
  }

  /**
   * "3 commits hidden by -f:vendor/ · Show them", or "38 code matches for
   * retry hidden by type:file · Show code too".
   */
  private renderNote(note: HiddenNote): HTMLElement {
    if (note.reason === "symbol") return this.renderTextSearchNote(note);
    const showThem = this.showHiddenButton(note, UNDO_LABEL[note.reason] ?? "Show them");
    return element(
      "div",
      { class: "note", "data-reason": note.reason },
      element("span", {}, `${this.hiddenWhat(note)} hidden by `, element("code", {}, note.filter)),
      showThem,
    );
  }

  /** "Want every usage, not just definitions? · Search RetryPolicy as text · 23". */
  private renderTextSearchNote(note: HiddenNote): HTMLElement {
    const search = this.showHiddenButton(note, `${note.undo.title} · ${note.count.toLocaleString("en-US")}`);
    return element(
      "div",
      { class: "note", "data-reason": note.reason },
      element("span", {}, "Want every usage, not just definitions?"),
      search,
    );
  }

  /** The button that undoes the filter that hid `note`'s results. */
  private showHiddenButton(note: HiddenNote, label: string): HTMLButtonElement {
    return button(
      { class: "btn link", "data-testid": "show-hidden" },
      () => {
        this.handlers.onApplyFix(note.undo);
      },
      label,
    );
  }

  /** "3 commits", or for type:file "38 code matches for retry": what the filter hid. */
  private hiddenWhat(note: HiddenNote): string {
    const count = note.count.toLocaleString("en-US");
    const unit = note.count === 1 ? SINGULAR_UNIT[note.unit] : note.unit;
    if (note.reason !== "type") return `${count} ${unit}`;
    const terms = textTerms(this.state.parsed);
    return `${count} code ${unit}` + (terms.length > 0 ? ` for ${terms.join(" and ")}` : "");
  }

  private updateSectionCounts(): void {
    const counts = this.counts();
    this.sections.file.count.textContent = String(counts.file);
    this.sections.symbol.count.textContent = String(counts.symbol);
    this.sections.line.count.textContent =
      counts.line > 0 ? `${counts.line} in ${plural(counts.codeFiles, "file")}` : "";
    this.sections.commit.count.textContent = String(counts.commit);
  }

  private renderItem(item: ResultItem): HTMLElement {
    switch (item.kind) {
      case "file": {
        return this.renderFileRow(item);
      }
      case "line": {
        return this.renderCodeRow(item);
      }
      case "symbol": {
        return this.renderSymbolRow(item);
      }
      case "commit": {
        return this.renderCommitRow(item);
      }
    }
  }

  /** Attributes every result row shares: id, ARIA option role and the opaque ref. */
  private rowAttributes(item: ResultItem): Record<string, string> {
    return {
      id: rowId(item.ref),
      role: "option",
      "aria-selected": "false",
      "data-ref": item.ref,
      "data-kind": item.kind,
      "data-testid": "result",
    };
  }

  private renderFileRow(item: ItemOf<"file">): HTMLElement {
    return element(
      "div",
      { ...this.rowAttributes(item), class: "row file", title: item.path },
      element("span", { class: "path" }, highlight(item.path, item.nameHits)),
      item.dirty ? element("span", { class: "badge warn" }, "Uncommitted changes") : null,
      item.lastCommit
        ? element("span", { class: "meta" }, `${item.lastCommit.author} · ${timeAgo(item.lastCommit.at)}`)
        : null,
      element("span", { class: "repo" }, repoName(this.state, item.repoId)),
    );
  }

  /** A code line, preceded by a file header when it starts a new file. */
  private renderCodeRow(item: ItemOf<"line">): HTMLElement {
    const group = element("div", { class: "contents" });
    const key = fileKey(item);
    if (this.openCodeFile?.key !== key) {
      const countLabel = element("span", { class: "count" });
      group.append(
        element(
          "div",
          { class: "group", "data-testid": "code-group" },
          element("span", { class: "path" }, item.path),
          element("span", { class: "repo" }, repoName(this.state, item.repoId)),
          countLabel,
        ),
      );
      this.openCodeFile = { key, countLabel, lineCount: 0 };
    }
    this.openCodeFile.lineCount++;
    this.openCodeFile.countLabel.textContent = String(this.openCodeFile.lineCount);
    group.append(codeLineRow(item, this.rowAttributes(item)));
    return group;
  }

  private renderSymbolRow(item: ItemOf<"symbol">): HTMLElement {
    return element(
      "div",
      { ...this.rowAttributes(item), class: "row symbol" },
      element("span", { class: `badge kind-${item.symbolKind}` }, item.symbolKind),
      element("code", { class: "name" }, highlight(item.name, item.hits)),
      element("span", { class: "path muted" }, `${item.path}:${item.line}`),
      element("span", { class: "repo" }, repoName(this.state, item.repoId)),
    );
  }

  private renderCommitRow(item: ItemOf<"commit">): HTMLElement {
    // Each commit is tagged with the OR terms it matched, in their colors.
    const terms = textNodes(this.state.parsed);
    const tags = item.matchedTerms.map((termIndex) => {
      const term = terms.find((candidate) => candidate.termIndex === termIndex);
      return term ? element("span", { class: `tag ${termClass(termIndex)}` }, term.value) : null;
    });
    const files: HTMLElement[] = item.files
      .slice(0, COMMIT_FILES_SHOWN)
      .map((file) => fileStat(file.path, file.added, file.removed));
    if (item.files.length > COMMIT_FILES_SHOWN)
      files.push(element("span", { class: "muted" }, `+${item.files.length - COMMIT_FILES_SHOWN} more`));
    return element(
      "div",
      { ...this.rowAttributes(item), class: "row commit" },
      element(
        "div",
        { class: "commit-title" },
        element("span", { class: "subject" }, highlight(item.subject, item.subjectHits)),
        element("code", { class: "sha" }, shortSha(item.sha)),
      ),
      element(
        "div",
        { class: "commit-meta muted" },
        element("span", {}, item.author.name),
        element("span", {}, timeAgo(item.at)),
        element("span", {}, repoName(this.state, item.repoId)),
        commitMatchPlace(item),
        ...tags,
      ),
      item.bodyLine
        ? element("div", { class: "commit-body-line" }, highlight(item.bodyLine.text, item.bodyLine.hits))
        : null,
      element("div", { class: "commit-files" }, ...files),
    );
  }
}

/** Where a commit's text terms matched: "in message · 3 hits in diff", "in message only" or "3 hits in diff". */
function commitMatchPlace(item: ItemOf<"commit">): HTMLElement | null {
  const inDiff = item.diffHits ? plural(item.diffHits, "hit") + " in diff" : "";
  if (item.inMessage) return element("span", {}, inDiff ? `in message · ${inDiff}` : "in message only");
  return inDiff ? element("span", {}, inDiff) : null;
}

/** "Only files whose full path matches .*test\.py$ are searched.", above the code section. */
function pathScopeNote(scope: string[]): HTMLElement {
  const patterns = codeList(scope);
  return element(
    "div",
    { class: "scope muted", "data-testid": "path-scope" },
    "Only files whose full path matches ",
    ...patterns,
    " are searched.",
  );
}

function makeSection(title: string, testId: string): Section {
  const count = element("span", { class: "muted" });
  const list = element("div", { class: "list" });
  const root = element(
    "div",
    { class: "result-section", "data-testid": testId, hidden: true },
    element("div", { class: "section-title spread" }, element("span", {}, title), count),
    list,
  );
  return { root, list, count };
}
