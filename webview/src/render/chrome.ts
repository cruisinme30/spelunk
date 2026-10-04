// The panel chrome around the results: index status, banners, completions,
// query diagnostics, parsed-query chips and the key hints footer.
import { el, plural } from "../format";
import type { Layout } from "../layout";
import type { Diagnostic, Fix, Node as QueryNode } from "../protocol.gen";
import { OP_LABEL, OP_TONE } from "../sheet";
import { hasErrors, type ViewState } from "../state";

/** The header dot and text: up to date, indexing, or a problem. */
export function renderIndexStatus(layout: Layout, state: ViewState): void {
  const repos = state.repos;
  const isBusy = (s: string) => s === "indexing" || s === "queued";
  const busy = repos.filter((r) => isBusy(r.tree) || isBusy(r.history));
  const failed = repos.filter((r) => r.tree === "error" || r.history === "error");
  layout.statusDot.className = "dot " + (failed.length ? "err" : busy.length ? "busy" : "ok");
  if (failed.length) layout.statusText.textContent = `Index problem in ${failed.map((r) => r.name).join(", ")}`;
  else if (busy.length) layout.statusText.textContent = `Indexing ${busy.length} of ${plural(repos.length, "repo")}`;
  else layout.statusText.textContent = repos.length ? "Index up to date" : "No folders open";
  layout.reposButton.textContent = `All repos · ${repos.length}`;
}

/** Daemon health (failure table) and per-repo indexing progress (mock 14). */
export function renderBanner(layout: Layout, state: ViewState, onRestart: () => void): void {
  layout.banner.replaceChildren();
  const banner = state.banner;
  if (banner) {
    const title =
      banner.state === "restarting"
        ? "Search restarting…"
        : banner.state === "stopped"
          ? "Search stopped"
          : (banner.message ?? "Search unavailable");
    const restart =
      banner.state === "stopped"
        ? el("button", { type: "button", class: "btn", "data-testid": "restart" }, "Restart")
        : null;
    restart?.addEventListener("click", onRestart);
    const detail =
      banner.message && banner.state !== "restarting" && banner.message !== title
        ? el("span", {}, banner.message)
        : null;
    const tone = banner.state === "restarting" ? "warn" : "error";
    layout.banner.append(
      el("div", { class: `banner ${tone}`, role: "alert" }, el("span", { class: "strong" }, title), detail, restart),
    );
  }
  for (const repo of state.repos) {
    const what = repo.tree === "indexing" ? "files" : repo.history === "indexing" ? "history" : "";
    if (!what) continue;
    const percent = Math.round((repo.progress ?? 0) * 100);
    const bar = el(
      "span",
      {
        class: "progress",
        role: "progressbar",
        "aria-valuenow": percent,
        "aria-valuemin": 0,
        "aria-valuemax": 100,
        "aria-label": `${repo.name} ${what} indexing`,
      },
      el("span", { style: `width:${percent}%` }),
    );
    const note = repo.message ?? "Results from this repo may be incomplete. Search keeps working while it finishes.";
    layout.banner.append(
      el(
        "div",
        { class: "banner index", "data-testid": "indexing-banner" },
        el("span", { class: "strong" }, `Indexing ${repo.name} · ${percent}%`),
        bar,
        el("span", {}, note),
      ),
    );
  }
}

const GROUP_TITLE: Record<string, string> = {
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
      completions.append(el("div", { class: "cgroup", role: "presentation" }, GROUP_TITLE[group] ?? group));
    }
    const selected = index === state.completionIndex;
    const tone = OP_TONE[completion.label.replace(/:.*$/, "")] ?? "logic";
    const option = el(
      "div",
      {
        class: "copt" + (selected ? " selected" : ""),
        role: "option",
        id: `c${index}`,
        "aria-selected": String(selected),
        "data-testid": "completion",
      },
      el("code", { class: `tone-${tone}` }, completion.label),
      el("span", { class: "detail" }, completion.detail),
      selected ? el("kbd", {}, "Tab") : null,
    );
    // mousedown, not click: keep focus in the query box.
    option.addEventListener("mousedown", (event) => {
      event.preventDefault();
      onPick(index);
    });
    completions.append(option);
  });
}

/** Query errors and warnings with their fix-it buttons (mock 13). */
export function renderDiagnostics(layout: Layout, state: ViewState, onFix: (fix: Fix) => void): void {
  layout.diagnostics.replaceChildren();
  const raw = state.parsed?.raw ?? "";
  for (const diagnostic of state.parsed?.diagnostics ?? []) {
    layout.diagnostics.append(renderDiagnostic(raw, diagnostic, onFix));
  }
}

function renderDiagnostic(raw: string, diagnostic: Diagnostic, onFix: (fix: Fix) => void): HTMLElement {
  const { start, end } = diagnostic.span;
  const snippet = el("code", { class: "snippet" });
  snippet.append(
    raw.slice(0, start),
    el("span", { class: "bad" }, raw.slice(start, Math.max(end, start + 1)) || " "),
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

/** The parsed query as chips, with the mode and a result summary (mocks 1-3). */
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

function chipsFor(node: QueryNode, mode: "workingTree" | "history"): (Node | string)[] {
  const joiner = (text: string, extra = "") => el("span", { class: `joiner ${extra}`.trim() }, text);
  switch (node.kind) {
    case "and":
      return node.children.flatMap((child, i) =>
        i ? [joiner("AND"), ...chipsFor(child, mode)] : chipsFor(child, mode),
      );
    case "or":
      return [
        joiner("("),
        ...node.children.flatMap((child, i) => (i ? [joiner("OR"), ...chipsFor(child, mode)] : chipsFor(child, mode))),
        joiner(")"),
      ];
    case "not":
      return [joiner("NOT", "not"), ...chipsFor(node.child, mode)];
    case "text": {
      const label = node.match === "regex" ? "regex" : mode === "history" ? "diff contains" : "text";
      return [
        el(
          "span",
          { class: `chip term t${node.termIndex % 4}` },
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
          el("span", { class: "k" }, OP_LABEL[node.op] ?? node.op),
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
  const pushed = (key: string, what: string) => el("span", { class: "push" }, el("kbd", {}, key), what);
  const hints: Record<FooterMode, HTMLElement[]> = {
    empty: [hint("↑↓", "move"), hint("↵", "run recent query"), pushed("?", "opens this sheet anytime")],
    errors: [hint("⌘.", "apply first fix"), hint("Tab", "complete operator"), pushed("?", "all operators")],
    results: [
      hint("↑↓", "move"),
      hint("↵", isHistory ? "open diff" : "open"),
      hint("⌘↵", "open to side"),
      hint("Tab", "complete operator"),
      pushed("Esc", "close"),
    ],
  };
  layout.footer.replaceChildren(...hints[mode]);
}
