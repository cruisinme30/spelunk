// The panel chrome around the results: index status, health and indexing
// banners, query diagnostics, parsed-query chips and the key
// hints footer.
import { button, clamp, element, isIndexing, percent, plural, progressBar, termClass } from "../format";
import type { Layout } from "../layout";
import { fullName, OPERATOR_TONE } from "../operators";
import { scopedRepo, type OperatorNode, type TextNode } from "../parsedQuery";
import type { BannerMessage, Diagnostic, Fix, Mode, Node as QueryNode, RepoStatus } from "../protocol.gen";
import { hasErrors, type ViewState } from "../state";

/** How the index is doing overall: the status dot's color class and the words beside it. */
interface IndexHealth {
  dotClass: "ok" | "busy" | "error";
  text: string;
}

function indexHealth(repos: RepoStatus[]): IndexHealth {
  const failed = repos.filter((repo) => repo.tree === "error" || repo.history === "error");
  if (failed.length > 0) {
    return { dotClass: "error", text: `Index problem in ${failed.map((repo) => repo.name).join(", ")}` };
  }
  const busy = repos.filter((repo) => isIndexing(repo.tree) || isIndexing(repo.history));
  if (busy.length > 0) return { dotClass: "busy", text: `Indexing ${busy.length} of ${plural(repos.length, "repo")}` };
  return { dotClass: "ok", text: repos.length > 0 ? "Index up to date" : "No folders open" };
}

