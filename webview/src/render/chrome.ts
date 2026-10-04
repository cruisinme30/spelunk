// The panel chrome around the results: index status, health and indexing
// banners, completions, query diagnostics, parsed-query chips and the key
// hints footer.
import { el, plural, termClass } from "../format";
import type { Layout } from "../layout";
import type { BannerMsg, Diagnostic, Fix, IndexState, Mode, Node as QueryNode, RepoStatus } from "../protocol.gen";
import { OP_LABEL, OP_TONE, toneForLabel } from "../sheet";
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
  layout.reposButton.textContent = `All repos · ${repos.length}`;
}

/** Daemon health (failure table) followed by one banner per indexing repo (mock 14). */
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

const COMPLETION_GROUP_TITLE: Record<string, string> = {
  operator: "Operators",
  author: "Authors",
  repo: "Repos",
  lang: "Languages",
  value: "Values",
};

/** The autocomplete list under the box (mocks 5 and 6). */
export function renderCompletions(layout: Layout, state: ViewState, onPick: (index: number) => void): void {
  const { completions, input } = layout;
  const visible = state.completionsOpen && state.completions.length > 0 && document.activeElement === input;
  completions.hidden = !visible;
  input.setAttribute("aria-expanded", String(visible));
  completions.replaceChildren();
  if (!visible) return;
  let group = "";
  state.completions.forEach((completion, index) => {
    if (completion.group !== group) {
      group = completion.group;
      completions.append(el("div", { class: "cgroup", role: "presentation" }, COMPLETION_GROUP_TITLE[group] ?? group));
    }
    const selected = index === state.completionIndex;
    const option = el(
      "div",
      {
        class: selected ? "copt selected" : "copt",
        role: "option",
        id: `c${index}`,
        "aria-selected": String(selected),
        "data-testid": "completion",
      },
      el("code", { class: `tone-${toneForLabel(completion.label)}` }, completion.label),
      el("span", { class: "detail" }, completion.detail),
      selected ? el("kbd", {}, "Tab") : null,
    );
    // mousedown, not click: picking must not move focus out of the query box.
    option.addEventListener("mousedown", (event) => {
      event.preventDefault();
      onPick(index);
    });
    completions.append(option);
  });
}

/** Query errors and warnings with their fix-it buttons (mock 13). */
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

/** The parsed query as chips, with the mode and the result summary (mocks 1-3). */
export function renderChips(layout: Layout, state: ViewState, summary: HTMLElement): void {
  layout.chips.replaceChildren();
  const query = state.parsed;
  if (!state.ui.showParsedQuery || !query?.root || hasErrors(state)) return;
  const modeLabel = query.mode === "history" ? "Commit history" : "Working tree";
  layout.chips.append(
    el("span", { class: "label" }, "Query"),
    ...chipsFor(query.root, query.mode),
    el("span", { class: `mode ${query.mode}`, "data-testid": "mode" }, modeLabel),
    summary,
  );
}

const joiner = (text: string, extraClass = "") => el("span", { class: `joiner ${extraClass}`.trim() }, text);

/** Children's chips separated by AND or OR. */
function joinChips(children: QueryNode[], word: "AND" | "OR", mode: Mode): Node[] {
  return children.flatMap((child, index) => (index ? [joiner(word), ...chipsFor(child, mode)] : chipsFor(child, mode)));
}

function chipsFor(node: QueryNode, mode: Mode): Node[] {
  switch (node.kind) {
    case "and":
      return joinChips(node.children, "AND", mode);
    case "or":
      return [joiner("("), ...joinChips(node.children, "OR", mode), joiner(")")];
    case "not":
      return [joiner("NOT", "not"), ...chipsFor(node.child, mode)];
    case "text": {
      const label = node.match === "regex" ? "regex" : mode === "history" ? "diff contains" : "text";
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
      const value = node.op === "case" ? (node.value === "yes" ? "sensitive" : "insensitive") : node.value;
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

export type FooterMode = "empty" | "errors" | "results";

/** Key hints along the bottom, which change with what the panel shows. */
export function renderFooter(layout: Layout, mode: FooterMode, isHistory: boolean): void {
  const hint = (key: string, what: string) => el("span", {}, el("kbd", {}, key), what);
  const hintAtEnd = (key: string, what: string) => el("span", { class: "push" }, el("kbd", {}, key), what);
  const hints: Record<FooterMode, HTMLElement[]> = {
    empty: [hint("↑↓", "move"), hint("↵", "run recent query"), hintAtEnd("?", "opens this sheet anytime")],
    errors: [hint("⌘.", "apply first fix"), hint("Tab", "complete operator"), hintAtEnd("?", "all operators")],
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
