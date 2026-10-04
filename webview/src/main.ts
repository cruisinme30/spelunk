// The search panel (Contract 2, webview side). A pure view: it renders what
// the host sends and reports what the user did. Fix-its, completions and
// the Aa / .* toggles are text edits followed by a normal query.changed.
import type {
  BannerMsg,
  Completion,
  Diagnostic,
  Fix,
  HiddenNote,
  HostToWebview,
  Node as QNode,
  ParsedQuery,
  Preview,
  RepoStatus,
  ResultItem,
  SearchDoneMsg,
  UiSettings,
  WebviewToHost,
} from "./protocol.gen";
import { el, highlight, plural, timeAgo } from "./format";
import { applyEdits, isCasePressed, isRegexPressed, textNodes, toggleCase, toggleRegex } from "./query-edit";
import { OP_LABEL, OP_TONE, SHEET } from "./sheet";

interface VsCodeApi {
  postMessage(m: unknown): void;
  getState(): unknown;
  setState(s: unknown): void;
}
declare function acquireVsCodeApi(): VsCodeApi;
const vscode: VsCodeApi = acquireVsCodeApi();

function send<T extends keyof WebviewToHost>(type: T, payload: WebviewToHost[T]): void {
  vscode.postMessage({ v: 1, type, payload });
}

// ---------------------------------------------------------------- state
const state = {
  seq: 0,
  parsed: undefined as ParsedQuery | undefined,
  completions: [] as Completion[],
  compIndex: 0,
  compOpen: false,
  searchId: "",
  items: [] as ResultItem[],
  done: undefined as SearchDoneMsg | undefined,
  lastGoodText: "",
  selected: "",
  preview: undefined as { ref: string; preview: Preview | null; stale?: boolean } | undefined,
  showHiddenFiles: false,
  recent: [] as string[],
  recentIndex: 0,
  ui: { typingDelayMs: 120, openTrigger: "doubleClick", preview: true, showParsedQuery: true, caseSensitive: false } as UiSettings,
  repos: [] as RepoStatus[],
  banner: undefined as BannerMsg | undefined,
  sheet: false,
};

// ---------------------------------------------------------------- skeleton
const input = el("input", {
  id: "q", type: "text", spellcheck: "false", autocomplete: "off", "aria-label": "Search query",
  placeholder: "Search code, file names and history…", "data-testid": "query", role: "combobox",
  "aria-expanded": "false", "aria-controls": "results", "aria-autocomplete": "list",
});
const caseBtn = el("button", { type: "button", class: "toggle", "aria-label": "Match case", "aria-pressed": "false", "data-testid": "toggle-case", title: "Match case (case:yes)" }, "Aa");
const regexBtn = el("button", { type: "button", class: "toggle mono", "aria-label": "Regular expression", "aria-pressed": "false", "data-testid": "toggle-regex", title: "Regular expression (/…/)" }, ".*");
const reposBtn = el("button", { type: "button", class: "toggle repos", "data-testid": "repos" }, "All repos");
const statusDot = el("span", { class: "dot", "aria-hidden": "true" });
const statusText = el("span", { class: "status-text", "data-testid": "index-status" });
const searchIcon = el("span", { class: "icon", "aria-hidden": "true" });
searchIcon.innerHTML = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/></svg>';

const bar = el("div", { class: "bar" }, el("label", { for: "q", class: "qlabel" }, searchIcon), input, el("div", { class: "tools" }, caseBtn, regexBtn, reposBtn));
const statusRow = el("div", { class: "status", role: "status" }, statusDot, statusText);
const banner = el("div", { id: "banner", "data-testid": "banner" });
const diag = el("div", { id: "diag", "data-testid": "diagnostics" });
const completions = el("div", { id: "completions", role: "listbox", "aria-label": "Suggestions", "data-testid": "completions", hidden: true });
const chips = el("div", { id: "chips", "data-testid": "chips" });
const body = el("div", { id: "body" });
const footer = el("div", { id: "keys", "data-testid": "keys" });
const shell = el("section", { class: "us", "aria-label": "Unified search" }, bar, statusRow, completions, banner, diag, chips, body, footer);
document.getElementById("app")!.append(shell);

// Result containers are built once per search and appended to as batches stream.
let resultsEl: HTMLElement;
let previewEl: HTMLElement;
let sections: Record<"file" | "line" | "symbol" | "commit", { root: HTMLElement; list: HTMLElement; count: HTMLElement }>;
let lastCodeGroup: { key: string; count: HTMLElement; n: number } | undefined;
let notesEl: HTMLElement;
let moreEl: HTMLElement;
let headerEl: HTMLElement;

