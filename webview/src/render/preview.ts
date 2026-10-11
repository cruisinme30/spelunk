// The preview pane: a file excerpt around the match, or a commit's diff.
import { button, element, fileStat, highlight, plural, scrollIntoContainer, shortSha, timeAgo } from "../format";
import { saveOutlineOpen } from "../host";
import { textTerms } from "../parsedQuery";
import type {
  Hunk,
  NotebookCell,
  OpenPosition,
  OpenWhere,
  OutlineSymbol,
  Ownership,
  Preview,
  ResultItem,
  SinceRevision,
  SymbolKind,
} from "../protocol.gen";
import { repoName, type ViewState } from "../state";
import { markWhitespace } from "../syntax";
import { cellHeading, cellHeadingAt, cellOfLine, lineInCell, outputsNoteAt, shownLine } from "./notebook";
import { outlineSash } from "./sash";

type FilePreview = Extract<Preview, { kind: "file" }>;
type CommitPreview = Extract<Preview, { kind: "commit" }>;

/** The letter the Outline marks each kind of definition with, as VS Code's Outline marks them with an icon. */
const OUTLINE_ICON: Record<SymbolKind, string> = {
  class: "C",
  interface: "I",
  type: "T",
  function: "ƒ",
  method: "m",
  other: "◇",
};
const DIFF_SIGN = { add: "+", del: "−", ctx: " " } as const;

/** What the preview's buttons do. */
interface PreviewHandlers {
  /** Opens the result, at `position` instead of its match when given one. */
  onOpen: (ref: string, where: OpenWhere, position?: OpenPosition) => void;
  onShowHiddenFiles: () => void;
  /** What a replace would make of a line's matches, while the Replace row previews one. */
  replacementsFor: (path: string, line: number) => string[] | undefined;
  /** Colors `code`, which shows `text` of `path`, in the theme's syntax colors. */
  paint: (code: HTMLElement, path: string, text: string) => void;
}

/** The selected result a preview shows: its repo, and, for an at: search, its revision. */
export interface PreviewOf {
  repoId: string | undefined;
  /** The at: value its file is at, and how the file differs now; undefined for a current file. */
  at?: { label: string; since: SinceRevision | undefined };
}

/** What the preview of `item` shows it as: in its repo, and at the revision of the search on screen, if any. */
export function previewOf(state: ViewState, item: ResultItem | undefined): PreviewOf {
  const at = state.searchedAt;
  if (at === undefined || !item) return { repoId: item?.repoId };
  return { repoId: item.repoId, at: { label: at, since: "sinceRevision" in item ? item.sinceRevision : undefined } };
}

