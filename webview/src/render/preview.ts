// The preview pane: a file excerpt around the match, or a commit's diff.
import { el, highlight, plural, timeAgo } from "../format";
import type { Preview } from "../protocol.gen";
import { repoName, type PreviewState, type ViewState } from "../state";
import { fileStat } from "./results";

type FilePreview = Extract<Preview, { kind: "file" }>;
type CommitPreview = Extract<Preview, { kind: "commit" }>;

export interface PreviewHandlers {
  onOpen(ref: string, where: "current" | "side"): void;
  onShowHiddenFiles(): void;
}

export function renderPreview(
  container: HTMLElement,
  state: ViewState,
  repoId: string | undefined,
  handlers: PreviewHandlers,
): void {
  container.replaceChildren();
  const current: PreviewState | undefined = state.preview;
  if (!current) return;
  if (!current.preview) {
    container.append(el("div", { class: "notice" }, current.stale ? "No longer exists" : "No preview available"));
    return;
  }
  const actions = el(
    "div",
    { class: "actions" },
    el(
      "button",
      { type: "button", class: "btn", "data-act": "current" },
      current.preview.kind === "commit" ? "Open diff ↵" : "Open ↵",
    ),
    el("button", { type: "button", class: "btn", "data-act": "side" }, "Open to side ⌘↵"),
  );
  actions.addEventListener("click", (event) => {
    const where = (event.target as HTMLElement).closest<HTMLElement>("[data-act]")?.dataset.act;
    if (where === "current" || where === "side") handlers.onOpen(current.ref, where);
  });
  const repo = repoId ? repoName(state, repoId) : "";
  if (current.preview.kind === "file") renderFile(container, current.preview, repo, actions);
  else renderCommit(container, current.preview, repo, actions, state.showHiddenFiles, handlers.onShowHiddenFiles);
}

function renderFile(container: HTMLElement, preview: FilePreview, repo: string, actions: HTMLElement): void {
  container.append(
    el(
      "div",
      { class: "phead" },
      el("span", { class: "ptitle" }, el("span", { class: "muted" }, repo ? `${repo} / ` : ""), preview.path),
      el("span", { class: "muted" }, `line ${preview.focusLine}`),
      actions,
    ),
  );
  const hitsByLine = new Map(preview.hits.map((h) => [h.line, h.ranges]));
  const dirty = new Set(preview.dirtyLines);
  const code = el("div", { class: "code" });
  preview.lines.forEach((text, index) => {
    const line = preview.firstLine + index;
    const classes = ["cl", line === preview.focusLine ? "focus" : "", dirty.has(line) ? "dirty" : ""]
      .filter(Boolean)
      .join(" ");
    code.append(
      el(
        "div",
        { class: classes },
        el("span", { class: "ln" }, String(line)),
        el("code", {}, highlight(text, hitsByLine.get(line) ?? [])),
      ),
    );
  });
  container.append(code);
  code.querySelector(".focus")?.scrollIntoView({ block: "center" });
  if (preview.symbols?.length) {
    const names = preview.symbols.slice(0, 8).map((s) => el("code", {}, s.name));
    container.append(el("div", { class: "outline muted" }, "In this file: ", ...names));
  }
}

function renderCommit(
  container: HTMLElement,
  preview: CommitPreview,
  repo: string,
  actions: HTMLElement,
  showHidden: boolean,
  onShowHiddenFiles: () => void,
): void {
  container.append(
    el(
      "div",
      { class: "phead commit" },
      el(
        "div",
        { class: "line1" },
        el("span", { class: "ptitle" }, preview.subject),
        el("code", { class: "sha" }, preview.sha.slice(0, 7)),
      ),
      el("div", { class: "muted" }, `${preview.author} committed ${timeAgo(preview.at)}${repo ? " · " + repo : ""}`),
      actions,
    ),
  );
  if (preview.body.trim()) container.append(el("pre", { class: "body" }, preview.body.trim()));

  // Files outside an f: filter are hidden until "Show all" (mock 3).
  const hiddenFiles = preview.files.filter((f) => f.hiddenByFilter);
  const shownFiles = preview.files.filter((f) => !f.hiddenByFilter || showHidden);
  const files = el("div", { class: "files" }, ...shownFiles.map((f) => fileStat(f.path, f.added, f.removed)));
  if (hiddenFiles.length && !showHidden) {
    const showAll = el("button", { type: "button", class: "btn link", "data-testid": "show-all-files" }, "Show all");
    showAll.addEventListener("click", onShowHiddenFiles);
    files.append(
      el("span", { class: "muted" }, `${plural(hiddenFiles.length, "other changed file")} hidden by f:`),
      showAll,
    );
  }
  container.append(files);

  const hiddenPaths = new Set(hiddenFiles.map((f) => f.path));
  const code = el("div", { class: "code diff" });
  for (const hunk of preview.hunks) {
    if (hiddenPaths.has(hunk.path) && !showHidden) continue;
    code.append(el("div", { class: "hunk" }, el("span", { class: "muted" }, `${hunk.path}  ${hunk.header}`)));
    for (const line of hunk.lines) {
      const sign = line.kind === "add" ? "+" : line.kind === "del" ? "−" : " ";
      const number = line.kind === "del" ? line.oldNo : line.newNo;
      code.append(
        el(
          "div",
          { class: `cl ${line.kind}` },
          el("span", { class: "ln" }, String(number ?? "")),
          el("span", { class: "sign" }, sign),
          el("code", {}, highlight(line.text, line.hits)),
        ),
      );
    }
  }
  container.append(code);
}
