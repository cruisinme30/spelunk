// The result list: sections for file names, definitions, code and commits,
// appended to as batches stream in, plus hidden-result notes and Load more.
import { el, highlight, plural, timeAgo } from "../format";
import type { Fix, HiddenNote, ResultItem, SearchDoneMsg } from "../protocol.gen";
import { textNodes } from "../queryEdit";
import { repoName, type ViewState } from "../state";

type Kind = ResultItem["kind"];

interface Section {
  root: HTMLElement;
  list: HTMLElement;
  count: HTMLElement;
}

export interface ResultsHandlers {
  onApplyFix(fix: Fix): void;
  onLoadMore(searchId: string, cursor: string): void;
}

/** A DOM id for a result row (refs are opaque, so sanitize them). */
export function rowId(ref: string): string {
  return "r-" + ref.replace(/[^A-Za-z0-9_-]/g, "_");
}

/**
 * Owns the result list and preview containers for one search. Rows are
 * appended per batch rather than re-rendered, so large result sets stream
 * smoothly.
 */
export class ResultsView {
  readonly items: ResultItem[] = [];
  readonly list: HTMLElement;
  readonly preview: HTMLElement;
  private readonly sections: Record<Kind, Section>;
  private readonly notes: HTMLElement;
  private readonly more: HTMLElement;
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
    this.more = el("div", { class: "more" });
    this.list = el(
      "div",
      { id: "results", role: "listbox", "aria-label": "Results", "data-testid": "results" },
      lastGoodText
        ? el(
            "div",
            { class: "lastgood muted" },
            "Showing results for the last query that worked ",
            el("code", {}, lastGoodText),
          )
        : null,
      this.sections.file.root,
      this.sections.symbol.root,
      this.sections.line.root,
      this.sections.commit.root,
      this.notes,
      this.more,
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
    this.updateCounts();
  }

  /** Counts per kind, for the chips summary. */
  counts(): Record<Kind, number> & { codeFiles: number; repos: number } {
    const counts = { file: 0, line: 0, symbol: 0, commit: 0, codeFiles: 0, repos: 0 };
    const codeFiles = new Set<string>();
    const repos = new Set<string>();
    for (const item of this.items) {
      counts[item.kind]++;
      repos.add(item.repoId);
      if (item.kind === "line") codeFiles.add(item.repoId + "\0" + item.path);
    }
    counts.codeFiles = codeFiles.size;
    counts.repos = repos.size;
    return counts;
  }

  /** Renders hidden-result notes, Load more, errors and the empty result. */
  finish(done: SearchDoneMsg): void {
    this.notes.replaceChildren(...done.hidden.filter((note) => note.count > 0).map((note) => this.renderNote(note)));
    this.more.replaceChildren();
    if (done.nextCursor) {
      const cursor = done.nextCursor;
      const button = el("button", { type: "button", class: "btn", "data-testid": "load-more" }, "Load more");
      button.addEventListener("click", () => this.handlers.onLoadMore(done.searchId, cursor));
      this.more.append(button);
    }
    if (done.error) {
      this.preview.replaceChildren(el("div", { class: "notice error" }, done.error));
    } else if (!this.items.length) {
      this.preview.replaceChildren();
      this.list.querySelector(".none")?.remove();
      const hint = done.hidden.some((note) => note.count > 0)
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
    this.updateCounts();
  }

  private renderNote(note: HiddenNote): HTMLElement {
    const unit = note.count === 1 ? note.unit.replace(/(es|s)$/, "") : note.unit;
    const filter = note.undo.title.replace(/^Remove /, "");
    const show = el("button", { type: "button", class: "btn link", "data-testid": "show-hidden" }, "Show them");
    show.addEventListener("click", () => this.handlers.onApplyFix(note.undo));
    return el(
      "div",
      { class: "note", "data-reason": note.reason },
      el("span", {}, `${note.count.toLocaleString("en-US")} ${unit} hidden by `, el("code", {}, filter)),
      show,
    );
  }

  private updateCounts(): void {
    const counts = this.counts();
    this.sections.file.count.textContent = String(counts.file);
    this.sections.symbol.count.textContent = String(counts.symbol);
    this.sections.line.count.textContent = counts.line ? `${counts.line} in ${plural(counts.codeFiles, "file")}` : "";
    this.sections.commit.count.textContent = String(counts.commit);
  }

  private renderItem(item: ResultItem): HTMLElement {
    const attrs = {
      id: rowId(item.ref),
      role: "option",
      "aria-selected": "false",
      "data-ref": item.ref,
      "data-kind": item.kind,
      "data-testid": "result",
    };
    const repo = repoName(this.state, item.repoId);
    switch (item.kind) {
      case "file":
        return el(
          "div",
          { ...attrs, class: "row file", title: item.path },
          el("span", { class: "path" }, highlight(item.path, item.nameHits)),
          item.dirty ? el("span", { class: "badge warn" }, "Uncommitted changes") : null,
          item.lastCommit
            ? el("span", { class: "meta" }, `${item.lastCommit.author} · ${timeAgo(item.lastCommit.at)}`)
            : null,
          el("span", { class: "repo" }, repo),
        );
      case "line": {
        // Consecutive lines from one file share a group header.
        const group = el("div", { class: "contents" });
        const key = `${item.repoId}\0${item.path}`;
        if (this.codeGroup?.key !== key) {
          const count = el("span", { class: "count" });
          group.append(
            el(
              "div",
              { class: "group", "data-testid": "code-group" },
              el("span", { class: "path" }, item.path),
              el("span", { class: "repo" }, repo),
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
            { ...attrs, class: "row line" },
            el("span", { class: "ln" }, String(item.line)),
            el("code", { class: "text" }, highlight(item.text, item.hits)),
          ),
        );
        return group;
      }
      case "symbol":
        return el(
          "div",
          { ...attrs, class: "row symbol" },
          el("span", { class: `badge kind-${item.symbolKind}` }, item.symbolKind),
          el("code", { class: "name" }, highlight(item.name, item.hits)),
          el("span", { class: "path muted" }, `${item.path}:${item.line}`),
          el("span", { class: "repo" }, repo),
        );
      case "commit": {
        // Tag each commit with the OR terms it matched, in their colors (mock 7).
        const terms = textNodes(this.state.parsed);
        const tags = item.matchedTerms.map((index) => {
          const term = terms.find((t) => t.termIndex === index);
          return term ? el("span", { class: `tag t${index % 4}` }, term.value) : null;
        });
        const files = item.files.slice(0, 2).map((f) => fileStat(f.path, f.added, f.removed));
        if (item.files.length > 2) files.push(el("span", { class: "muted" }, `+${item.files.length - 2} more`));
        return el(
          "div",
          { ...attrs, class: "row commit" },
          el(
            "div",
            { class: "line1" },
            el("span", { class: "subject" }, highlight(item.subject, item.subjectHits)),
            el("code", { class: "sha" }, item.sha.slice(0, 7)),
          ),
          el(
            "div",
            { class: "line2 muted" },
            el("span", {}, item.author.name),
            el("span", {}, timeAgo(item.at)),
            el("span", {}, repo),
            item.diffHits ? el("span", {}, plural(item.diffHits, "hit") + " in diff") : null,
            ...tags,
          ),
          el("div", { class: "line3" }, ...files),
        );
      }
    }
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

export function fileStat(path: string, added: number, removed: number): HTMLElement {
  return el(
    "span",
    { class: "file" },
    path,
    " ",
    el("span", { class: "add" }, `+${added}`),
    " ",
    el("span", { class: "del" }, `−${removed}`),
  );
}