/** Fills `container` with the preview in `state.preview`, if any. */
export function renderPreview(
  container: HTMLElement,
  state: ViewState,
  of: PreviewOf,
  handlers: PreviewHandlers,
): void {
  container.replaceChildren();
  const current = state.preview;
  if (!current) return;
  if (!current.preview) {
    container.append(element("div", { class: "notice" }, current.stale ? "No longer exists" : "No preview available"));
    return;
  }
  const preview = current.preview;
  const actions = element(
    "div",
    { class: "actions" },
    element(
      "button",
      { type: "button", class: "btn", "data-where": "current" },
      preview.kind === "commit" ? "Open diff ↵" : "Open ↵",
    ),
    element("button", { type: "button", class: "btn", "data-where": "side" }, "Open to side ⌘↵"),
    of.at ? compareButton(of.at.since) : null,
  );
  // One listener for every button: each says where it opens in data-where.
  actions.addEventListener("click", (event) => {
    const where = (event.target as HTMLElement).closest<HTMLButtonElement>("[data-where]:not(:disabled)")?.dataset[
      "where"
    ];
    if (where === "current" || where === "side" || where === "compare") handlers.onOpen(current.ref, where);
  });
  const header = { repo: of.repoId ? repoName(state, of.repoId) : "", actions, at: of.at?.label };
  if (preview.kind === "file") {
    renderFilePreview(container, preview, header, {
      outlineOpen: state.outlineOpen,
      onToggleOutline: (open) => {
        state.outlineOpen = open;
        saveOutlineOpen(open);
      },
      replacementsFor: handlers.replacementsFor,
      decorate: (code, text, focus) => {
        markWhitespace(code, text, state.ui.renderWhitespace, focus);
        handlers.paint(code, preview.path, text);
      },
    });
    // A double-click on the code opens the file beside the panel, at the word it selected.
    container.querySelector(".code")?.addEventListener("dblclick", (event) => {
      const position = doubleClicked(event as MouseEvent);
      if (position) handlers.onOpen(current.ref, "side", position);
    });
    // A click on a definition in the Outline opens the file beside the panel, at its line.
    container.querySelector(".outline-pane")?.addEventListener("click", (event) => {
      const line = Number((event.target as Element).closest<HTMLElement>("[data-line]")?.dataset["line"]);
      if (Number.isSafeInteger(line)) handlers.onOpen(current.ref, "side", { line, column: 1, length: 0 });
    });
  } else {
    renderCommitPreview(container, preview, header, {
      showHiddenFiles: state.showHiddenFiles,
      onShowHiddenFiles: handlers.onShowHiddenFiles,
      hasTextTerms: textTerms(state.parsed).length > 0,
      paint: handlers.paint,
    });
  }
}

/** What every preview header shows besides the title: the repo name, the Open buttons, and an at: search's revision. */
interface PreviewHeader {
  repo: string;
  actions: HTMLElement;
  at?: string | undefined;
}

/**
 * Compare, for a file at a revision: a diff of the file then against the file now. A file deleted since, or the
 * same now, has nothing to compare with.
 */
function compareButton(since: SinceRevision | undefined): HTMLElement {
  const reason = {
    deleted: "Deleted since: there's no current file to compare with",
    none: "The same as the current file",
  }[since === "deleted" ? "deleted" : "none"];
  return element(
    "button",
    {
      type: "button",
      class: "btn",
      "data-where": "compare",
      "data-testid": "compare",
      ...(since === "changed" ? { title: "Show what changed since, as a diff" } : { disabled: true, title: reason }),
    },
    "Compare with now",
  );
}

/** How a file preview draws its lines: as a replace would change them, then decorated (colored, whitespace marked). */
interface FileLineDrawing {
  /** Whether the Outline starts open, and what keeps it so when it's toggled. */
  outlineOpen: boolean;
  onToggleOutline: (open: boolean) => void;
  replacementsFor: PreviewHandlers["replacementsFor"];
  decorate: (code: HTMLElement, text: string, focus: boolean) => void;
}

function renderFilePreview(
  container: HTMLElement,
  preview: FilePreview,
  { repo, actions, at }: PreviewHeader,
  { outlineOpen, onToggleOutline, replacementsFor, decorate }: FileLineDrawing,
): void {
  const symbols = preview.symbols ?? [];
  const cells = preview.cells ?? [];
  const outline = symbols.length > 0 ? outlinePane(symbols, preview.focusLine, outlineOpen, cells) : undefined;
  const toggle = outline ? outlineToggle(outline, symbols.length, onToggleOutline) : null;
  container.append(
    element(
      "div",
      { class: "preview-header" },
      element(
        "span",
        { class: "preview-title" },
        element("span", { class: "muted" }, repo ? `${repo} / ` : ""),
        preview.path,
      ),
      at === undefined
        ? null
        : element("span", { class: "badge revision", "data-testid": "preview-revision" }, `${at} · read-only`),
      element("span", { class: "muted" }, focusLabel(preview)),
      toggle,
      actions,
    ),
  );
  if (preview.ownership) container.append(ownershipLine(preview.ownership));
  const code = fileCode(preview, { replacementsFor, decorate });
  container.append(element("div", { class: "preview-body" }, code, outline ?? null));
  const focus = code.querySelector<HTMLElement>(".focus");
  if (focus) scrollIntoContainer(container, focus, "center");
}

