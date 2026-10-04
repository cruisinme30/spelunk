// The result list: sections for file names, definitions, code and commits,
// appended to as batches stream in, plus hidden-result notes and Load more.
import { el, fileStat, highlight, plural, shortSha, termClass, timeAgo, trimIndent } from "../format";
import type { Fix, HiddenNote, Node as QueryNode, ParsedQuery, ResultItem, SearchDoneMsg } from "../protocol.gen";
import { textNodes } from "../queryEdit";
import { repoName, type ViewState } from "../state";

type ResultKind = ResultItem["kind"];
type ItemOf<K extends ResultKind> = Extract<ResultItem, { kind: K }>;

/** Changed files listed under a commit before "+N more". */
const COMMIT_FILES_SHOWN = 2;

const SINGULAR_UNIT: Record<HiddenNote["unit"], string> = { matches: "match", files: "file", commits: "commit" };

/** What undoing a filter does, where "Show them" would mislead. */
const UNDO_LABEL: Partial<Record<HiddenNote["reason"], string>> = {
  // Undoing case:yes changes how text matches rather than showing hidden rows (mock 8).
  case: "Ignore case",
  type: "Show code too",
};

/** The plain words of a parsed query, in order. */
export function textTerms(query: ParsedQuery | undefined): string[] {
  const words: string[] = [];
  const visit = (node: QueryNode): void => {
    if (node.kind === "text") words.push(node.value);
    else if (node.kind === "and" || node.kind === "or") node.children.forEach(visit);
    else if (node.kind === "not") visit(node.child);
  };
  if (query?.root) visit(query.root);
  return words;
}

interface Section {
  root: HTMLElement;
  list: HTMLElement;
  count: HTMLElement;
}

export interface ResultsHandlers {
  onApplyFix(fix: Fix): void;
  onLoadMore(searchId: string, cursor: string): void;
}

export interface ResultCounts extends Record<ResultKind, number> {
  codeFiles: number;
  repos: number;
}

/**
 * The path patterns that scope a search: the values of top-level positive
 * f: operators. Code results then come only from matching paths (mock 2).
 */
export function pathScope(query: ParsedQuery | undefined): string[] {
  const root = query?.root;
  if (!root) return [];
  const conjuncts = root.kind === "and" ? root.children : [root];
  return conjuncts.flatMap((node) => (node.kind === "op" && node.op === "f" ? [node.value] : []));
}

/** A DOM id for a result row. Refs are opaque, so they are sanitized, never parsed. */
export function rowId(ref: string): string {
  return "r-" + ref.replace(/[^A-Za-z0-9_-]/g, "_");
}

/** Code results from one file share a group header. */
const fileKey = (item: ResultItem) => `${item.repoId}\0${"path" in item ? item.path : ""}`;

/**
 * The result list and preview containers for one search. Rows are appended
 * per batch rather than re-rendered, so large result sets stream smoothly.
 */
export class ResultsView {
  readonly items: ResultItem[] = [];
  readonly list: HTMLElement;
  readonly preview: HTMLElement;
  private readonly sections: Record<ResultKind, Section>;
  private readonly notes: HTMLElement;
  private readonly loadMore: HTMLElement;
  private readonly lastGood: HTMLElement;
  private codeGroup?: { key: string; count: HTMLElement; matches: number };

