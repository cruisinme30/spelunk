// The preview pane: a file excerpt around the match, or a commit's diff.
import { el, fileStat, highlight, plural, shortSha, timeAgo } from "../format";
import type { OpenWhere, Preview } from "../protocol.gen";
import { repoName, type ViewState } from "../state";

type FilePreview = Extract<Preview, { kind: "file" }>;
type CommitPreview = Extract<Preview, { kind: "commit" }>;

/** Definitions listed under a file preview ("In this file: …"). */
const MAX_OUTLINE_SYMBOLS = 8;
const DIFF_SIGN = { add: "+", del: "−", ctx: " " } as const;

export interface PreviewHandlers {
  onOpen(ref: string, where: OpenWhere): void;
  onShowHiddenFiles(): void;
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
    container.append(el("div", { class: "notice" }, current.stale ? "No longer exists" : "No preview available"));
    return;
  }
  const preview = current.preview;
  const actions = el(
    "div",
    { class: "actions" },
    el(
      "button",
      { type: "button", class: "btn", "data-where": "current" },
      preview.kind === "commit" ? "Open diff ↵" : "Open ↵",
    ),
    el("button", { type: "button", class: "btn", "data-where": "side" }, "Open to side ⌘↵"),
  );
  actions.addEventListener("click", (event) => {
    const where = (event.target as HTMLElement).closest<HTMLElement>("[data-where]")?.dataset.where;
    if (where === "current" || where === "side") handlers.onOpen(current.ref, where);
  });
  const repo = repoId ? repoName(state, repoId) : "";
  if (preview.kind === "file") {
    renderFilePreview(container, preview, repo, actions);
  } else {
    renderCommitPreview(container, preview, repo, actions, {
      showHiddenFiles: state.showHiddenFiles,
      onShowHiddenFiles: handlers.onShowHiddenFiles,
    });
  }
}

function renderFilePreview(container: HTMLElement, preview: FilePreview, repo: string, actions: HTMLElement): void {
  container.append(
    el(
      "div",
      { class: "phead" },
      el("span", { class: "ptitle" }, el("span", { class: "muted" }, repo ? `${repo} / ` : ""), preview.path),
      el("span", { class: "muted" }, `line ${preview.focusLine}`),
      actions,
    ),
  );
  const hitsByLine = new Map(preview.hits.map((hits) => [hits.line, hits.ranges]));
  const dirtyLines = new Set(preview.dirtyLines);
  const code = el("div", { class: "code" });
  preview.lines.forEach((text, index) => {
    const lineNumber = preview.firstLine + index;
    const classes = ["cl"];
    if (lineNumber === preview.focusLine) classes.push("focus");
    if (dirtyLines.has(lineNumber)) classes.push("dirty");
    code.append(
      el(
        "div",
        { class: classes.join(" ") },
        el("span", { class: "ln" }, String(lineNumber)),
        el("code", {}, highlight(text, hitsByLine.get(lineNumber) ?? [])),
      ),
    );
  });
  container.append(code);
  code.querySelector(".focus")?.scrollIntoView({ block: "center" });
  if (preview.symbols?.length) {
    const names = preview.symbols.slice(0, MAX_OUTLINE_SYMBOLS).map((symbol) => el("code", {}, symbol.name));
    container.append(el("div", { class: "outline muted" }, "In this file: ", ...names));
  }
}

interface HiddenFilesOptions {
  showHiddenFiles: boolean;
  onShowHiddenFiles(): void;
}

function renderCommitPreview(
  container: HTMLElement,
  preview: CommitPreview,
  repo: string,
  actions: HTMLElement,
  hidden: HiddenFilesOptions,
): void {
  container.append(
    el(
      "div",
      { class: "phead commit" },
      el(
        "div",
        { class: "line1" },
        el("span", { class: "ptitle" }, preview.subject),
        el("code", { class: "sha" }, shortSha(preview.sha)),
      ),
      el("div", { class: "muted" }, `${preview.author} committed ${timeAgo(preview.at)}${repo ? " · " + repo : ""}`),
      actions,
    ),
  );
  if (preview.body.trim()) container.append(el("pre", { class: "body" }, preview.body.trim()));

  // Files outside an f: filter stay hidden until "Show all".
  const hiddenFiles = preview.files.filter((file) => file.hiddenByFilter);
  const shownFiles = preview.files.filter((file) => !file.hiddenByFilter || hidden.showHiddenFiles);
  const files = el(
    "div",
    { class: "files" },
    ...shownFiles.map((file) => fileStat(file.path, file.added, file.removed)),
  );
  if (hiddenFiles.length && !hidden.showHiddenFiles) {
    const showAll = el("button", { type: "button", class: "btn link", "data-testid": "show-all-files" }, "Show all");
    showAll.addEventListener("click", hidden.onShowHiddenFiles);
    files.append(
      el("span", { class: "muted" }, `${plural(hiddenFiles.length, "other changed file")} hidden by f:`),
      showAll,
    );
  }
  container.append(files);

  const hiddenPaths = new Set(hiddenFiles.map((file) => file.path));
  const code = el("div", { class: "code diff" });
  for (const hunk of preview.hunks) {
    if (hiddenPaths.has(hunk.path) && !hidden.showHiddenFiles) continue;
    code.append(el("div", { class: "hunk" }, el("span", { class: "muted" }, `${hunk.path}  ${hunk.header}`)));
    for (const line of hunk.lines) {
      const lineNumber = line.kind === "del" ? line.oldNo : line.newNo;
      code.append(
        el(
          "div",
          { class: `cl ${line.kind}` },
          el("span", { class: "ln" }, String(lineNumber ?? "")),
          el("span", { class: "sign" }, DIFF_SIGN[line.kind]),
          el("code", {}, highlight(line.text, line.hits)),
        ),
      );
    }
  }
  container.append(code);
}