/** A file preview's code: its lines, numbered, the focus and dirty lines marked; a notebook's under their cells. */
function fileCode(
  preview: FilePreview,
  { replacementsFor, decorate }: Pick<FileLineDrawing, "replacementsFor" | "decorate">,
): HTMLElement {
  const cells = preview.cells ?? [];
  const hitsByLine = new Map(preview.hits.map((hits) => [hits.line, hits.ranges]));
  const dirtyLines = new Set(preview.dirtyLines);
  const code = element("div", { class: "code" });
  for (const [index, text] of preview.lines.entries()) {
    const lineNumber = preview.firstLine + index;
    code.append(...cellHeadingAt(cells, lineNumber, index === 0));
    const classes = ["code-line"];
    const focus = lineNumber >= preview.focusLine && lineNumber <= (preview.focusEndLine ?? preview.focusLine);
    if (focus) classes.push("focus");
    if (dirtyLines.has(lineNumber)) classes.push("dirty");
    const lineCode = element(
      "code",
      {},
      highlight(text, hitsByLine.get(lineNumber) ?? [], replacementsFor(preview.path, lineNumber)),
    );
    decorate(lineCode, text, focus);
    code.append(
      element(
        "div",
        { class: classes.join(" "), "data-line": String(lineNumber) },
        element("span", { class: "line-number" }, String(shownLine(cells, lineNumber))),
        lineCode,
      ),
      ...outputsNoteAt(cells, lineNumber),
    );
  }
  return code;
}

/**
 * The Outline: every definition in the file, each class, type or interface followed by its members, indented. The
 * one the focus line is in is marked. Hidden until its toggle opens it.
 */
function outlinePane(symbols: OutlineSymbol[], focusLine: number, open: boolean, cells: NotebookCell[]): HTMLElement {
  let here: OutlineSymbol | undefined;
  for (const symbol of symbols) if (symbol.line <= focusLine) here = symbol;
  const rows = symbols.map((symbol) => {
    const where = placeOf(cells, symbol.line);
    return element(
      "button",
      {
        type: "button",
        class: symbol === here ? "outline-row here" : "outline-row",
        style: `--depth: ${symbol.depth}`,
        "data-line": String(symbol.line),
        title: `Open beside the panel at ${where.words}`,
        "aria-label": `${symbol.name}, ${symbol.kind}, ${where.words}`,
        ...(symbol === here ? { "aria-current": "true" } : {}),
      },
      element("span", { class: `outline-icon kind-${symbol.kind}`, "aria-hidden": "true" }, OUTLINE_ICON[symbol.kind]),
      element("span", { class: "outline-name" }, symbol.name),
      element("span", { class: "outline-line", "aria-hidden": "true" }, where.short),
    );
  });
  const pane = element("div", { class: "outline-pane", id: "preview-outline", hidden: !open });
  pane.append(outlineSash(pane), element("nav", { class: "outline-list", "aria-label": "Outline" }, ...rows));
  return pane;
}

/** The header button that opens and closes the Outline, with how many definitions it lists. */
function outlineToggle(outline: HTMLElement, count: number, onToggle: (open: boolean) => void): HTMLElement {
  const toggle = element(
    "button",
    {
      type: "button",
      class: "btn outline-toggle",
      "aria-controls": "preview-outline",
      "aria-expanded": String(!outline.hidden),
      "data-testid": "outline-toggle",
      title: "Show or hide the file's definitions",
    },
    "Outline",
    element("span", { class: "count" }, String(count)),
  );
  toggle.addEventListener("click", () => {
    outline.hidden = !outline.hidden;
    toggle.setAttribute("aria-expanded", String(!outline.hidden));
    onToggle(!outline.hidden);
  });
  return toggle;
}

