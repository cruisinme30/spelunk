// The panel's fixed skeleton, built once. Regions are filled by render/*.
import { element } from "./format";

/** The panel's regions and controls, from top to bottom. */
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

/** The class of the element that holds the repos button and its menu; a click outside it closes the menu. */
export const REPO_MENU_ANCHOR_CLASS = "repo-menu-anchor";

const SEARCH_ICON =
  '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/></svg>';

function createQueryInput(): HTMLInputElement {
  return element("input", {
    id: "query",
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
}

/** How one of the Aa and .* buttons beside the box reads. */
interface ToggleLabels {
  text: string;
  label: string;
  title: string;
  testId: string;
  monospace?: boolean;
}

function createToggle({ text, label, title, testId, monospace }: ToggleLabels): HTMLButtonElement {
  return element(
    "button",
    {
      type: "button",
      class: monospace ? "toggle mono" : "toggle",
      "aria-label": label,
      "aria-pressed": "false",
      "data-testid": testId,
      title,
    },
    text,
  );
}

function createSearchIcon(): HTMLElement {
  const icon = element("span", { class: "icon", "aria-hidden": "true" });
  icon.innerHTML = SEARCH_ICON;
  return element("label", { for: "query", class: "query-icon" }, icon);
}

/** Builds the skeleton into `root` and returns its parts. */
export function createLayout(root: HTMLElement): Layout {
  const input = createQueryInput();
  const caseButton = createToggle({
    text: "Aa",
    label: "Match case",
    title: "Match case (case:yes)",
    testId: "toggle-case",
  });
  const regexButton = createToggle({
    text: ".*",
    label: "Regular expression",
    title: "Regular expression (/…/)",
    testId: "toggle-regex",
    monospace: true,
  });
  const reposButton = element(
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
  const repoMenu = element("div", { class: "menu", role: "menu", "data-testid": "repo-menu", hidden: true });
  const statusDot = element("span", { class: "status-dot", "aria-hidden": "true" });
  const statusText = element("span", { class: "status-text", "data-testid": "index-status" });
  // The regions under the search bar, in display order.
  const regions = {
    completions: element("div", {
      id: "completions",
      role: "listbox",
      "aria-label": "Suggestions",
      "data-testid": "completions",
      hidden: true,
    }),
    banner: element("div", { id: "banner", "data-testid": "banner" }),
    diagnostics: element("div", { id: "diagnostics", "data-testid": "diagnostics" }),
    chips: element("div", { id: "chips", "data-testid": "chips" }),
    body: element("div", { id: "body" }),
    footer: element("div", { id: "key-hints", "data-testid": "keys" }),
  };
  const searchBar = element(
    "div",
    { class: "search-bar" },
    createSearchIcon(),
    input,
    element(
      "div",
      { class: "tools" },
      caseButton,
      regexButton,
      element("span", { class: REPO_MENU_ANCHOR_CLASS }, reposButton, repoMenu),
    ),
  );
  const shell = element(
    "section",
    { class: "search-panel", "aria-label": "Unified search" },
    searchBar,
    element("div", { class: "status", role: "status" }, statusDot, statusText),
    ...Object.values(regions),
  );
  root.append(shell);
  return { shell, input, caseButton, regexButton, reposButton, repoMenu, statusDot, statusText, ...regions };
}