// ---------------------------------------------------------------- query box
let debounce: ReturnType<typeof setTimeout> | undefined;

function queryChanged(immediate = false): void {
  clearTimeout(debounce);
  const fire = () => {
    state.seq++;
    vscode.setState({ text: input.value });
    send("query.changed", { text: input.value, cursor: input.selectionStart ?? input.value.length, seq: state.seq });
  };
  if (immediate || state.ui.typingDelayMs <= 0) fire();
  else debounce = setTimeout(fire, state.ui.typingDelayMs);
}

function setQuery(text: string, cursor = text.length, immediate = true): void {
  input.value = text;
  input.setSelectionRange(cursor, cursor);
  state.compOpen = false;
  renderCompletions();
  queryChanged(immediate);
  if (!text) renderEmpty();
}

function applyFix(fix: Fix): void {
  const r = applyEdits(input.value, fix.edits);
  setQuery(r.text, r.cursor);
  input.focus();
}

input.addEventListener("input", () => {
  state.compOpen = true;
  state.compIndex = 0;
  state.sheet = false;
  queryChanged();
  if (!input.value) renderEmpty();
});

caseBtn.addEventListener("click", () => {
  const r = toggleCase(input.value, state.parsed, state.ui.caseSensitive);
  setQuery(r.text, r.cursor);
});
regexBtn.addEventListener("click", () => {
  const r = toggleRegex(input.value, state.parsed);
  setQuery(r.text, r.cursor);
});
reposBtn.addEventListener("click", () => {
  const sep = input.value && !input.value.endsWith(" ") ? " " : "";
  input.value = input.value + sep + "repo:";
  input.focus();
  input.setSelectionRange(input.value.length, input.value.length);
  state.compOpen = true;
  queryChanged(true);
});

function rows(): HTMLElement[] {
  if (!input.value) return Array.from(body.querySelectorAll<HTMLElement>("[data-recent]"));
  return Array.from(body.querySelectorAll<HTMLElement>("[data-ref]"));
}

input.addEventListener("keydown", (e) => {
  const mod = e.metaKey || e.ctrlKey;
  const compVisible = state.compOpen && state.completions.length > 0;
  if (e.key === "?" && !input.value) {
    e.preventDefault();
    state.sheet = !state.sheet;
    renderEmpty();
    return;
  }
  if (mod && e.key === ".") {
    e.preventDefault();
    const fix = state.parsed?.diagnostics.find((d) => d.fixes.length)?.fixes[0];
    if (fix) applyFix(fix);
    return;
  }
  if (e.key === "Tab" && compVisible) {
    e.preventDefault();
    applyFix(state.completions[state.compIndex].insert);
    state.compOpen = true; // keep suggesting values after an operator
    return;
  }
  if (e.key === "Escape") {
    e.preventDefault();
    if (compVisible) {
      state.compOpen = false;
      renderCompletions();
    } else send("panel.close", {});
    return;
  }
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault();
    const d = e.key === "ArrowDown" ? 1 : -1;
    if (compVisible) {
      state.compIndex = (state.compIndex + d + state.completions.length) % state.completions.length;
      renderCompletions();
      return;
    }
    moveSelection(d);
    return;
  }
  if (e.key === "Enter") {
    e.preventDefault();
    if (compVisible && state.compIndex >= 0 && !mod) {
      applyFix(state.completions[state.compIndex].insert);
      return;
    }
    if (!input.value) {
      const r = rows()[state.recentIndex];
      if (r?.dataset.recent) setQuery(r.dataset.recent);
      return;
    }
    if (state.selected) send("result.open", { ref: state.selected, where: mod ? "side" : "current" });
  }
});

function moveSelection(d: number): void {
  const list = rows();
  if (!list.length) return;
  if (!input.value) {
    state.recentIndex = (state.recentIndex + d + list.length) % list.length;
    list.forEach((r, i) => r.classList.toggle("selected", i === state.recentIndex));
    return;
  }
  const i = list.findIndex((r) => r.dataset.ref === state.selected);
  const next = list[Math.max(0, Math.min(list.length - 1, i < 0 ? 0 : i + d))];
  select(next.dataset.ref!);
}

let selectTimer: ReturnType<typeof setTimeout> | undefined;
function select(ref: string): void {
  if (state.selected === ref) return;
  state.selected = ref;
  for (const r of rows()) {
    const on = r.dataset.ref === ref;
    r.classList.toggle("selected", on);
    r.setAttribute("aria-selected", String(on));
    if (on) {
      r.scrollIntoView({ block: "nearest" });
      input.setAttribute("aria-activedescendant", r.id);
    }
  }
  clearTimeout(selectTimer);
  selectTimer = setTimeout(() => send("result.select", { ref }), 30);
}