/**
 * Where a double-click in a file preview landed: the word the browser selected, on its line, or the line's start
 * for a double-click on its number.
 */
function doubleClicked(event: MouseEvent): OpenPosition | undefined {
  const row = (event.target as Element).closest<HTMLElement>(".code-line[data-line]");
  const line = Number(row?.dataset["line"]);
  if (!row || !Number.isSafeInteger(line)) return undefined;
  const code = row.querySelector("code");
  const selection = document.getSelection();
  const range = selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : undefined;
  if (!code || !range || !code.contains(range.startContainer)) return { line, column: 1, length: 0 };
  const start = columnOf(code, range.startContainer, range.startOffset);
  const end = code.contains(range.endContainer) ? columnOf(code, range.endContainer, range.endOffset) : start;
  return { line, column: start + 1, length: Math.max(0, end - start) };
}

/**
 * How many UTF-16 characters of the file's line come before `offset` in `node`, in a line's `code`. A replace
 * preview's new text (ins) isn't in the file, so it doesn't count.
 */
function columnOf(code: HTMLElement, node: Node, offset: number): number {
  const before = document.createRange();
  before.setStart(code, 0);
  before.setEnd(node, offset);
  const fragment = before.cloneContents();
  for (const inserted of fragment.querySelectorAll("ins")) inserted.remove();
  return fragment.textContent.length;
}

/** "line 14", or "lines 29–30" for a match that spans lines; in a notebook, "cell 3, line 2". */
function focusLabel({ focusLine, focusEndLine, cells }: FilePreview): string {
  const cell = cellOfLine(cells ?? [], focusLine);
  const shown = (line: number) => (cell ? lineInCell(cell, line) : line);
  const lines =
    focusEndLine !== undefined && focusEndLine > focusLine
      ? `lines ${shown(focusLine)}–${shown(focusEndLine)}`
      : `line ${shown(focusLine)}`;
  return cell ? `cell ${String(cell.number)}, ${lines}` : lines;
}

/** How the Outline names a definition's line: "line 14", or in a notebook "cell 3, line 1", short "3:1". */
function placeOf(cells: NotebookCell[], line: number): { words: string; short: string } {
  const cell = cellOfLine(cells, line);
  if (!cell) return { words: `line ${String(line)}`, short: String(line) };
  const inCell = lineInCell(cell, line);
  return {
    words: `cell ${String(cell.number)}, line ${String(inCell)}`,
    short: `${String(cell.number)}:${String(inCell)}`,
  };
}

/**
 * Under a file preview's header: "Owned by @acme/payments-core @jane ·
 * .github/CODEOWNERS line 4", "No owner · .github/CODEOWNERS line 6 names
 * none", or "No owner · no line of CODEOWNERS matches this file".
 */
function ownershipLine({ owners, file, line }: Ownership): HTMLElement {
  const names = owners.flatMap((owner, index) =>
    index ? [" ", element("code", {}, owner)] : [element("code", {}, owner)],
  );
  const who = owners.length > 0 ? ["Owned by ", ...names] : ["No owner"];
  let where: (Node | string)[] = [file, line > 0 ? ` line ${String(line)}` : ""];
  if (owners.length === 0) {
    where = line > 0 ? [file, ` line ${String(line)} names none`] : ["no line of ", file, " matches this file"];
  }
  return element(
    "div",
    { class: "ownership muted", "data-testid": "ownership" },
    element("span", {}, ...who),
    element("span", { "aria-hidden": "true" }, "·"),
    element("span", {}, ...where),
  );
}

/**
 * How a commit preview lists the files its file filters hid, and how to
 * ask for them, and how it colors its diff's code.
 */
interface CommitPreviewOptions {
  paint: PreviewHandlers["paint"];
  showHiddenFiles: boolean;
  onShowHiddenFiles: () => void;
  /** Whether the query has plain words, which match changed lines or the message. */
  hasTextTerms: boolean;
}

