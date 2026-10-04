// The panel chrome around the results: index status, health and indexing
// banners, completions, query diagnostics, parsed-query chips and the key
// hints footer.
import { el, plural, termClass } from "../format";
import type { Layout } from "../layout";
import type { BannerMsg, Diagnostic, Fix, IndexState, Mode, Node as QueryNode, RepoStatus } from "../protocol.gen";
import { OP_LABEL, OP_TONE } from "../sheet";
import { scopedRepo } from "../queryEdit";
import { hasErrors, type ViewState } from "../state";

const isBusy = (state: IndexState) => state === "indexing" || state === "queued";

/** The header dot and text: up to date, indexing, or a problem. */
export function renderIndexStatus(layout: Layout, state: ViewState): void {
  const repos = state.repos;
  const busy = repos.filter((repo) => isBusy(repo.tree) || isBusy(repo.history));
  const failed = repos.filter((repo) => repo.tree === "error" || repo.history === "error");
  let dot = "ok";
  let text = repos.length ? "Index up to date" : "No folders open";
  if (failed.length) {
    dot = "err";
    text = `Index problem in ${failed.map((repo) => repo.name).join(", ")}`;
  } else if (busy.length) {
    dot = "busy";
    text = `Indexing ${busy.length} of ${plural(repos.length, "repo")}`;
  }
  layout.statusDot.className = `dot ${dot}`;
  layout.statusText.textContent = text;
  const scoped = scopedRepo(state.parsed);
  layout.reposButton.textContent = scoped ?? `All repos · ${repos.length}`;
}

/** Daemon health (restarting, stopped) followed by one banner per repo still indexing. */
export function renderBanners(layout: Layout, state: ViewState, onRestart: () => void): void {
  layout.banner.replaceChildren();
  if (state.banner) layout.banner.append(renderHealthBanner(state.banner, onRestart));
  for (const repo of state.repos) {
    const banner = renderIndexingBanner(repo);
    if (banner) layout.banner.append(banner);
  }
}

function healthTitle(banner: BannerMsg): string {
  switch (banner.state) {
    case "restarting":
      return "Search restarting…";
    case "stopped":
      return "Search stopped";
    default:
      return banner.message ?? "Search unavailable";
  }
}

function renderHealthBanner(banner: BannerMsg, onRestart: () => void): HTMLElement {
  const title = healthTitle(banner);
  const detail =
    banner.message && banner.state !== "restarting" && banner.message !== title ? el("span", {}, banner.message) : null;
  let restart: HTMLElement | null = null;
  if (banner.state === "stopped") {
    restart = el("button", { type: "button", class: "btn", "data-testid": "restart" }, "Restart");
    restart.addEventListener("click", onRestart);
  }
  const tone = banner.state === "restarting" ? "warn" : "error";
  return el("div", { class: `banner ${tone}`, role: "alert" }, el("span", { class: "strong" }, title), detail, restart);
}

function renderIndexingBanner(repo: RepoStatus): HTMLElement | null {
  const phase = repo.tree === "indexing" ? "files" : repo.history === "indexing" ? "history" : "";
  if (!phase) return null;
  const percent = Math.round((repo.progress ?? 0) * 100);
  const bar = el(
    "span",
    {
      class: "progress",
      role: "progressbar",
      "aria-valuenow": percent,
      "aria-valuemin": 0,
      "aria-valuemax": 100,
      "aria-label": `${repo.name} ${phase} indexing`,
    },
    el("span", { style: `width:${percent}%` }),
  );
  const note = repo.message ?? "Results from this repo may be incomplete. Search keeps working while it finishes.";
  return el(
    "div",
    { class: "banner index", "data-testid": "indexing-banner" },
    el("span", { class: "strong" }, `Indexing ${repo.name} · ${percent}%`),
    bar,
    el("span", {}, note),
  );
}

/** Query errors and warnings with their fix-it buttons. */
export function renderDiagnostics(layout: Layout, state: ViewState, onFix: (fix: Fix) => void): void {
  const raw = state.parsed?.raw ?? "";
  const diagnostics = state.parsed?.diagnostics ?? [];
  layout.diagnostics.replaceChildren(...diagnostics.map((diagnostic) => renderDiagnostic(raw, diagnostic, onFix)));
}

function renderDiagnostic(raw: string, diagnostic: Diagnostic, onFix: (fix: Fix) => void): HTMLElement {
  const { start, end } = diagnostic.span;
  const offending = raw.slice(start, Math.max(end, start + 1)) || " ";
  const snippet = el(
    "code",
    { class: "snippet" },
    raw.slice(0, start),
    el("span", { class: "bad" }, offending),
    raw.slice(end),
  );
  const caret = el("code", { class: "caret" }, " ".repeat(start) + "^");
  const fixes = diagnostic.fixes.map((fix, index) => {
    const button = el("button", { type: "button", class: "btn fix", "data-testid": "fix" }, fix.title);
    if (index === 0) button.append(el("kbd", {}, "⌘."));
    button.addEventListener("click", () => onFix(fix));
    return button;
  });
  return el(
    "div",
    { class: `diag ${diagnostic.severity}`, "data-code": diagnostic.code },
    el(
      "div",
      { class: "dmain" },
      el("span", { class: "dtitle" }, diagnostic.message),
      el("div", { class: "snippet-wrap" }, snippet, el("br"), caret),
    ),
    el("div", { class: "dfixes" }, ...fixes),
  );
}