body.addEventListener("click", (e) => {
  const row = (e.target as HTMLElement).closest<HTMLElement>("[data-ref]");
  if (row) {
    select(row.dataset.ref!);
    if (state.ui.openTrigger === "singleClick") send("result.open", { ref: row.dataset.ref!, where: e.metaKey || e.ctrlKey ? "side" : "current" });
  }
});
body.addEventListener("dblclick", (e) => {
  const row = (e.target as HTMLElement).closest<HTMLElement>("[data-ref]");
  if (row && state.ui.openTrigger === "doubleClick") send("result.open", { ref: row.dataset.ref!, where: e.metaKey || e.ctrlKey ? "side" : "current" });
});

// ---------------------------------------------------------------- messages
type Msg = { [K in keyof HostToWebview]: { v: 1; type: K; payload: HostToWebview[K] } }[keyof HostToWebview];

window.addEventListener("message", (ev: MessageEvent<Msg>) => {
  const m = ev.data;
  if (!m || m.v !== 1) return;
  switch (m.type) {
    case "state.restore": {
      if (m.payload.settings) state.ui = m.payload.settings;
      state.recent = m.payload.recent;
      if (m.payload.text !== input.value) {
        input.value = m.payload.text;
        input.setSelectionRange(input.value.length, input.value.length);
      }
      if (input.value) queryChanged(true);
      else renderEmpty();
      renderFooter();
      input.focus();
      return;
    }
    case "focus":
      input.focus();
      input.select();
      return;
    case "parse.result":
      if (m.payload.seq !== state.seq) return;
      onParse(m.payload.query, m.payload.completions);
      return;
    case "search.batch":
      if (m.payload.seq !== state.seq && m.payload.searchId !== state.searchId) return;
      if (m.payload.searchId !== state.searchId) startResults(m.payload.searchId);
      appendItems(m.payload.items);
      return;
    case "search.done":
      if (m.payload.seq !== state.seq && m.payload.searchId !== state.searchId) return;
      if (m.payload.searchId !== state.searchId) startResults(m.payload.searchId);
      onDone(m.payload);
      return;
    case "preview.result":
      if (m.payload.ref !== state.selected) return;
      state.preview = m.payload;
      state.showHiddenFiles = false;
      renderPreview();
      if (m.payload.stale) document.getElementById(rowId(m.payload.ref))?.classList.add("stale");
      return;
    case "index.status":
      state.repos = m.payload.repos;
      renderStatus();
      return;
    case "banner":
      state.banner = m.payload.state === "ok" ? undefined : m.payload;
      renderBanner();
      return;
  }
});

function onParse(q: ParsedQuery, comps: Completion[]): void {
  state.parsed = q;
  state.completions = comps;
  if (state.compIndex >= comps.length) state.compIndex = 0;
  const errors = q.diagnostics.some((d) => d.severity === "error");
  if (!errors && q.root) state.lastGoodText = q.raw;
  caseBtn.setAttribute("aria-pressed", String(isCasePressed(q, state.ui.caseSensitive)));
  regexBtn.setAttribute("aria-pressed", String(isRegexPressed(q)));
  shell.classList.toggle("has-errors", errors);
  input.setAttribute("aria-invalid", String(errors));
  renderCompletions();
  renderDiagnostics(q.diagnostics);
  renderChips();
  if (!q.root && !errors) renderEmpty();
  if (headerEl) renderResultsHeader();
  renderFooter();
}

// ---------------------------------------------------------------- render: chrome
function renderStatus(): void {
  const repos = state.repos;
  const busy = repos.filter((r) => r.tree === "indexing" || r.tree === "queued" || r.history === "indexing" || r.history === "queued");
  const failed = repos.filter((r) => r.tree === "error" || r.history === "error");
  statusDot.className = "dot " + (failed.length ? "err" : busy.length ? "busy" : "ok");
  statusText.textContent = failed.length
    ? `Index problem in ${failed.map((r) => r.name).join(", ")}`
    : busy.length
      ? `Indexing ${busy.length} of ${plural(repos.length, "repo")}`
      : repos.length ? "Index up to date" : "No folders open";
  reposBtn.textContent = `All repos · ${repos.length}`;
  renderBanner();
}

