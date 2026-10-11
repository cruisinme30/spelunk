// The panel chrome around the results: index status, health and indexing
// banners, query diagnostics, parsed-query chips and the key
// hints footer.
import { button, clamp, element, isIndexing, percent, plural, progressBar, termClass } from "../format";
import type { Layout } from "../layout";
import { fullName, OPERATOR_TONE } from "../operators";
import { diffSide, operatorNodes, scopedRepo, type OperatorNode, type TextNode } from "../parsedQuery";
import type {
  BannerMessage,
  CaseSetting,
  Diagnostic,
  Fix,
  Mode,
  ParsedQuery,
  Node as QueryNode,
  RepoStatus,
  RevisionInfo,
} from "../protocol.gen";
import { isCasePressed, isCaseSmart } from "../queryEdit";
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

/**
 * Aa: pressed when case matches. Under smart case it carries a dot, and its
 * tooltip says which way the query's capital letters sent it.
 */
export function renderCaseToggle(
  button: HTMLButtonElement,
  query: ParsedQuery | undefined,
  setting: CaseSetting,
): void {
  const pressed = isCasePressed(query, setting);
  const smart = isCaseSmart(query, setting);
  button.setAttribute("aria-pressed", String(pressed));
  button.classList.toggle("smart", smart);
  if (!smart) button.title = "Match case (case:yes)";
  else if (pressed) button.title = "Smart case: matching case, since the query has a capital letter";
  else button.title = "Smart case: ignoring case until the query has a capital letter";
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
  // An empty span marks the character at it, which the rest then starts after.
  const markedEnd = Math.max(end, Math.min(start + 1, raw.length));
  const offending = raw.slice(start, markedEnd) || " ";
  const snippet = element(
    "code",
    { class: "snippet" },
    raw.slice(0, start),
    element("span", { class: "bad" }, offending),
    raw.slice(markedEnd),
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
  const at = query.mode === "workingTree" ? query.globals.at : undefined;
  const mode = at === undefined ? query.mode : "revision";
  const revisions = state.done?.seq === state.seq ? state.done.revisions : undefined;
  // f: in history matches a file under the names it had before, which the chip says.
  const follows = query.mode === "history" && operatorNodes(query, "f").length > 0;
  const modeLabel = {
    history: follows ? "Commit history · follows renames" : "Commit history",
    workingTree: "Working tree",
    revision: revisionLabel(at, revisions),
  }[mode];
  layout.chips.append(
    element("span", { class: "label" }, "Query"),
    ...chipsFor(query.root, {
      mode: query.mode,
      fileNamesOnly: query.globals.type === "file",
      diffSide: diffSide(query),
    }),
    element("span", { class: `mode ${mode}`, "data-testid": "mode" }, modeLabel),
    summary,
  );
}

/** A value of at: that reads as a commit's sha, in full or in part, rather than a tag or branch such as "3". */
const SHA_LIKE = /^[0-9a-f]{4,40}$/i;

/**
 * What an at: search reads: "At v1.0.0", then, once the search names the commit, "At v1.0.0 · 3f9c2e1 · 14 Mar
 * 2026", or "At 6m · 2 commits" when the repos read different ones. A sha typed in full or in part isn't repeated.
 */
function revisionLabel(at = "", revisions: RevisionInfo[] = []): string {
  const commits = [...new Set(revisions.flatMap((revision) => (revision.sha ? [revision.sha] : [])))];
  const [only] = commits;
  if (commits.length > 1) return `At ${at} · ${plural(commits.length, "commit")}`;
  if (only === undefined) return `At ${at}`;
  const date = revisions.find((revision) => revision.sha === only)?.date;
  const day = date
    ? new Date(date).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric", timeZone: "UTC" })
    : "";
  const sha = SHA_LIKE.test(at) && only.startsWith(at.toLowerCase()) ? [] : [only.slice(0, 7)];
  return ["At " + at, ...sha, ...(day ? [day] : [])].join(" · ");
}

/** A word between chips: AND, OR, NOT or a parenthesis. */
const joiner = (text: string, extraClass = "") => element("span", { class: `joiner ${extraClass}`.trim() }, text);

/** What a chip's wording depends on beyond its own node. */
interface ChipContext {
  mode: Mode;
  /** type:file: text terms match file names only. */
  fileNamesOnly: boolean;
  /** type:added or type:removed: text terms match only that side of each diff. */
  diffSide: "added" | "removed" | null;
}

/** How a type: value reads in its chip. */
const TYPE_LABEL: Record<string, string> = {
  file: "file names only",
  code: "code lines only",
  commit: "commits only",
  added: "added lines only",
  removed: "removed lines only",
};

/** Children's chips separated by AND or OR. */
function joinChips(children: QueryNode[], word: "AND" | "OR", context: ChipContext): Node[] {
  return children.flatMap((child, index) =>
    index ? [joiner(word), ...chipsFor(child, context)] : chipsFor(child, context),
  );
}

/** The word on a text term's chip: how it matches in this query. content: terms never match names. */
function textLabel(node: TextNode, context: ChipContext): string {
  if (node.match === "structural") return "struct";
  if (node.match === "regex") return "regex";
  if (context.diffSide) return `${context.diffSide} line contains`;
  if (context.mode === "history") return "diff contains";
  if (node.contentOnly) return "content";
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

/** How in:'s values read on its chip: in:strings reads "in quoted text". */
const IN_LABEL: Record<string, string> = { comments: "comments", strings: "quoted text", code: "code only" };

/** How case:'s values read on its chip. */
const CASE_LABEL: Record<string, string> = { yes: "sensitive", no: "insensitive", smart: "smart" };

/** How an operator's value reads on its chip: case:yes reads "sensitive", type:file "file names only". */
function operatorChipValue(node: OperatorNode): string {
  if (node.op === "case") return CASE_LABEL[node.value] ?? node.value;
  if (node.op === "word") return node.value === "yes" ? "whole words" : "parts of words";
  if (node.op === "type") return TYPE_LABEL[node.value] ?? node.value;
  if (node.op === "in") return IN_LABEL[node.value] ?? node.value;
  return node.value;
}

/** One key hint: the key, then what it does. */
const hint = (key: string, what: string) => element("span", {}, element("kbd", {}, key), what);
/** A key hint pushed to the right end of the footer. */
const hintAtEnd = (key: string, what: string) =>
  element("span", { class: "hint-at-end" }, element("kbd", {}, key), what);

/** What the panel shows, which decides the key hints. */
export type FooterMode = "empty" | "errors" | "results" | "operators" | "values" | "replace";

/** The empty box's selected row, which decides its key hints: a pin, and whether ⌥W would start or stop its watch. */
export interface EmptyBoxRow {
  pinned: boolean;
  /** What ⌥W does on it; undefined on a recent query, or a pin that can't be watched. */
  watch?: "start" | "stop";
}

/** The empty box's key hints: only the keys that do something on the selected row. */
function emptyBoxHints(row: EmptyBoxRow | undefined): HTMLElement[] {
  const hints: HTMLElement[] = [];
  if (row) {
    hints.push(
      hint("↑↓", "move"),
      hint("↵", row.pinned ? "run pinned query" : "run recent query"),
      hint("⇧⌫", row.pinned ? "unpin" : "remove"),
    );
  }
  if (row?.watch) hints.push(hint("⌥W", row.watch === "start" ? "watch" : "stop watching"));
  return [...hints, hintAtEnd("?", "opens this sheet anytime")];
}

/** What decides the footer's hints besides its mode: history or files, the empty box's row, and whether Ask is on. */
export interface FooterContext {
  isHistory: boolean;
  emptyRow?: EmptyBoxRow | undefined;
  askEnabled?: boolean;
}

/** Key hints along the bottom, which change with what the panel shows; `emptyRow` is the empty box's selected row. */
export function renderFooter(
  layout: Layout,
  mode: FooterMode,
  { isHistory, emptyRow, askEnabled }: FooterContext,
): void {
  // With Ask on, a question ending in ? is asked in plain words.
  const ask = askEnabled === true ? [hint("…?", "ask in plain words")] : [];
  const hints: Record<FooterMode, HTMLElement[]> = {
    empty: emptyBoxHints(emptyRow),
    errors: [hint("⌘.", "apply first fix"), hint("Tab", "complete operator"), ...ask, hintAtEnd("?", "all operators")],
    operators: [
      hint("↑↓", "pick"),
      hint("Tab", "insert operator"),
      hint("Esc", "dismiss"),
      hintAtEnd("?", "all operators"),
    ],
    values: [hint("↑↓", "pick"), hint("Tab", "insert value"), hint("Esc", "keep as typed")],
    replace: [hint("⌘↵", "replace all"), hintAtEnd("Esc", "close replace")],
    results: [
      hint("↑↓", "move"),
      hint("↵", isHistory ? "open diff" : "open"),
      hint("⌘↵", "open to side"),
      hint("Tab", "complete operator"),
      ...ask,
      hintAtEnd("Esc", "close"),
    ],
  };
  layout.footer.replaceChildren(...hints[mode]);
}