/** The parsed query as chips, with the mode and the result summary. */
export function renderChips(layout: Layout, state: ViewState, summary: HTMLElement): void {
  layout.chips.replaceChildren();
  const query = state.parsed;
  if (!state.ui.showParsedQuery || !query?.root || hasErrors(state)) return;
  const modeLabel = query.mode === "history" ? "Commit history" : "Working tree";
  layout.chips.append(
    el("span", { class: "label" }, "Query"),
    ...chipsFor(query.root, { mode: query.mode, fileNamesOnly: query.globals.type === "file" }),
    el("span", { class: `mode ${query.mode}`, "data-testid": "mode" }, modeLabel),
    summary,
  );
}

const joiner = (text: string, extraClass = "") => el("span", { class: `joiner ${extraClass}`.trim() }, text);

/** What a chip's wording depends on beyond its own node. */
interface ChipContext {
  mode: Mode;
  /** type:file: text terms match file names only. */
  fileNamesOnly: boolean;
}

/** How a type: value reads in its chip. */
const TYPE_LABEL: Record<string, string> = { file: "file names only", code: "code lines only", commit: "commits only" };

/** Children's chips separated by AND or OR. */
function joinChips(children: QueryNode[], word: "AND" | "OR", context: ChipContext): Node[] {
  return children.flatMap((child, index) =>
    index ? [joiner(word), ...chipsFor(child, context)] : chipsFor(child, context),
  );
}

/** The word on a text term's chip: how it matches in this query. */
function textLabel(node: Extract<QueryNode, { kind: "text" }>, context: ChipContext): string {
  if (node.match === "regex") return "regex";
  if (context.mode === "history") return "diff contains";
  return context.fileNamesOnly ? "name" : "text";
}

function chipsFor(node: QueryNode, context: ChipContext): Node[] {
  switch (node.kind) {
    case "and":
      return joinChips(node.children, "AND", context);
    case "or":
      return [joiner("("), ...joinChips(node.children, "OR", context), joiner(")")];
    case "not":
      return [joiner("NOT", "not"), ...chipsFor(node.child, context)];
    case "text": {
      const label = textLabel(node, context);
      return [
        el(
          "span",
          { class: `chip term ${termClass(node.termIndex)}` },
          el("span", { class: "k" }, label),
          el("code", {}, node.value),
        ),
      ];
    }
    case "op": {
      let value = node.value;
      if (node.op === "case") value = node.value === "yes" ? "sensitive" : "insensitive";
      if (node.op === "type") value = TYPE_LABEL[node.value] ?? node.value;
      return [
        el(
          "span",
          { class: `chip tone-${OP_TONE[node.op]}` },
          el("span", { class: "k" }, OP_LABEL[node.op]),
          el("code", {}, value),
          node.resolved ? el("span", { class: "resolved" }, `→ ${node.resolved.label}`) : null,
        ),
      ];
    }
  }
}

export type FooterMode = "empty" | "errors" | "results" | "operators" | "values";

/** Key hints along the bottom, which change with what the panel shows. */
export function renderFooter(layout: Layout, mode: FooterMode, isHistory: boolean): void {
  const hint = (key: string, what: string) => el("span", {}, el("kbd", {}, key), what);
  const hintAtEnd = (key: string, what: string) => el("span", { class: "push" }, el("kbd", {}, key), what);
  const hints: Record<FooterMode, HTMLElement[]> = {
    empty: [hint("↑↓", "move"), hint("↵", "run recent query"), hintAtEnd("?", "opens this sheet anytime")],
    errors: [hint("⌘.", "apply first fix"), hint("Tab", "complete operator"), hintAtEnd("?", "all operators")],
    operators: [
      hint("↑↓", "pick"),
      hint("Tab", "insert operator"),
      hint("Esc", "dismiss"),
      hintAtEnd("?", "all operators"),
    ],
    values: [
      hint("↑↓", "pick"),
      hint("Tab", "insert value"),
      hint("Esc", "keep as typed"),
      hintAtEnd("?", "all operators"),
    ],
    results: [
      hint("↑↓", "move"),
      hint("↵", isHistory ? "open diff" : "open"),
      hint("⌘↵", "open to side"),
      hint("Tab", "complete operator"),
      hintAtEnd("Esc", "close"),
    ],
  };
  layout.footer.replaceChildren(...hints[mode]);
}