function renderBanner(): void {
  banner.replaceChildren();
  const b = state.banner;
  if (b) {
    const restart = b.state === "stopped" ? el("button", { type: "button", class: "btn", "data-testid": "restart" }, "Restart") : null;
    restart?.addEventListener("click", () => send("daemon.restart", {}));
    const text = b.state === "restarting" ? "Search restarting…" : b.state === "stopped" ? "Search stopped" : b.message ?? "Search unavailable";
    banner.append(el("div", { class: `banner ${b.state === "restarting" ? "warn" : "error"}`, role: "alert" }, el("span", { class: "strong" }, text), b.message && b.state !== "restarting" && b.message !== text ? el("span", {}, b.message) : null, restart));
  }
  for (const r of state.repos) {
    const which = r.tree === "indexing" ? "Files" : r.history === "indexing" ? "History" : "";
    if (!which) continue;
    const pct = Math.round((r.progress ?? 0) * 100);
    const barEl = el("span", { class: "progress", role: "progressbar", "aria-valuenow": pct, "aria-valuemin": 0, "aria-valuemax": 100, "aria-label": `${r.name} ${which.toLowerCase()} indexing` }, el("span", { style: `width:${pct}%` }));
    banner.append(el("div", { class: "banner index", "data-testid": "indexing-banner" },
      el("span", { class: "strong" }, `Indexing ${r.name} · ${pct}%`), barEl,
      el("span", {}, r.message ?? "Results from this repo may be incomplete. Search keeps working while it finishes.")));
  }
}

function renderCompletions(): void {
  const show = state.compOpen && state.completions.length > 0 && document.activeElement === input;
  completions.hidden = !show;
  input.setAttribute("aria-expanded", String(show));
  completions.replaceChildren();
  if (!show) return;
  let group = "";
  state.completions.forEach((c, i) => {
    if (c.group !== group) {
      group = c.group;
      completions.append(el("div", { class: "cgroup", role: "presentation" }, { operator: "Operators", author: "Authors", repo: "Repos", lang: "Languages", value: "Values" }[group] ?? group));
    }
    const opt = el("div", { class: "copt" + (i === state.compIndex ? " selected" : ""), role: "option", id: `c${i}`, "aria-selected": String(i === state.compIndex), "data-testid": "completion" },
      el("code", { class: `tone-${OP_TONE[c.label.replace(/:.*$/, "")] ?? "logic"}` }, c.label), el("span", { class: "detail" }, c.detail),
      i === state.compIndex ? el("kbd", {}, "Tab") : null);
    opt.addEventListener("mousedown", (e) => {
      e.preventDefault();
      state.compIndex = i;
      applyFix(c.insert);
    });
    completions.append(opt);
  });
}
input.addEventListener("blur", () => setTimeout(renderCompletions, 0));
input.addEventListener("focus", renderCompletions);

function renderDiagnostics(ds: Diagnostic[]): void {
  diag.replaceChildren();
  for (const d of ds) {
    const raw = state.parsed?.raw ?? "";
    const pre = el("code", { class: "snippet" });
    pre.append(raw.slice(0, d.span.start), el("span", { class: "bad" }, raw.slice(d.span.start, Math.max(d.span.end, d.span.start + 1)) || " "), raw.slice(d.span.end));
    const caret = el("code", { class: "caret" }, " ".repeat(d.span.start) + "^");
    const fixes = d.fixes.map((f, i) => {
      const b = el("button", { type: "button", class: "btn fix", "data-testid": "fix" }, f.title);
      if (i === 0) b.append(el("kbd", {}, "⌘."));
      b.addEventListener("click", () => applyFix(f));
      return b;
    });
    diag.append(el("div", { class: `diag ${d.severity}`, "data-code": d.code },
      el("div", { class: "dmain" }, el("span", { class: "dtitle" }, d.message), el("div", { class: "snippet-wrap" }, pre, el("br"), caret)),
      el("div", { class: "dfixes" }, ...fixes)));
  }
}

const TEXT_LABEL = { workingTree: "text", history: "diff contains" } as const;

