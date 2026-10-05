// The preview pane: a file excerpt around the match, or a commit's diff.
import { element, fileStat, highlight, plural, scrollIntoContainer, shortSha, timeAgo } from "../format";
import type { OpenWhere, Preview } from "../protocol.gen";
import { repoName, type ViewState } from "../state";

type FilePreview = Extract<Preview, { kind: "file" }>;
type CommitPreview = Extract<Preview, { kind: "commit" }>;

/** Definitions listed under a file preview ("In this file: …"). */
const MAX_OUTLINE_SYMBOLS = 8;
const DIFF_SIGN = { add: "+", del: "−", ctx: " " } as const;

/** What the preview's buttons do. */
export interface PreviewHandlers {
  onOpen: (ref: string, where: OpenWhere) => void;
  onShowHiddenFiles: () => void;
}

/** Fills `container` with the preview in `state.preview`, if any. */
export function renderPreview(
  container: HTMLElement,
  state: ViewState,
  repoId: string | undefined,
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
  );
  // One listener for both buttons: each says where it opens in data-where.
  actions.addEventListener("click", (event) => {
    const where = (event.target as HTMLElement).closest<HTMLElement>("[data-where]")?.dataset["where"];
    if (where === "current" || where === "side") handlers.onOpen(current.ref, where);
  });
  const header = { repo: repoId ? repoName(state, repoId) : "", actions };
  if (preview.kind === "file") {
    renderFilePreview(container, preview, header);
  } else {
    renderCommitPreview(container, preview, header, {
      showHiddenFiles: state.showHiddenFiles,
      onShowHiddenFiles: handlers.onShowHiddenFiles,
    });
  }
}

/** What every preview header shows besides the title: the repo name and the Open buttons. */
interface PreviewHeader {
  repo: string;
  actions: HTMLElement;
}

function renderFilePreview(container: HTMLElement, preview: FilePreview, { repo, actions }: PreviewHeader): void {
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
      element("span", { class: "muted" }, `line ${preview.focusLine}`),
      actions,
    ),
  );
  const hitsByLine = new Map(preview.hits.map((hits) => [hits.line, hits.ranges]));
  const dirtyLines = new Set(preview.dirtyLines);
  const code = element("div", { class: "code" });
  for (const [index, text] of preview.lines.entries()) {
    const lineNumber = preview.firstLine + index;
    const classes = ["code-line"];
    if (lineNumber === preview.focusLine) classes.push("focus");
    if (dirtyLines.has(lineNumber)) classes.push("dirty");
    code.append(
      element(
        "div",
        { class: classes.join(" ") },
        element("span", { class: "line-number" }, String(lineNumber)),
        element("code", {}, highlight(text, hitsByLine.get(lineNumber) ?? [])),
      ),
    );
  }
  container.append(code);
  const focus = code.querySelector<HTMLElement>(".focus");
  if (focus) scrollIntoContainer(container, focus, "center");
  if (preview.symbols && preview.symbols.length > 0) {
    const names = preview.symbols.slice(0, MAX_OUTLINE_SYMBOLS).map((symbol) => element("code", {}, symbol.name));
    container.append(element("div", { class: "outline muted" }, "In this file: ", ...names));
  }
}

/** Whether a commit preview lists the files an f: filter hid, and how to ask for them. */
interface HiddenFilesToggle {
  showHiddenFiles: boolean;
  onShowHiddenFiles: () => void;
}

function renderCommitPreview(
  container: HTMLElement,
  preview: CommitPreview,
  { repo, actions }: PreviewHeader,
  { showHiddenFiles, onShowHiddenFiles }: HiddenFilesToggle,
): void {
  container.append(
    element(
      "div",
      { class: "preview-header commit" },
      element(
        "div",
        { class: "commit-title" },
        element("span", { class: "preview-title" }, preview.subject),
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
  if (preview.body.trim()) container.append(element("pre", { class: "body" }, preview.body.trim()));

  // Files outside an f: filter stay hidden until "Show all".
  const hiddenFiles = preview.files.filter((file) => file.hiddenByFilter);
  const shownFiles = preview.files.filter((file) => !file.hiddenByFilter || showHiddenFiles);
  const files = element(
    "div",
    { class: "files" },
    ...shownFiles.map((file) => fileStat(file.path, file.added, file.removed)),
  );
  if (hiddenFiles.length > 0 && !showHiddenFiles) {
    const showAll = element(
      "button",
      { type: "button", class: "btn link", "data-testid": "show-all-files" },
      "Show all",
    );
    showAll.addEventListener("click", onShowHiddenFiles);
    files.append(
      element("span", { class: "muted" }, `${plural(hiddenFiles.length, "other changed file")} hidden by f:`),
      showAll,
    );
  }
  container.append(files);

  const hiddenPaths = new Set(hiddenFiles.map((file) => file.path));
  const code = element("div", { class: "code diff" });
  for (const hunk of preview.hunks) {
    if (hiddenPaths.has(hunk.path) && !showHiddenFiles) continue;
    code.append(element("div", { class: "hunk" }, element("span", { class: "muted" }, `${hunk.path}  ${hunk.header}`)));
    for (const line of hunk.lines) {
      const lineNumber = line.kind === "del" ? line.oldNo : line.newNo;
      code.append(
        element(
          "div",
          { class: `code-line ${line.kind}` },
          element("span", { class: "line-number" }, String(lineNumber ?? "")),
          element("span", { class: "sign" }, DIFF_SIGN[line.kind]),
          element("code", {}, highlight(line.text, line.hits)),
        ),
      );
    }
  }
  container.append(code);
}