/** The header dot and text (up to date, indexing, or a problem), and the repos button's label. */
export function renderIndexStatus(layout: Layout, state: ViewState): void {
  const health = indexHealth(state.repos);
  layout.statusDot.className = `status-dot ${health.dotClass}`;
  layout.statusText.textContent = health.text;
  layout.reposButton.textContent = scopedRepo(state.parsed) ?? `All repos · ${state.repos.length}`;
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

function healthTitle(banner: BannerMessage): string {
  switch (banner.state) {
    case "restarting": {
      return "Search restarting…";
    }
    case "stopped": {
      return "Search stopped";
    }
    case "ok":
    case "protocolMismatch": {
      return banner.message ?? "Search unavailable";
    }
  }
}

function restartButton(onRestart: () => void): HTMLElement {
  return button({ class: "btn", "data-testid": "restart" }, onRestart, "Restart");
}

function renderHealthBanner(banner: BannerMessage, onRestart: () => void): HTMLElement {
  const title = healthTitle(banner);
  const detail =
    banner.message && banner.state !== "restarting" && banner.message !== title
      ? element("span", {}, banner.message)
      : null;
  const restart = banner.state === "stopped" ? restartButton(onRestart) : null;
  const tone = banner.state === "restarting" ? "warn" : "error";
  return element(
    "div",
    { class: `banner ${tone}`, role: "alert" },
    element("span", { class: "strong" }, title),
    detail,
    restart,
  );
}

function renderIndexingBanner(repo: RepoStatus): HTMLElement | null {
  const phase = repo.tree === "indexing" ? "files" : repo.history === "indexing" ? "history" : "";
  if (!phase) return null;
  const percentDone = percent(repo.progress);
  const bar = progressBar(percentDone, `${repo.name} ${phase} indexing`);
  const note = repo.message ?? "Results from this repo may be incomplete. Search keeps working while it finishes.";
  return element(
    "div",
    { class: "banner index", "data-testid": "indexing-banner" },
    element("span", { class: "strong" }, `Indexing ${repo.name} · ${percentDone}%`),
    bar,
    element("span", {}, note),
  );
}

/** Query errors and warnings with their fix-it buttons. */
export function renderDiagnostics(layout: Layout, state: ViewState, onFix: (fix: Fix) => void): void {
  const raw = state.parsed?.raw ?? "";
  const diagnostics = state.parsed?.diagnostics ?? [];
  layout.diagnostics.replaceChildren(...diagnostics.map((diagnostic) => renderDiagnostic(raw, diagnostic, onFix)));
}

function renderDiagnostic(raw: string, diagnostic: Diagnostic, onFix: (fix: Fix) => void): HTMLElement {
  // A span outside the text marks its end, rather than breaking the caret's " ".repeat.
  const start = clamp(Math.trunc(diagnostic.span.start) || 0, 0, raw.length);
  const end = clamp(Math.trunc(diagnostic.span.end) || 0, start, raw.length);
  const offending = raw.slice(start, Math.max(end, start + 1)) || " ";
  const snippet = element(
    "code",
    { class: "snippet" },
    raw.slice(0, start),
    element("span", { class: "bad" }, offending),
    raw.slice(end),
  );
  const caret = element("code", { class: "caret" }, " ".repeat(start) + "^");
  const fixes = diagnostic.fixes.map((fix, index) =>
    button(
      { class: "btn fix", "data-testid": "fix" },
      () => {
        onFix(fix);
      },
      fix.title,
      index === 0 ? element("kbd", {}, "⌘.") : null,
    ),
  );
  return element(
    "div",
    { class: `diagnostic ${diagnostic.severity}`, "data-code": diagnostic.code },
    element(
      "div",
      { class: "diagnostic-main" },
      element("span", { class: "diagnostic-message" }, diagnostic.message),
      element("div", { class: "snippet-wrap" }, snippet, element("br"), caret),
    ),
    element("div", { class: "diagnostic-fixes" }, ...fixes),
  );
}

/** The parsed query as chips, with the mode and the result summary. */
export function renderChips(layout: Layout, state: ViewState, summary: HTMLElement): void {
  layout.chips.replaceChildren();
  const query = state.parsed;
  if (!state.ui.showParsedQuery || !query?.root || hasErrors(state)) return;
  const modeLabel = query.mode === "history" ? "Commit history" : "Working tree";
  layout.chips.append(
    element("span", { class: "label" }, "Query"),
    ...chipsFor(query.root, { mode: query.mode, fileNamesOnly: query.globals.type === "file" }),
    element("span", { class: `mode ${query.mode}`, "data-testid": "mode" }, modeLabel),
    summary,
  );
}

/** A word between chips: AND, OR, NOT or a parenthesis. */
const joiner = (text: string, extraClass = "") => element("span", { class: `joiner ${extraClass}`.trim() }, text);

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
function textLabel(node: TextNode, context: ChipContext): string {
  if (node.match === "regex") return "regex";
  if (context.mode === "history") return "diff contains";
  return context.fileNamesOnly ? "name" : "text";
}

function chipsFor(node: QueryNode, context: ChipContext): Node[] {
  switch (node.kind) {
    case "and": {
      return joinChips(node.children, "AND", context);
    }
    case "or": {
      return [joiner("("), ...joinChips(node.children, "OR", context), joiner(")")];
    }
    case "not": {
      return [joiner("NOT", "not"), ...chipsFor(node.child, context)];
    }
    case "text": {
      const label = textLabel(node, context);
      return [
        element(
          "span",
          { class: `chip term ${termClass(node.termIndex)}` },
          element("span", { class: "chip-label" }, label),
          element("code", {}, node.value),
        ),
      ];
    }
    case "op": {
      return [
        element(
          "span",
          { class: `chip tone-${OPERATOR_TONE[node.op]}` },
          element("span", { class: "chip-label" }, fullName(node.op)),
          element("code", {}, operatorChipValue(node)),
          node.resolved ? element("span", { class: "resolved" }, `→ ${node.resolved.label}`) : null,
        ),
      ];
    }
  }
}

/** How an operator's value reads on its chip: case:yes reads "sensitive", type:file "file names only". */
function operatorChipValue(node: OperatorNode): string {
  if (node.op === "case") return node.value === "yes" ? "sensitive" : "insensitive";
  if (node.op === "type") return TYPE_LABEL[node.value] ?? node.value;
  return node.value;
}

/** One key hint: the key, then what it does. */
const hint = (key: string, what: string) => element("span", {}, element("kbd", {}, key), what);
/** A key hint pushed to the right end of the footer. */
const hintAtEnd = (key: string, what: string) =>
  element("span", { class: "hint-at-end" }, element("kbd", {}, key), what);

/** What the panel shows, which decides the key hints. */
export type FooterMode = "empty" | "errors" | "results" | "operators" | "values";

/** Key hints along the bottom, which change with what the panel shows. */
export function renderFooter(layout: Layout, mode: FooterMode, isHistory: boolean): void {
  const hints: Record<FooterMode, HTMLElement[]> = {
    empty: [
      hint("↑↓", "move"),
      hint("↵", "run recent query"),
      hint("⇧⌫", "remove"),
      hintAtEnd("?", "opens this sheet anytime"),
    ],
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