  constructor(
    body: HTMLElement,
    private readonly state: ViewState,
    private readonly handlers: ResultsHandlers,
    scope: string[] = [],
  ) {
    this.sections = {
      file: makeSection("File names", "section-files"),
      symbol: makeSection("Definitions", "section-symbols"),
      line: makeSection(scope.length ? "Code in matching paths" : "Code", "section-code"),
      commit: makeSection("Commits · newest first", "section-commits"),
    };
    if (scope.length) {
      const patterns = scope.flatMap((pattern, i) =>
        i ? [" and ", el("code", {}, pattern)] : [el("code", {}, pattern)],
      );
      this.sections.line.list.before(
        el(
          "div",
          { class: "scope muted", "data-testid": "path-scope" },
          "Only files whose full path matches ",
          ...patterns,
          " are searched.",
        ),
      );
    }
    this.notes = el("div", { class: "notes", "data-testid": "hidden-notes" });
    this.loadMore = el("div", { class: "more" });
    this.lastGood = el("div", { class: "lastgood muted", "data-testid": "last-good", hidden: true });
    this.list = el(
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
    this.preview = el("div", { id: "preview", "data-testid": "preview", "aria-live": "polite" });
    body.replaceChildren(el("div", { class: "split" }, this.list, this.preview));
  }

  /**
   * While the query box has errors, says which query the results on screen
   * are for (mock 13); undefined hides the line again.
   */
  showLastGood(text: string | undefined): void {
    this.lastGood.hidden = text === undefined;
    this.lastGood.replaceChildren(
      ...(text === undefined ? [] : ["Showing results for the last query that worked ", el("code", {}, text)]),
    );
  }

  /** Every selectable row, in visual order. */
  rows(): HTMLElement[] {
    return Array.from(this.list.querySelectorAll<HTMLElement>("[data-ref]"));
  }

  append(items: ResultItem[]): void {
    for (const item of items) {
      this.items.push(item);
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

  counts(): ResultCounts {
    const counts: ResultCounts = { file: 0, line: 0, symbol: 0, commit: 0, codeFiles: 0, repos: 0 };
    const codeFiles = new Set<string>();
    const repos = new Set<string>();
    for (const item of this.items) {
      counts[item.kind]++;
      repos.add(item.repoId);
      if (item.kind === "line") codeFiles.add(fileKey(item));
    }
    counts.codeFiles = codeFiles.size;
    counts.repos = repos.size;
    return counts;
  }

  /** Renders hidden-result notes, Load more, an error, or the empty result. */
  finish(done: SearchDoneMsg): void {
    this.notes.replaceChildren(...done.hidden.filter((note) => note.count > 0).map((note) => this.renderNote(note)));
    this.loadMore.replaceChildren();
    if (done.nextCursor) {
      const cursor = done.nextCursor;
      const button = el("button", { type: "button", class: "btn", "data-testid": "load-more" }, "Load more");
      button.addEventListener("click", () => this.handlers.onLoadMore(done.searchId, cursor));
      this.loadMore.append(button);
    }
    if (done.error) {
      this.preview.replaceChildren(el("div", { class: "notice error" }, done.error));
    } else if (!this.items.length) {
      this.showNoResults(done.hidden.some((note) => note.count > 0));
    }
    this.updateSectionCounts();
  }

  private showNoResults(someHidden: boolean): void {
    this.preview.replaceChildren();
    this.list.querySelector(".none")?.remove();
    const hint = someHidden
      ? "Some results are hidden by filters below."
      : "Try fewer terms, or check the operators with ?";
    this.list.prepend(
      el(
        "div",
        { class: "none", "data-testid": "no-results" },
        el("span", { class: "strong" }, "No results"),
        el("span", { class: "muted" }, hint),
      ),
    );
  }

  /**
   * "3 commits hidden by -f:vendor/ · Show them" (mock 7), "38 code matches
   * for retry hidden by type:file · Show code too" (mock 12).
   */
  private renderNote(note: HiddenNote): HTMLElement {
    const unit = note.count === 1 ? SINGULAR_UNIT[note.unit] : note.unit;
    let what = `${note.count.toLocaleString("en-US")} ${unit}`;
    if (note.reason === "type") {
      const terms = textTerms(this.state.parsed);
      what = `${note.count.toLocaleString("en-US")} code ${unit}` + (terms.length ? ` for ${terms.join(" and ")}` : "");
    }
    const showThem = el(
      "button",
      { type: "button", class: "btn link", "data-testid": "show-hidden" },
      UNDO_LABEL[note.reason] ?? "Show them",
    );
    showThem.addEventListener("click", () => this.handlers.onApplyFix(note.undo));
    return el(
      "div",
      { class: "note", "data-reason": note.reason },
      el("span", {}, `${what} hidden by `, el("code", {}, note.filter)),
      showThem,
    );
  }

  private updateSectionCounts(): void {
    const counts = this.counts();
    this.sections.file.count.textContent = String(counts.file);
    this.sections.symbol.count.textContent = String(counts.symbol);
    this.sections.line.count.textContent = counts.line ? `${counts.line} in ${plural(counts.codeFiles, "file")}` : "";
    this.sections.commit.count.textContent = String(counts.commit);
  }

  private renderItem(item: ResultItem): HTMLElement {
    switch (item.kind) {
      case "file":
        return this.renderFileRow(item);
      case "line":
        return this.renderCodeRow(item);
      case "symbol":
        return this.renderSymbolRow(item);
      case "commit":
        return this.renderCommitRow(item);
    }
  }

  /** Attributes every result row shares: id, ARIA option role and the opaque ref. */
  private rowAttributes(item: ResultItem) {
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
    return el(
      "div",
      { ...this.rowAttributes(item), class: "row file", title: item.path },
      el("span", { class: "path" }, highlight(item.path, item.nameHits)),
      item.dirty ? el("span", { class: "badge warn" }, "Uncommitted changes") : null,
      item.lastCommit
        ? el("span", { class: "meta" }, `${item.lastCommit.author} · ${timeAgo(item.lastCommit.at)}`)
        : null,
      el("span", { class: "repo" }, repoName(this.state, item.repoId)),
    );
  }

  /** A code line, preceded by a file header when it starts a new file. */
  private renderCodeRow(item: ItemOf<"line">): HTMLElement {
    const group = el("div", { class: "contents" });
    const key = fileKey(item);
    if (this.codeGroup?.key !== key) {
      const count = el("span", { class: "count" });
      group.append(
        el(
          "div",
          { class: "group", "data-testid": "code-group" },
          el("span", { class: "path" }, item.path),
          el("span", { class: "repo" }, repoName(this.state, item.repoId)),
          count,
        ),
      );
      this.codeGroup = { key, count, matches: 0 };
    }
    this.codeGroup.matches++;
    this.codeGroup.count.textContent = String(this.codeGroup.matches);
    const shown = trimIndent(item.text, item.hits);
    group.append(
      el(
        "div",
        { ...this.rowAttributes(item), class: "row line" },
        el("span", { class: "ln" }, String(item.line)),
        el("code", { class: "text" }, highlight(shown.text, shown.hits)),
      ),
    );
    return group;
  }

  private renderSymbolRow(item: ItemOf<"symbol">): HTMLElement {
    return el(
      "div",
      { ...this.rowAttributes(item), class: "row symbol" },
      el("span", { class: `badge kind-${item.symbolKind}` }, item.symbolKind),
      el("code", { class: "name" }, highlight(item.name, item.hits)),
      el("span", { class: "path muted" }, `${item.path}:${item.line}`),
      el("span", { class: "repo" }, repoName(this.state, item.repoId)),
    );
  }

  private renderCommitRow(item: ItemOf<"commit">): HTMLElement {
    // Each commit is tagged with the OR terms it matched, in their colors (mock 7).
    const terms = textNodes(this.state.parsed);
    const tags = item.matchedTerms.map((termIndex) => {
      const term = terms.find((t) => t.termIndex === termIndex);
      return term ? el("span", { class: `tag ${termClass(termIndex)}` }, term.value) : null;
    });
    const files: HTMLElement[] = item.files
      .slice(0, COMMIT_FILES_SHOWN)
      .map((file) => fileStat(file.path, file.added, file.removed));
    if (item.files.length > COMMIT_FILES_SHOWN)
      files.push(el("span", { class: "muted" }, `+${item.files.length - COMMIT_FILES_SHOWN} more`));
    return el(
      "div",
      { ...this.rowAttributes(item), class: "row commit" },
      el(
        "div",
        { class: "line1" },
        el("span", { class: "subject" }, highlight(item.subject, item.subjectHits)),
        el("code", { class: "sha" }, shortSha(item.sha)),
      ),
      el(
        "div",
        { class: "line2 muted" },
        el("span", {}, item.author.name),
        el("span", {}, timeAgo(item.at)),
        el("span", {}, repoName(this.state, item.repoId)),
        item.diffHits ? el("span", {}, plural(item.diffHits, "hit") + " in diff") : null,
        ...tags,
      ),
      el("div", { class: "line3" }, ...files),
    );
  }
}

function makeSection(title: string, testId: string): Section {
  const count = el("span", { class: "muted" });
  const list = el("div", { class: "list" });
  const root = el(
    "div",
    { class: "rsection", "data-testid": testId, hidden: true },
    el("div", { class: "section-title split" }, el("span", {}, title), count),
    list,
  );
  return { root, list, count };
}
