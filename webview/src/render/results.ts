// The result list: sections for file names, definitions, code and commits,
// appended to as batches stream in, plus hidden-result notes and Load more.
import {
  button,
  codeLineRow,
  codeLineText,
  codeList,
  element,
  fileStat,
  highlight,
  indentOf,
  lastLineOf,
  plural,
  redrawCodeLineText,
  shortSha,
  termClass,
  timeAgo,
} from "../format";
import { diffSide, operatorNodes, textNodes, textTerms } from "../parsedQuery";
import type {
  Fix,
  HiddenNote,
  RankReason,
  ResultItem,
  ResultOrder,
  SearchDoneMessage,
  SinceRevision,
} from "../protocol.gen";
import { menuContext, repoName, type ViewState } from "../state";
import { ChunkedList } from "./chunkedList";
import { ResultIndex } from "./resultIndex";
import { resultsSash } from "./sash";

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

/** The choices of the Context buttons: how many lines to show around each code match. */
const CONTEXT_CHOICES = [0, 1, 2, 3] as const;

/** How a section title says the order of current files. */
const ORDER_LABEL: Record<ResultOrder, string> = { best: "best match first", path: "path order" };

/** The badge a result's file shows for why best-match order put it where it is. */
const RANK_BADGE: Record<RankReason, string> = {
  definition: "definition",
  open: "open",
  test: "test",
  vendored: "vendored",
  generated: "generated",
};

/** What undoing a filter does, where "Show them" would mislead. */
const UNDO_LABEL: Partial<Record<HiddenNote["reason"], string>> = {
  // Undoing case:yes or word:yes changes how text matches rather than showing hidden rows.
  case: "Ignore case",
  word: "Match parts of words",
  type: "Show code too",
  kind: "Show every kind",
};

/** A result as appended to its section: `node` holds its `row`, after a file header for a code line. */
interface Rendered {
  node: HTMLElement;
  row: HTMLElement;
}

/** A result whose row stands alone. */
const asRendered = (row: HTMLElement): Rendered => ({ node: row, row });

/** One section of the list (file names, definitions, code or commits). */
interface Section {
  root: HTMLElement;
  list: HTMLElement;
  /** The list's rows, in chunks the browser lays out only near the view. */
  chunks: ChunkedList;
  count: HTMLElement;
}

/** What the result list's buttons do. */
interface ResultsHandlers {
  onApplyFix(fix: Fix): void;
  onLoadMore(searchId: string, cursor: string): void;
  /** A Context button was pressed: show `lines` lines around each code match from now on. */
  onContextLines(lines: number): void;
  /** What a replace would make of a code line's matches, while the Replace row previews one. */
  replacementsFor(item: ItemOf<"line">): string[] | undefined;
  /** Colors the code a code result's `node` shows: its lines and the context lines around them. */
  paintCode(node: ParentNode, item: ItemOf<"line">): void;
}

/** How many results of each kind are on screen, plus the files and repos they come from. */
interface ResultCounts extends Record<ResultKind, number> {
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
  /** The id of the header's path, which names the file to screen readers on each of its lines. */
  pathId: string;
  /** The header's count, updated as the file's lines arrive. */
  countLabel: HTMLElement;
  lineCount: number;
  /** The run of lines shown last, when results carry context lines. */
  block?: CodeBlock;
}

/**
 * A run of consecutive lines of one file: matches and the context lines
 * around them. The next match in the file joins it when their lines touch
 * or overlap, so no line is shown twice.
 */
interface CodeBlock {
  /** The last line of its last match. */
  matchLine: number;
  /** The context lines shown after that match, which the next match may take over. */
  trailing: { line: number; row: HTMLElement }[];
  /** How much indentation every line of the run drops, so they keep their places relative to each other. */
  indent: number;
  /** Draw each line of the run again dropping less indentation, when a match joins it with less. */
  redraws: ((indent: number) => void)[];
}

/**
 * The result list and preview containers for one search. Rows are appended
 * per batch rather than re-rendered, in chunks laid out only near the view,
 * and found through an index, so large result sets stream and step smoothly.
 */