function chipFor(n: QNode, negated = false): (Node | string)[] {
  switch (n.kind) {
    case "and":
      return n.children.flatMap((c, i) => (i ? [el("span", { class: "joiner" }, "AND"), ...chipFor(c, negated)] : chipFor(c, negated)));
    case "or":
      return [el("span", { class: "joiner" }, "("), ...n.children.flatMap((c, i) => (i ? [el("span", { class: "joiner" }, "OR"), ...chipFor(c, negated)] : chipFor(c, negated))), el("span", { class: "joiner" }, ")")];
    case "not":
      return [el("span", { class: "joiner not" }, "NOT"), ...chipFor(n.child, !negated)];
    case "text":
      return [el("span", { class: `chip term t${n.termIndex % 4}` }, el("span", { class: "k" }, n.match === "regex" ? "regex" : TEXT_LABEL[state.parsed?.mode ?? "workingTree"]), el("code", {}, n.value))];
    case "op": {
      let value = n.value;
      if (n.op === "case") value = n.value === "yes" ? "sensitive" : "insensitive";
      return [el("span", { class: `chip tone-${OP_TONE[n.op]}` }, el("span", { class: "k" }, OP_LABEL[n.op] ?? n.op), el("code", {}, value), n.resolved ? el("span", { class: "resolved" }, `→ ${n.resolved.label}`) : null)];
    }
  }
}

function renderChips(): void {
  chips.replaceChildren();
  const q = state.parsed;
  if (!state.ui.showParsedQuery || !q?.root || q.diagnostics.some((d) => d.severity === "error")) return;
  chips.append(el("span", { class: "label" }, "Query"), ...chipFor(q.root), el("span", { class: `mode ${q.mode}`, "data-testid": "mode" }, q.mode === "history" ? "Commit history" : "Working tree"));
  headerEl = el("span", { class: "summary", "data-testid": "summary" });
  chips.append(headerEl);
  renderResultsHeader();
}

function renderFooter(): void {
  const k = (key: string, what: string) => el("span", {}, el("kbd", {}, key), what);
  footer.replaceChildren();
  if (!input.value) footer.append(k("↑↓", "move"), k("↵", "run recent query"), el("span", { class: "push" }, el("kbd", {}, "?"), "opens this sheet anytime"));
  else if (state.parsed?.diagnostics.some((d) => d.severity === "error")) footer.append(k("⌘.", "apply first fix"), k("Tab", "complete operator"), el("span", { class: "push" }, el("kbd", {}, "?"), "all operators"));
  else footer.append(k("↑↓", "move"), k("↵", state.parsed?.mode === "history" ? "open diff" : "open"), k("⌘↵", "open to side"), k("Tab", "complete operator"), el("span", { class: "push" }, el("kbd", {}, "Esc"), "close"));
}

// ---------------------------------------------------------------- render: empty state (mock 4)
function renderEmpty(): void {
  state.searchId = "";
  state.items = [];
  state.selected = "";
  state.preview = undefined;
  body.replaceChildren();
  chips.replaceChildren();
  diag.replaceChildren();
  const recent = el("div", { class: "recent" }, el("div", { class: "section-title" }, "Recent"));
  state.recent.forEach((r, i) => {
    const b = el("button", { type: "button", class: "recent-row" + (i === state.recentIndex ? " selected" : ""), "data-recent": r, "data-testid": "recent" }, el("code", {}, r));
    b.addEventListener("click", () => setQuery(r));
    recent.append(b);
  });
  if (!state.recent.length) recent.append(el("p", { class: "muted pad" }, "Queries you run show up here."));
  recent.append(el("div", { class: "card" }, el("span", { class: "section-title" }, "Combine freely"), el("code", {}, "author:jane (timeout OR retry) -f:vendor/ since:6m"), el("span", { class: "muted" }, "Spaces mean AND. AND binds tighter than OR.")));
  const sheet = el("div", { class: "sheet", "data-testid": "sheet" }, el("div", { class: "section-title split" }, el("span", {}, "Operators"), el("span", { class: "muted" }, "Click one to insert it")));
  const groups = el("div", { class: "groups" });
  for (const g of SHEET) {
    const card = el("div", { class: "card" }, el("span", { class: `gname tone-${g.tone}` }, g.name));
    for (const it of g.items) {
      const b = el("button", { type: "button", class: "op", "data-testid": "sheet-op" }, el("code", { class: `tone-${g.tone}` }, it.label), el("span", { class: "muted" }, it.detail));
      b.addEventListener("click", () => insertAtCursor(it.insert));
      card.append(b);
    }
    groups.append(card);
  }
  sheet.append(groups);
  body.append(el("div", { class: "empty" + (state.sheet ? " sheet-open" : "") }, recent, sheet));
  renderFooter();
}

