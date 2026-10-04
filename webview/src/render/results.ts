// The result list: sections for file names, definitions, code and commits,
// appended to as batches stream in, plus hidden-result notes and Load more.
import { el, fileStat, highlight, plural, shortSha, termClass, timeAgo } from "../format";
import type { Fix, HiddenNote, ResultItem, SearchDoneMsg } from "../protocol.gen";
import { textNodes } from "../queryEdit";
import { repoName, type ViewState } from "../state";

type ResultKind = ResultItem["kind"];
type ItemOf<K extends ResultKind> = Extract<ResultItem, { kind: K }>;

/** Changed files listed under a commit before "+N more". */
const COMMIT_FILES_SHOWN = 2;

const SINGULAR_UNIT: Record<HiddenNote["unit"], string> = { matches: "match", files: "file", commits: "commit" };

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
  private codeGroup?: { key: string; count: HTMLElement; matches: number };

  constructor(
    body: HTMLElement,
    private readonly state: ViewState,
    private readonly handlers: ResultsHandlers,
    lastGoodText?: string,
  ) {
    this.sections = {
      file: makeSection("File names", "section-files"),
      symbol: makeSection("Definitions", "section-symbols"),
      line: makeSection("Code", "section-code"),
      commit: makeSection("Commits · newest first", "section-commits"),
    };
    this.notes = el("div", { class: "notes", "data-testid": "hidden-notes" });
    this.loadMore = el("div", { class: "more" });
    const lastGood = lastGoodText
      ? el(
          "div",
          { class: "lastgood muted" },
          "Showing results for the last query that worked ",
          el("code", {}, lastGoodText),
        )
      : null;
    this.list = el(
      "div",
      { id: "results", role: "listbox", "aria-label": "Results", "data-testid": "results" },
      lastGood,
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

  /** "3 commits hidden by -f:vendor/ · Show them" (mock 7). */
  private renderNote(note: HiddenNote): HTMLElement {
    const unit = note.count === 1 ? SINGULAR_UNIT[note.unit] : note.unit;
    const showThem = el("button", { type: "button", class: "btn link", "data-testid": "show-hidden" }, "Show them");
    showThem.addEventListener("click", () => this.handlers.onApplyFix(note.undo));
    return el(
      "div",
      { class: "note", "data-reason": note.reason },
      el("span", {}, `${note.count.toLocaleString("en-US")} ${unit} hidden by `, el("code", {}, note.filter)),
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
    group.append(
      el(
        "div",
        { ...this.rowAttributes(item), class: "row line" },
        el("span", { class: "ln" }, String(item.line)),
        el("code", { class: "text" }, highlight(item.text, item.hits)),
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
