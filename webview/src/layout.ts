// The panel's fixed skeleton, built once. Regions are filled by render/*.
import { el } from "./format";

export interface Layout {
  shell: HTMLElement;
  input: HTMLInputElement;
  caseButton: HTMLButtonElement;
  regexButton: HTMLButtonElement;
  reposButton: HTMLButtonElement;
  repoMenu: HTMLElement;
  statusDot: HTMLElement;
  statusText: HTMLElement;
  completions: HTMLElement;
  banner: HTMLElement;
  diagnostics: HTMLElement;
  chips: HTMLElement;
  body: HTMLElement;
  footer: HTMLElement;
}

const SEARCH_ICON =
  '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/></svg>';

export function createLayout(root: HTMLElement): Layout {
  const input = el("input", {
    id: "q",
    type: "text",
    spellcheck: "false",
    autocomplete: "off",
    "aria-label": "Search query",
    placeholder: "Search code, file names and history…",
    "data-testid": "query",
    role: "combobox",
    "aria-expanded": "false",
    "aria-controls": "results",
    "aria-autocomplete": "list",
  });
  const caseButton = el(
    "button",
    {
      type: "button",
      class: "toggle",
      "aria-label": "Match case",
      "aria-pressed": "false",
      "data-testid": "toggle-case",
      title: "Match case (case:yes)",
    },
    "Aa",
  );
  const regexButton = el(
    "button",
    {
      type: "button",
      class: "toggle mono",
      "aria-label": "Regular expression",
      "aria-pressed": "false",
      "data-testid": "toggle-regex",
      title: "Regular expression (/…/)",
    },
    ".*",
  );
  const reposButton = el(
    "button",
    {
      type: "button",
      class: "toggle repos",
      "data-testid": "repos",
      "aria-haspopup": "menu",
      "aria-expanded": "false",
    },
    "All repos",
  );
  const repoMenu = el("div", { class: "menu", role: "menu", "data-testid": "repo-menu", hidden: true });
  const icon = el("span", { class: "icon", "aria-hidden": "true" });
  icon.innerHTML = SEARCH_ICON;

  const statusDot = el("span", { class: "dot", "aria-hidden": "true" });
  const statusText = el("span", { class: "status-text", "data-testid": "index-status" });
  const completions = el("div", {
    id: "completions",
    role: "listbox",
    "aria-label": "Suggestions",
    "data-testid": "completions",
    hidden: true,
  });
  const banner = el("div", { id: "banner", "data-testid": "banner" });
  const diagnostics = el("div", { id: "diag", "data-testid": "diagnostics" });
  const chips = el("div", { id: "chips", "data-testid": "chips" });
  const body = el("div", { id: "body" });
  const footer = el("div", { id: "keys", "data-testid": "keys" });

  const shell = el(
    "section",
    { class: "us", "aria-label": "Unified search" },
    el(
      "div",
      { class: "bar" },
      el("label", { for: "q", class: "qlabel" }, icon),
      input,
      el(
        "div",
        { class: "tools" },
        caseButton,
        regexButton,
        el("span", { class: "menuanchor" }, reposButton, repoMenu),
      ),
    ),
    el("div", { class: "status", role: "status" }, statusDot, statusText),
    completions,
    banner,
    diagnostics,
    chips,
    body,
    footer,
  );
  root.append(shell);
  return {
    shell,
    input,
    caseButton,
    regexButton,
    reposButton,
    repoMenu,
    statusDot,
    statusText,
    completions,
    banner,
    diagnostics,
    chips,
    body,
    footer,
  };
}