function insertAtCursor(snippet: string): void {
  const pos = input.selectionStart ?? input.value.length;
  const before = input.value.slice(0, pos);
  const pad = before && !before.endsWith(" ") && !snippet.startsWith(" ") ? " " : "";
  const text = before + pad + snippet + input.value.slice(pos);
  // Paired snippets ("", //, ()) put the cursor inside.
  const inner = /^(""|\/\/|\(\))$/.test(snippet) ? 1 : 0;
  const cursor = before.length + pad.length + snippet.length - inner;
  input.value = text;
  input.focus();
  input.setSelectionRange(cursor, cursor);
  state.compOpen = true;
  state.sheet = false;
  queryChanged(true);
}

// ---------------------------------------------------------------- render: results
const repoName = (id: string) => state.repos.find((r) => r.repoId === id)?.name ?? id;
const rowId = (ref: string) => "r-" + ref.replace(/[^A-Za-z0-9_-]/g, "_");

function startResults(searchId: string): void {
  const append = searchId === state.searchId;
  if (append) return;
  state.searchId = searchId;
  state.items = [];
  state.done = undefined;
  state.selected = "";
  state.preview = undefined;
  lastCodeGroup = undefined;
  const mk = (title: string, testid: string) => {
    const count = el("span", { class: "muted" });
    const list = el("div", { class: "list" });
    const root = el("div", { class: "rsection", "data-testid": testid, hidden: true }, el("div", { class: "section-title split" }, el("span", {}, title), count), list);
    return { root, list, count };
  };
  sections = {
    file: mk("File names", "section-files"),
    line: mk("Code", "section-code"),
    symbol: mk("Definitions", "section-symbols"),
    commit: mk("Commits · newest first", "section-commits"),
  };
  notesEl = el("div", { class: "notes", "data-testid": "hidden-notes" });
  moreEl = el("div", { class: "more" });
  resultsEl = el("div", { id: "results", role: "listbox", "aria-label": "Results", "data-testid": "results" },
    sections.file.root, sections.symbol.root, sections.line.root, sections.commit.root, notesEl, moreEl);
  previewEl = el("div", { id: "preview", "data-testid": "preview", "aria-live": "polite" });
  body.replaceChildren(el("div", { class: "split" }, resultsEl, previewEl));
  if (state.lastGoodText && state.parsed?.diagnostics.some((d) => d.severity === "error")) {
    resultsEl.prepend(el("div", { class: "lastgood muted" }, "Showing results for the last query that worked ", el("code", {}, state.lastGoodText)));
  }
}

function appendItems(items: ResultItem[]): void {
  if (!items.length) return;
  const firstNew = state.items.length === 0;
  state.items.push(...items);
  for (const it of items) {
    const s = sections[it.kind];
    s.root.hidden = false;
    s.list.append(renderItem(it));
  }
  updateSectionCounts();
  renderResultsHeader();
  if (firstNew && !state.selected) {
    const first = rows()[0];
    if (first) select(first.dataset.ref!);
  }
}

function renderItem(it: ResultItem): HTMLElement {
  const base = { id: rowId(it.ref), role: "option", "aria-selected": "false", "data-ref": it.ref, "data-kind": it.kind, "data-testid": "result" };
  switch (it.kind) {
    case "file":
      return el("div", { ...base, class: "row file", title: it.path },
        el("span", { class: "path" }, highlight(it.path, it.nameHits)),
        it.dirty ? el("span", { class: "badge warn" }, "Uncommitted changes") : null,
        it.lastCommit ? el("span", { class: "meta" }, `${it.lastCommit.author} · ${timeAgo(it.lastCommit.at)}`) : null,
        el("span", { class: "repo" }, repoName(it.repoId)));
    case "line": {
      const key = `${it.repoId}:${it.path}`;
      const frag = document.createDocumentFragment();
      if (!lastCodeGroup || lastCodeGroup.key !== key) {
        const count = el("span", { class: "count" });
        frag.append(el("div", { class: "group", "data-testid": "code-group" }, el("span", { class: "path" }, it.path), el("span", { class: "repo" }, repoName(it.repoId)), count));
        lastCodeGroup = { key, count, n: 0 };
      }
      lastCodeGroup.n++;
      lastCodeGroup.count.textContent = String(lastCodeGroup.n);
      const row = el("div", { ...base, class: "row line" }, el("span", { class: "ln" }, String(it.line)), el("code", { class: "text" }, highlight(it.text, it.hits)));
      frag.append(row);
      const wrap = el("div", { class: "contents" });
      wrap.append(frag);
      return wrap;
    }
    case "symbol":
      return el("div", { ...base, class: "row symbol" },
        el("span", { class: `badge kind-${it.symbolKind}` }, it.symbolKind),
        el("code", { class: "name" }, highlight(it.name, it.hits)),
        el("span", { class: "path muted" }, `${it.path}:${it.line}`),
        el("span", { class: "repo" }, repoName(it.repoId)));
    case "commit": {
      const terms = textNodes(state.parsed);
      const tags = it.matchedTerms.map((ti) => {
        const t = terms.find((n) => n.termIndex === ti);
        return t ? el("span", { class: `tag t${ti % 4}` }, t.value) : null;
      });
      const files = it.files.slice(0, 2).map((f) => el("span", { class: "file" }, f.path, " ", el("span", { class: "add" }, `+${f.added}`), " ", el("span", { class: "del" }, `−${f.removed}`)));
      if (it.files.length > 2) files.push(el("span", { class: "muted" }, `+${it.files.length - 2} more`));
      return el("div", { ...base, class: "row commit" },
        el("div", { class: "line1" }, el("span", { class: "subject" }, highlight(it.subject, it.subjectHits)), el("code", { class: "sha" }, it.sha.slice(0, 7))),
        el("div", { class: "line2 muted" }, el("span", {}, it.author.name), el("span", {}, timeAgo(it.at)), el("span", {}, repoName(it.repoId)), it.diffHits ? el("span", {}, plural(it.diffHits, "hit") + " in diff") : null, ...tags),
        el("div", { class: "line3" }, ...files));
    }
  }
}