export class ResultsView {
  readonly list: HTMLElement;
  readonly preview: HTMLElement;
  private readonly sections: Record<ResultKind, Section>;
  private readonly notes: HTMLElement;
  private readonly loadMore: HTMLElement;
  private readonly lastGood: HTMLElement;
  /** Every result on screen, indexed: by ref, by identity, and in the order ↑↓ go through them. */
  readonly index = new ResultIndex();
  /** Results per kind so far; append() keeps it current, so counting never rescans `items`. */
  private readonly countByKind: Record<ResultKind, number> = { file: 0, line: 0, symbol: 0, commit: 0 };
  /** The files (fileKey) the code lines so far come from. */
  private readonly codeFileKeys = new Set<string>();
  /** The repos the results so far come from. */
  private readonly repoIds = new Set<string>();
  /** Set while code lines of one file arrive in a row, so they share its header. */
  private openCodeFile: OpenCodeFile | undefined;
  /** Whether code results carry context lines (the contextLines setting is above 0). */
  private readonly withContext: boolean;
  /** How much indentation each code result drops, when it sits among context lines. */
  private readonly indentByRef = new Map<string, number>();

  /** Replaces `body` with an empty list and preview; `scope` is the query's path patterns (pathScope). */
  constructor(
    body: HTMLElement,
    private readonly state: ViewState,
    private readonly handlers: ResultsHandlers,
    scope: string[] = [],
  ) {
    const order = ORDER_LABEL[state.parsed?.globals.order ?? state.ui.order];
    this.sections = {
      file: makeSection(`File names · ${order}`, "section-files"),
      symbol: makeSection(`Definitions · ${order}`, "section-symbols"),
      line: makeSection(
        `${scope.length > 0 ? "Code in matching paths" : "Code"} · ${order}`,
        "section-code",
        contextSwitch(state.ui.contextLines, (lines) => {
          this.handlers.onContextLines(lines);
        }),
      ),
      commit: makeSection("Commits · newest first", "section-commits"),
    };
    if (scope.length > 0) this.sections.line.list.before(pathScopeNote(scope));
    this.notes = element("div", { class: "notes", "data-testid": "hidden-notes" });
    this.loadMore = element("div", { class: "more" });
    this.lastGood = element("div", { class: "last-good muted", "data-testid": "last-good", hidden: true });
    this.list = element(
      "div",
      { id: "results", "data-testid": "results" },
      this.lastGood,
      this.sections.file.root,
      this.sections.symbol.root,
      this.sections.line.root,
      this.sections.commit.root,
      this.notes,
      this.loadMore,
    );
    this.withContext = state.ui.contextLines > 0;
    this.list.classList.toggle("with-context", this.withContext);
    // Not a live region: it would read the whole excerpt again on every ↑↓; the selected row is announced instead.
    this.preview = element("div", { id: "preview", "data-testid": "preview" });
    body.replaceChildren(element("div", { class: "split" }, this.list, resultsSash(this.list), this.preview));
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

  /** Every result appended so far, in arrival order. */
  get items(): readonly ResultItem[] {
    return this.index.items;
  }

  /** The result `ref` names, if it is on screen. */
  itemFor(ref: string): ResultItem | undefined {
    return this.index.placed(ref)?.item;
  }

  /** The row of the result `ref` names, if it is on screen. */
  rowFor(ref: string): HTMLElement | undefined {
    return this.index.placed(ref)?.row;
  }

  /** The first `count` code results, for the glimpse under the suggestions. */
  firstLines(count: number): ItemOf<"line">[] {
    return this.index.firstLines(count);
  }

  /** Adds a batch of results to their sections, skipping any of a kind this webview doesn't know. */
  append(items: ResultItem[]): void {
    for (const item of items) {
      if (!Object.hasOwn(this.sections, item.kind)) continue;
      let rendered: Rendered;
      try {
        rendered = this.renderItem(item);
        if (item.kind === "line") this.handlers.paintCode(rendered.node, item);
      } catch {
        continue; // a malformed result is left out, not the rest of its batch
      }
      this.countByKind[item.kind]++;
      this.repoIds.add(item.repoId);
      if (item.kind === "line") this.codeFileKeys.add(fileKey(item));
      const section = this.sections[item.kind];
      section.root.hidden = false;
      this.index.add(item, rendered.row, section.chunks.append(rendered.node));
    }
    this.updateSectionCounts();
  }

  /**
   * Redraws the code lines on the lines `keys` (lineKey) name, whose
   * replacements the Replace row's preview changed: replaced, or marked
   * again. A row out of sight is redrawn once it shows.
   */
  showReplacements(keys: Iterable<string>): void {
    for (const key of new Set(keys)) {
      for (const item of this.index.linesAt(key)) {
        // A match that spans lines is never replaced, so it stays marked.
        const placed = this.index.placed(item.ref);
        if ((item.spanLines?.length ?? 0) > 0 || !placed) continue;
        this.sections.line.chunks.redraw(placed.chunk, item.ref, () => {
          placed.row
            .querySelector(".text")
            ?.replaceChildren(codeLineText(item, this.handlers.replacementsFor(item), this.indentByRef.get(item.ref)));
        });
      }
    }
  }

  /** Tags the commits on screen that a watch found since their query last ran (state.newCommits) as New. */
  markNew(): void {
    for (const sha of this.state.newCommits) {
      for (const row of this.index.commitRows(sha)) {
        const title = row.querySelector(".commit-title");
        if (title && !title.querySelector(".new-commit")) title.querySelector(".sha")?.before(newTag());
      }
    }
  }

  /** Greys out a row whose file or commit no longer exists. */
  markStale(ref: string): void {
    this.rowFor(ref)?.classList.add("stale");
  }

  /** How many results are on screen, by kind, file and repo. */
  counts(): ResultCounts {
    return { ...this.countByKind, codeFiles: this.codeFileKeys.size, repos: this.repoIds.size };
  }

  /** Renders hidden-result notes, Load more, an error, or the empty result. */
  finish(done: SearchDoneMessage): void {
    this.notes.replaceChildren(
      ...revisionProblems(done).map((problem) =>
        element("div", { class: "note", "data-reason": "revision", "data-testid": "revision-problem" }, problem),
      ),
      ...done.hidden.filter((note) => note.count > 0).map((note) => this.renderNote(note)),
    );
    this.loadMore.replaceChildren();
    // The daemon sends a cursor when there are more pages; Load more asks for the next one.
    if (done.nextCursor) {
      const cursor = done.nextCursor;
      const more = button(
        { class: "btn", "data-testid": "load-more" },
        () => {
          // Once: a second click would fetch the same page again, and show its results twice.
          more.disabled = true;
          this.handlers.onLoadMore(done.searchId, cursor);
        },
        "Load more",
      );
      this.loadMore.append(more);
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
    const caseNote = notes.find((note) => note.reason === "case");
    if (caseNote?.filter === "case:smart") hint = "Smart case matches case when the query has a capital letter.";
    else if (caseNote) hint = "case:yes is on, so capital letters have to match.";
    else if (notes.some((note) => note.reason === "word")) hint = "word:yes is on, so only whole words match.";
    else if (notes.length > 0) hint = "Some results are hidden by filters below.";
    else if (revisionProblems(this.state.done).length > 0) hint = "Some repos don't have that revision; see below.";
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
   * "3 commits hidden by -f:vendor/ · Show them", "38 code matches for
   * retry hidden by type:file · Show code too", or "1 commit for retry
   * hidden by type:added · Search every changed line".
   */
  private renderNote(note: HiddenNote): HTMLElement {
    if (note.reason === "symbol") return this.renderTextSearchNote(note);
    // type:added and type:removed hide commits; their undo says what it searches instead.
    const label = note.reason === "type" && note.unit === "commits" ? note.undo.title : UNDO_LABEL[note.reason];
    const showThem = this.showHiddenButton(note, label ?? "Show them");
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
    const one = SINGULAR_UNIT[note.unit];
    if (note.reason !== "type") return plural(note.count, one, note.unit);
    const terms = textTerms(this.state.parsed);
    const what =
      note.unit === "commits"
        ? plural(note.count, one, note.unit)
        : plural(note.count, `code ${one}`, `code ${note.unit}`);
    return what + (terms.length > 0 ? ` for ${terms.join(" and ")}` : "");
  }

  private updateSectionCounts(): void {
    const counts = this.counts();
    this.sections.file.count.textContent = String(counts.file);
    this.sections.symbol.count.textContent = String(counts.symbol);
    this.sections.line.count.textContent =
      counts.line > 0 ? `${counts.line} in ${plural(counts.codeFiles, "file")}` : "";
    this.sections.commit.count.textContent = String(counts.commit);
  }

  private renderItem(item: ResultItem): Rendered {
    switch (item.kind) {
      case "file": {
        return asRendered(this.renderFileRow(item));
      }
      case "line": {
        return this.renderCodeRow(item);
      }
      case "symbol": {
        return asRendered(this.renderSymbolRow(item));
      }
      case "commit": {
        return asRendered(this.renderCommitRow(item));
      }
    }
  }

  /** Attributes every result row shares: id, ARIA option role, the opaque ref, and what VS Code's menu reads. */
  private rowAttributes(item: ResultItem): Record<string, string> {
    return {
      id: rowId(item.ref),
      role: "option",
      "aria-selected": "false",
      "data-ref": item.ref,
      "data-kind": item.kind,
      "data-testid": "result",
      "data-vscode-context": menuContext(this.state, item),
    };
  }

  /**
   * A code line's attributes: a screen reader hears its file's path, which
   * the header above shows once for all its lines, then the line's own text
   * ("src/client.py 42 self.retry_policy = …").
   */
  private codeRowAttributes(item: ItemOf<"line">, file: OpenCodeFile): Record<string, string> {
    return { ...this.rowAttributes(item), "aria-labelledby": `${file.pathId} ${rowId(item.ref)}` };
  }

  private renderFileRow(item: ItemOf<"file">): HTMLElement {
    return element(
      "div",
      { ...this.rowAttributes(item), class: `row file${demoted(item.rankReason)}`, title: item.path },
      element("span", { class: "path" }, highlight(item.path, item.nameHits)),
      // Together after the path, on a line of their own when they don't fit beside it.
      element(
        "span",
        { class: "details" },
        rankBadge(item.rankReason),
        sinceBadge(item.sinceRevision),
        ownerBadge(item.owners),
        item.dirty ? element("span", { class: "badge warn" }, "Uncommitted changes") : null,
        item.lastCommit
          ? element("span", { class: "meta" }, `${item.lastCommit.author} · ${timeAgo(item.lastCommit.at)}`)
          : null,
        element("span", { class: "repo" }, repoName(this.state, item.repoId)),
      ),
    );
  }

  /** A code line, preceded by a file header when it starts a new file. */
  private renderCodeRow(item: ItemOf<"line">): Rendered {
    const group = element("div", { class: "contents" });
    const key = fileKey(item);
    if (this.openCodeFile?.key !== key) {
      const countLabel = element("span", { class: "count" });
      const pathId = `${rowId(item.ref)}-file`;
      group.append(
        element(
          "div",
          { class: `group${demoted(item.rankReason)}`, "data-testid": "code-group" },
          element("span", { class: "path", id: pathId }, item.path),
          rankBadge(item.rankReason),
          sinceBadge(item.sinceRevision),
          ownerBadge(item.owners),
          element("span", { class: "repo" }, repoName(this.state, item.repoId)),
          countLabel,
        ),
      );
      this.openCodeFile = { key, pathId, countLabel, lineCount: 0 };
    }
    this.openCodeFile.lineCount++;
    this.openCodeFile.countLabel.textContent = String(this.openCodeFile.lineCount);
    if (this.withContext) return { node: group, row: this.appendWithContext(group, item, this.openCodeFile) };
    const row = codeLineRow(item, this.codeRowAttributes(item, this.openCodeFile), this.handlers.replacementsFor(item));
    group.append(row);
    return { node: group, row };
  }

  /**
   * Appends a code result with the dimmed lines around it, and returns its row. When the file's
   * last run of lines reaches this match's context, the two join: lines
   * already shown aren't repeated, and context the last match showed past
   * this one makes way for it. Otherwise "⋯" marks the lines skipped. A
   * match that spans lines has its context after its last line. A run drops
   * the indentation all its lines share, so a match joining it with less
   * redraws the lines before it.
   */
  private appendWithContext(group: HTMLElement, item: ItemOf<"line">, file: OpenCodeFile): HTMLElement {
    const before = item.contextBefore ?? [];
    const after = item.contextAfter ?? [];
    const lastLine = lastLineOf(item);
    const firstLine = item.line - before.length;
    const joined = runToJoin(file.block, item.line, firstLine);
    if (file.block && !joined) group.append(contextGap());
    const ownIndent = sharedIndent(item, before, after);
    const block = joined?.block ?? { matchLine: item.line, trailing: [], indent: ownIndent, redraws: [] };
    const lastShown = joined?.lastShown ?? 0;
    if (ownIndent < block.indent) {
      block.indent = ownIndent;
      for (const redraw of block.redraws) redraw(ownIndent);
    }
    const { indent, redraws } = block;
    const appendContext = (line: number, text: string) => {
      const row = contextRow(line, text, indent);
      group.append(row);
      redraws.push((less) => row.querySelector(".text")?.replaceChildren(withoutIndent(text, less)));
      return row;
    };
    for (const [index, text] of before.entries()) {
      const line = firstLine + index;
      if (line > lastShown) appendContext(line, text);
    }
    this.indentByRef.set(item.ref, indent);
    const row = codeLineRow(item, this.codeRowAttributes(item, file), this.handlers.replacementsFor(item), indent);
    group.append(row);
    redraws.push((less) => {
      this.indentByRef.set(item.ref, less);
      redrawCodeLineText(row, item, this.handlers.replacementsFor(item), less);
    });
    block.matchLine = lastLine;
    block.trailing = after.map((text, index) => ({
      line: lastLine + 1 + index,
      row: appendContext(lastLine + 1 + index, text),
    }));
    file.block = block;
    return row;
  }

  private renderSymbolRow(item: ItemOf<"symbol">): HTMLElement {
    return element(
      "div",
      { ...this.rowAttributes(item), class: `row symbol${demoted(item.rankReason)}` },
      element("span", { class: `badge kind-${item.symbolKind}` }, item.symbolKind),
      element("code", { class: "name" }, highlight(item.name, item.hits)),
      element("span", { class: "path muted" }, `${item.path}:${item.line}`),
      sinceBadge(item.sinceRevision),
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
        this.state.newCommits.has(item.sha) ? newTag() : null,
        element("code", { class: "sha" }, shortSha(item.sha)),
      ),
      element(
        "div",
        { class: "commit-meta muted" },
        element("span", {}, item.author.name),
        element("span", {}, timeAgo(item.at)),
        element("span", {}, repoName(this.state, item.repoId)),
        commitMatchPlace(item, diffSide(this.state.parsed)),
        ...tags,
      ),
      item.bodyLine
        ? element("div", { class: "commit-body-line" }, highlight(item.bodyLine.text, item.bodyLine.hits))
        : null,
      element("div", { class: "commit-files" }, ...files),
    );
  }
}

/** "New": a commit a watch found since its query last ran. */
function newTag(): HTMLElement {
  return element(
    "span",
    { class: "badge new-commit", title: "New since you last ran this watched query", "data-testid": "new-commit" },
    "New",
  );
}

/**
 * Where a commit's text terms matched: "in message · 3 hits in diff", "in message only" or "3 hits in diff".
 * Under type:added or type:removed only that side of the diff is searched: "3 hits in added lines".
 */
function commitMatchPlace(item: ItemOf<"commit">, side: "added" | "removed" | null): HTMLElement | null {
  const place = side ? `in ${side} lines` : "in diff";
  const inDiff = item.diffHits ? `${plural(item.diffHits, "hit")} ${place}` : "";
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

/** Why each repo an at: search left out has no such revision: "v9 isn't a tag, branch or commit in web-checkout". */
function revisionProblems(done: SearchDoneMessage | undefined): string[] {
  return (done?.revisions ?? []).flatMap((revision) => (revision.problem ? [revision.problem] : []));
}

/** How a file found at a revision (at:) differs now: "deleted since" or "changed since"; nothing when it's the same. */
const SINCE_BADGE: Record<SinceRevision, { label: string; title: string }> = {
  deleted: { label: "deleted since", title: "This file isn't in the current files" },
  changed: { label: "changed since", title: "This file is different in the current files" },
};

/** The badge for how a file at a revision differs now; none for a value this webview doesn't know. */
function sinceBadge(since: SinceRevision | undefined): HTMLElement | null {
  if (since === undefined || !Object.hasOwn(SINCE_BADGE, since)) return null;
  const { label, title } = SINCE_BADGE[since];
  return element("span", { class: `badge since-${since}`, title, "data-testid": "since-revision" }, label);
}

/** The badge for why best-match order put a result's file where it is, if it says. */
function rankBadge(reason: RankReason | undefined): HTMLElement | null {
  return reason
    ? element("span", { class: `badge rank-${reason}`, "data-testid": "rank-reason" }, RANK_BADGE[reason])
    : null;
}

/**
 * The badge naming who owns a result's file by its repo's CODEOWNERS file:
 * the first owner, a team by its name alone (payments-core for the team
 * `@acme/payments-core`), then "+1" for each other one, all of them in
 * full in its tooltip.
 */
function ownerBadge(owners: string[] | undefined): HTMLElement | null {
  // Only names count: anything else in a broken message is left out.
  const names = Array.isArray(owners)
    ? (owners as unknown[]).filter((owner): owner is string => typeof owner === "string")
    : [];
  const [first, ...others] = names;
  if (first === undefined) return null;
  const name = first.startsWith("@") && first.includes("/") ? first.slice(first.indexOf("/") + 1) : first;
  const label = others.length > 0 ? `${name} +${others.length}` : name;
  return element(
    "span",
    { class: "badge owner", title: `Owned by ${names.join(", ")}`, "data-testid": "owner" },
    label,
  );
}

/** The indentation a code result's lines and the context around them share; blank lines don't count. */
function sharedIndent(item: ItemOf<"line">, before: string[], after: string[]): number {
  const spanned = (item.spanLines ?? []).map((line) => line.text);
  const lines = [...before, item.text, ...spanned, ...after].filter((text) => text.trim() !== "");
  return Math.min(...lines.map((text) => indentOf(text)));
}

/**
 * A dimmed line around a code result, without the first `indent`
 * characters of its indentation. It isn't a result: the arrow keys skip it,
 * and screen readers read the match on its own.
 */
function contextRow(line: number, text: string, indent: number): HTMLElement {
  return element(
    "div",
    { class: "context-line", "aria-hidden": "true", "data-testid": "context-line", "data-line": String(line) },
    element("span", { class: "line-number" }, String(line)),
    element("code", { class: "text" }, withoutIndent(text, indent)),
  );
}

/**
 * The run a match from `matchLine` with context from `firstLine` joins: the
 * file's last run, when it reaches that far. Context the last run showed
 * from the match's line on makes way for the match, joined or not.
 * `lastShown` is the last line the run still shows.
 */
function runToJoin(
  last: CodeBlock | undefined,
  matchLine: number,
  firstLine: number,
): { block: CodeBlock; lastShown: number } | undefined {
  if (!last || matchLine <= last.matchLine) return undefined;
  for (const shown of last.trailing.filter((trailing) => trailing.line >= matchLine)) shown.row.remove();
  last.trailing = last.trailing.filter((trailing) => trailing.line < matchLine);
  const lastShown = last.trailing.at(-1)?.line ?? last.matchLine;
  return firstLine > lastShown + 1 ? undefined : { block: last, lastShown };
}

/** A context line without at most `indent` characters of its indentation. */
function withoutIndent(text: string, indent: number): string {
  return text.slice(Math.min(indentOf(text), indent));
}

/**
 * "Context 0 ±1 ±2 ±3" in the Code title: how many lines each match shows
 * around it (spelunk.results.contextLines). A press shows at once; the host
 * saves the setting, which runs the query again.
 */
function contextSwitch(current: number, onChange: (lines: number) => void): HTMLElement {
  const group = element(
    "div",
    { class: "context-switch", role: "group", "aria-label": "Context lines", "data-testid": "context-switch" },
    element("span", { class: "label" }, "Context"),
  );
  for (const lines of CONTEXT_CHOICES) {
    const title = lines === 0 ? "Only the matching lines" : `${plural(lines, "line")} above and below each match`;
    const option = button(
      { class: "toggle", "aria-pressed": String(lines === current), title, "data-lines": String(lines) },
      () => {
        for (const other of group.querySelectorAll("button"))
          other.setAttribute("aria-pressed", String(other === option));
        onChange(lines);
      },
      lines === 0 ? "0" : `±${lines}`,
    );
    group.append(option);
  }
  return group;
}

/** "⋯" between two runs of lines of one file, for the lines skipped. */
function contextGap(): HTMLElement {
  return element(
    "div",
    { class: "context-gap", "aria-hidden": "true", "data-testid": "context-gap" },
    element("span", { class: "line-number" }, "⋯"),
  );
}

/** " demoted" for a test, vendored or generated file, which reads dimmer; "" otherwise. */
function demoted(reason: RankReason | undefined): string {
  return reason === "test" || reason === "vendored" || reason === "generated" ? " demoted" : "";
}

/** A section of the list; `controls` go in its title, before the count. */
/** The ids of the sections' listboxes, which the query box controls while no suggestions show. */
export const RESULT_LISTS = ["section-files", "section-symbols", "section-code", "section-commits"]
  .map((testId) => `${testId}-list`)
  .join(" ");

function makeSection(title: string, testId: string, controls?: HTMLElement): Section {
  const count = element("span", { class: "muted" });
  // A listbox of the section's rows alone: its title's buttons, and the notes and Load more below, stay outside it.
  const list = element("div", { class: "list", id: `${testId}-list`, role: "listbox", "aria-label": title });
  const root = element(
    "div",
    { class: "result-section", "data-testid": testId, hidden: true },
    element(
      "div",
      { class: "section-title spread" },
      element("span", {}, title),
      // Together, so a narrow panel wraps them onto a line of their own.
      controls ? element("span", { class: "title-end" }, controls, count) : count,
    ),
    list,
  );
  return { root, list, chunks: new ChunkedList(list), count };
}