function renderCommitPreview(
  container: HTMLElement,
  preview: CommitPreview,
  { repo, actions }: PreviewHeader,
  { showHiddenFiles, onShowHiddenFiles, hasTextTerms, paint }: CommitPreviewOptions,
): void {
  container.append(
    element(
      "div",
      { class: "preview-header commit" },
      element(
        "div",
        { class: "commit-title" },
        element("span", { class: "preview-title" }, highlight(preview.subject, preview.subjectHits)),
        element("code", { class: "sha" }, shortSha(preview.sha)),
      ),
      element(
        "div",
        { class: "muted" },
        `${preview.author} committed ${timeAgo(preview.at)}${repo ? " · " + repo : ""}`,
      ),
      actions,
    ),
  );
  if (preview.body.trim())
    container.append(element("pre", { class: "body" }, highlight(preview.body, preview.bodyHits)));
  // Plain words match the message too, so a diff without marks isn't a mistake.
  const diffHasHits = preview.hunks.some((hunk) => hunk.lines.some((line) => line.hits.length > 0));
  const messageHasHits = preview.subjectHits.length > 0 || preview.bodyHits.length > 0;
  if (hasTextTerms && messageHasHits && !diffHasHits) {
    container.append(
      element(
        "div",
        { class: "message-note muted", "data-testid": "matched-in-message" },
        "Matched in the message, not in any changed line.",
      ),
    );
  }

  // Files the query's file filters (f:, lang:, is:test) leave out stay hidden until "Show all".
  const hiddenFiles = preview.files.filter((file) => file.hiddenByFilter);
  const shownFiles = preview.files.filter((file) => !file.hiddenByFilter || showHiddenFiles);
  const files = element(
    "div",
    { class: "files" },
    ...shownFiles.map((file) => fileStat(file.path, file.added, file.removed)),
  );
  if (hiddenFiles.length > 0 && !showHiddenFiles) {
    const showAll = button({ class: "btn link", "data-testid": "show-all-files" }, onShowHiddenFiles, "Show all");
    files.append(
      element("span", { class: "muted" }, `${plural(hiddenFiles.length, "other changed file")} hidden by filters`),
      showAll,
    );
  }
  container.append(files);

  const hiddenPaths = new Set(hiddenFiles.map((file) => file.path));
  container.append(diffCode(preview.hunks, (hunk) => hiddenPaths.has(hunk.path) && !showHiddenFiles, paint));
}

/** A commit preview's diff: each hunk's lines, a notebook's under their cells, but those `hidden` says to skip. */
function diffCode(hunks: Hunk[], hidden: (hunk: Hunk) => boolean, paint: CommitPreviewOptions["paint"]): HTMLElement {
  const code = element("div", { class: "code diff" });
  for (const hunk of hunks) {
    if (hidden(hunk)) continue;
    // A notebook's diff numbers lines of its cells' text, which mean nothing to read: each line's cell says instead.
    const notebook = hunk.lines.some((line) => line.cell);
    code.append(
      element(
        "div",
        { class: "hunk" },
        element("span", { class: "muted" }, notebook ? hunk.path : `${hunk.path}  ${hunk.header}`),
      ),
    );
    let lastCell = "";
    for (const line of hunk.lines) {
      if (line.cell && `${String(line.cell.number)} ${line.cell.kind}` !== lastCell) {
        lastCell = `${String(line.cell.number)} ${line.cell.kind}`;
        code.append(cellHeading(line.cell));
      }
      const lineNumber = line.cell ? line.cell.line : line.kind === "del" ? line.oldNo : line.newNo;
      const lineCode = element("code", {}, highlight(line.text, line.hits));
      paint(lineCode, hunk.path, line.text);
      code.append(
        element(
          "div",
          { class: `code-line ${line.kind}` },
          element("span", { class: "line-number" }, String(lineNumber ?? "")),
          element("span", { class: "sign" }, DIFF_SIGN[line.kind]),
          lineCode,
        ),
      );
    }
  }
  return code;
}