function updateSectionCounts(): void {
  const by = countBy();
  sections.file.count.textContent = String(by.file);
  sections.symbol.count.textContent = String(by.symbol);
  sections.line.count.textContent = by.line ? `${by.line} in ${plural(by.codeFiles, "file")}` : "";
  sections.commit.count.textContent = String(by.commit);
}

function countBy() {
  const c = { file: 0, line: 0, symbol: 0, commit: 0, codeFiles: 0, repos: 0 };
  const files = new Set<string>();
  const repos = new Set<string>();
  for (const it of state.items) {
    c[it.kind]++;
    repos.add(it.repoId);
    if (it.kind === "line") files.add(it.repoId + it.path);
  }
  c.codeFiles = files.size;
  c.repos = repos.size;
  return c;
}

function renderResultsHeader(): void {
  if (!headerEl) return;
  const c = countBy();
  const parts: string[] = [];
  if (state.parsed?.mode === "history") parts.push(`${plural(c.commit, "commit")} in ${plural(c.repos, "repo")}`);
  else {
    if (c.file) parts.push(plural(c.file, "file name"));
    if (c.symbol) parts.push(plural(c.symbol, "definition"));
    if (c.line) parts.push(plural(c.line, "code match", "code matches"));
  }
  if (state.done && !state.items.length) parts.push("0 results");
  if (state.done?.truncated) parts.push("truncated");
  headerEl.textContent = parts.join(" · ");
}

const HIDDEN_LABEL: Record<HiddenNote["reason"], string> = { not: "-", since: "since:", case: "case:", type: "type:", pathFilter: "f:" };

function onDone(d: SearchDoneMsg): void {
  state.done = d;
  renderResultsHeader();
  notesEl.replaceChildren();
  for (const h of d.hidden) {
    if (!h.count) continue;
    const b = el("button", { type: "button", class: "btn link", "data-testid": "show-hidden" }, "Show them");
    b.addEventListener("click", () => applyFix(h.undo));
    notesEl.append(el("div", { class: "note", "data-reason": h.reason }, el("span", {}, `${plural(h.count, h.unit.replace(/(e)?s$/, ""), h.unit)} hidden by `, el("code", {}, h.undo.title.replace(/^Remove /, "") || HIDDEN_LABEL[h.reason])), b));
  }
  moreEl.replaceChildren();
  if (d.nextCursor) {
    const b = el("button", { type: "button", class: "btn", "data-testid": "load-more" }, "Load more");
    b.addEventListener("click", () => send("results.more", { searchId: d.searchId, cursor: d.nextCursor! }));
    moreEl.append(b);
  }
  if (d.error) {
    previewEl.replaceChildren(el("div", { class: "notice error" }, d.error));
  } else if (!state.items.length) {
    previewEl.replaceChildren();
    resultsEl.querySelector(".none")?.remove();
    resultsEl.prepend(el("div", { class: "none", "data-testid": "no-results" }, el("span", { class: "strong" }, "No results"), el("span", { class: "muted" }, d.hidden.some((h) => h.count) ? "Some results are hidden by filters below." : "Try fewer terms, or check the operators with ?")));
  }
  updateSectionCounts();
}

