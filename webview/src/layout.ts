// The panel's fixed skeleton, built once. Regions are filled by render/*.
import { element, icon } from "./format";

/** The panel's regions and controls, from top to bottom. */
export interface Layout {
  shell: HTMLElement;
  input: HTMLInputElement;
  caseButton: HTMLButtonElement;
  wordButton: HTMLButtonElement;
  regexButton: HTMLButtonElement;
  reposButton: HTMLButtonElement;
  repoMenu: HTMLElement;
  settingsButton: HTMLButtonElement;
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

const GEAR_ICON =
  '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/></svg>';

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

/** How one of the Aa, ab and .* buttons beside the box reads. */
interface ToggleLabels {
  text: string;
  label: string;
  title: string;
  testId: string;
  monospace?: boolean;
  /** An extra class for the button, such as "word" for ab's underline. */
  modifier?: string;
}

function createToggle({ text, label, title, testId, monospace, modifier }: ToggleLabels): HTMLButtonElement {
  return element(
    "button",
    {
      type: "button",
      class: ["toggle", monospace ? "mono" : "", modifier ?? ""].filter(Boolean).join(" "),
      "aria-label": label,
      "aria-pressed": "false",
      "data-testid": testId,
      title,
    },
    text,
  );
}

/** The gear at the end of the search bar, which opens Settings filtered to this extension. */
function createSettingsButton(): HTMLButtonElement {
  const button = element("button", {
    type: "button",
    class: "toggle settings",
    "aria-label": "Search settings",
    title: "Search settings",
    "data-testid": "open-settings",
  });
  button.innerHTML = GEAR_ICON;
  return button;
}

function createSearchIcon(): HTMLElement {
  return element("label", { for: "query", class: "query-icon" }, icon(SEARCH_ICON));
}

/** The Aa, ab and .* buttons, which edit the query text. */
function createMatchToggles(): Pick<Layout, "caseButton" | "wordButton" | "regexButton"> {
  const caseButton = createToggle({
    text: "Aa",
    label: "Match case",
    title: "Match case (case:yes)",
    testId: "toggle-case",
  });
  const wordButton = createToggle({
    text: "ab",
    label: "Match whole word",
    title: "Match whole word (word:yes)",
    testId: "toggle-word",
    modifier: "word",
  });
  const regexButton = createToggle({
    text: ".*",
    label: "Regular expression",
    title: "Regular expression (/…/)",
    testId: "toggle-regex",
    monospace: true,
  });
  return { caseButton, wordButton, regexButton };
}

/** Builds the skeleton into `root` and returns its parts. */
export function createLayout(root: HTMLElement): Layout {
  const input = createQueryInput();
  const { caseButton, wordButton, regexButton } = createMatchToggles();
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
  const settingsButton = createSettingsButton();
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
      wordButton,
      regexButton,
      element("span", { class: REPO_MENU_ANCHOR_CLASS }, reposButton, repoMenu),
      settingsButton,
    ),
  );
  const shell = element(
    "section",
    { class: "search-panel", "aria-label": "Spelunk" },
    searchBar,
    element("div", { class: "status", role: "status" }, statusDot, statusText),
    ...Object.values(regions),
  );
  root.append(shell);
  return {
    shell,
    input,
    caseButton,
    wordButton,
    regexButton,
    reposButton,
    repoMenu,
    settingsButton,
    statusDot,
    statusText,
    ...regions,
  };
}