// ---------------------------------------------------------------- render: preview
function renderPreview(): void {
  if (!previewEl) return;
  const p = state.preview;
  previewEl.replaceChildren();
  if (!p) return;
  if (!p.preview) {
    previewEl.append(el("div", { class: "notice" }, p.stale ? "No longer exists" : "No preview available"));
    return;
  }
  const pv = p.preview;
  const openBtns = el("div", { class: "actions" },
    el("button", { type: "button", class: "btn", "data-act": "open" }, pv.kind === "commit" ? "Open diff ↵" : "Open ↵"),
    el("button", { type: "button", class: "btn", "data-act": "side" }, "Open to side ⌘↵"));
  openBtns.addEventListener("click", (e) => {
    const act = (e.target as HTMLElement).closest<HTMLElement>("[data-act]")?.dataset.act;
    if (act) send("result.open", { ref: p.ref, where: act === "side" ? "side" : "current" });
  });
  if (pv.kind === "file") {
    const item = state.items.find((i) => i.ref === p.ref);
    const repo = item ? repoName(item.repoId) : "";
    previewEl.append(el("div", { class: "phead" }, el("span", { class: "ptitle" }, el("span", { class: "muted" }, repo ? `${repo} / ` : ""), pv.path), el("span", { class: "muted" }, `line ${pv.focusLine}`), openBtns));
    const code = el("div", { class: "code" });
    const hitsByLine = new Map(pv.hits.map((h) => [h.line, h.ranges]));
    const dirty = new Set(pv.dirtyLines);
    pv.lines.forEach((text, i) => {
      const n = pv.firstLine + i;
      const row = el("div", { class: "cl" + (n === pv.focusLine ? " focus" : "") + (dirty.has(n) ? " dirty" : "") }, el("span", { class: "ln" }, String(n)), el("code", {}, highlight(text, hitsByLine.get(n) ?? [])));
      code.append(row);
    });
    previewEl.append(code);
    code.querySelector(".focus")?.scrollIntoView({ block: "center" });
    if (pv.symbols?.length) {
      previewEl.append(el("div", { class: "outline muted" }, "In this file: ", ...pv.symbols.slice(0, 8).map((s) => el("code", {}, s.name))));
    }
    return;
  }
  const item = state.items.find((i) => i.ref === p.ref);
  previewEl.append(el("div", { class: "phead commit" },
    el("div", { class: "line1" }, el("span", { class: "ptitle" }, pv.subject), el("code", { class: "sha" }, pv.sha.slice(0, 7))),
    el("div", { class: "muted" }, `${pv.author} committed ${timeAgo(pv.at)}${item ? " · " + repoName(item.repoId) : ""}`), openBtns));
  if (pv.body.trim()) previewEl.append(el("pre", { class: "body" }, pv.body.trim()));
  const hiddenFiles = pv.files.filter((f) => f.hiddenByFilter);
  const shownFiles = pv.files.filter((f) => !f.hiddenByFilter || state.showHiddenFiles);
  const files = el("div", { class: "files" }, ...shownFiles.map((f) => el("span", { class: "file" }, f.path, " ", el("span", { class: "add" }, `+${f.added}`), " ", el("span", { class: "del" }, `−${f.removed}`))));
  if (hiddenFiles.length && !state.showHiddenFiles) {
    const b = el("button", { type: "button", class: "btn link", "data-testid": "show-all-files" }, "Show all");
    b.addEventListener("click", () => {
      state.showHiddenFiles = true;
      renderPreview();
    });
    files.append(el("span", { class: "muted" }, `${plural(hiddenFiles.length, "other changed file")} hidden by f:`), b);
  }
  previewEl.append(files);
  const hidden = new Set(hiddenFiles.map((f) => f.path));
  const code = el("div", { class: "code diff" });
  for (const h of pv.hunks) {
    if (hidden.has(h.path) && !state.showHiddenFiles) continue;
    code.append(el("div", { class: "hunk" }, el("span", { class: "muted" }, `${h.path}  ${h.header}`)));
    for (const l of h.lines) {
      const sign = l.kind === "add" ? "+" : l.kind === "del" ? "−" : " ";
      code.append(el("div", { class: `cl ${l.kind}` }, el("span", { class: "ln" }, String(l.kind === "del" ? l.oldNo ?? "" : l.newNo ?? "")), el("span", { class: "sign" }, sign), el("code", {}, highlight(l.text, l.hits))));
    }
  }
  previewEl.append(code);
}

// ---------------------------------------------------------------- boot
renderStatus();
renderEmpty();
const saved = vscode.getState() as { text?: string } | undefined;
if (saved?.text) input.value = saved.text;
send("ready", {});
input.focus();
