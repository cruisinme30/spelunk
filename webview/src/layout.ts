// The panel's fixed skeleton, built once. Regions are filled by render/*.
import { element, icon } from "./format";
import { RESULT_LISTS } from "./render/results";
import { createReplaceRow, type ReplaceRow } from "./replace";

/** The panel's regions and controls, from top to bottom. */
export interface Layout {
  shell: HTMLElement;
  input: HTMLInputElement;
  caseButton: HTMLButtonElement;
  wordButton: HTMLButtonElement;
  regexButton: HTMLButtonElement;
  /** ⇄: opens and closes the Replace row. */
  replaceButton: HTMLButtonElement;
  /** Ask: turns the box's question into a query. */
  askButton: HTMLButtonElement;
  replaceRow: ReplaceRow;
  reposButton: HTMLButtonElement;
  repoMenu: HTMLElement;
  settingsButton: HTMLButtonElement;
  statusDot: HTMLElement;
  statusText: HTMLElement;
  completions: HTMLElement;
  banner: HTMLElement;
  /** What was asked in plain words and the query it became, or what Ask needs first. */
  asked: HTMLElement;
  diagnostics: HTMLElement;
  chips: HTMLElement;
  facets: HTMLElement;
  body: HTMLElement;
  footer: HTMLElement;
  /** Unseen, it says what ↑↓ picked where nothing else does: the empty box's queries. */
  announcer: HTMLElement;
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
    "aria-controls": RESULT_LISTS,
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
  return element(
    "button",
    {
      type: "button",
      class: "toggle settings",
      "aria-label": "Search settings",
      title: "Search settings",
      "data-testid": "open-settings",
    },
    icon(GEAR_ICON),
  );
}

function createSearchIcon(): HTMLElement {
  return element("label", { for: "query", class: "query-icon", title: "Search" }, icon(SEARCH_ICON));
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

/** ⇄, which opens and closes the Replace row. */
function createReplaceToggle(): HTMLButtonElement {
  return element(
    "button",
    {
      type: "button",
      class: "toggle replace",
      "aria-label": "Replace",
      "aria-expanded": "false",
      "aria-controls": "replacement",
      "data-testid": "toggle-replace",
      title: "Replace in current files (⌥⌘F)",
    },
    "⇄",
  );
}

/** ✦ Ask: asks the box's question in plain words. */
function createAskButton(): HTMLButtonElement {
  const sparkle =
    '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/><path d="M19 17l.7 2 2 .7-2 .7-.7 2-.7-2-2-.7 2-.7z"/></svg>';
  const button = element(
    "button",
    {
      type: "button",
      class: "toggle ask",
      "data-testid": "ask",
      "aria-label": "Ask in plain words",
      title: "Ask in plain words: type a question, end it with ? or press this, and the query it becomes runs",
    },
    "Ask",
  );
  button.insertAdjacentHTML("afterbegin", sparkle);
  return button;
}

/** The regions under the search bar, in display order. */
function createRegions(): Pick<
  Layout,
  "completions" | "banner" | "asked" | "diagnostics" | "chips" | "facets" | "body" | "footer"
> {
  return {
    completions: element("div", {
      id: "completions",
      role: "listbox",
      "aria-label": "Suggestions",
      "data-testid": "completions",
      hidden: true,
    }),
    banner: element("div", { id: "banner", "data-testid": "banner" }),
    asked: element("div", { id: "asked", "data-testid": "asked" }),
    diagnostics: element("div", { id: "diagnostics", "data-testid": "diagnostics" }),
    chips: element("div", { id: "chips", "data-testid": "chips" }),
    facets: element("div", {
      id: "facets",
      role: "toolbar",
      "aria-label": "Narrow the results",
      "data-testid": "facets",
    }),
    body: element("div", { id: "body" }),
    footer: element("div", { id: "key-hints", "data-testid": "keys" }),
  };
}

/** Builds the skeleton into `root` and returns its parts. */
export function createLayout(root: HTMLElement): Layout {
  const input = createQueryInput();
  const { caseButton, wordButton, regexButton } = createMatchToggles();
  const replaceButton = createReplaceToggle();
  const askButton = createAskButton();
  const replaceRow = createReplaceRow();
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
  const regions = createRegions();
  const announcer = element("div", { class: "visually-hidden", role: "status", "data-testid": "announcer" });
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
      replaceButton,
      askButton,
      element("span", { class: REPO_MENU_ANCHOR_CLASS }, reposButton, repoMenu),
      settingsButton,
    ),
    replaceRow.root,
  );
  const shell = element(
    "section",
    { class: "search-panel", "aria-label": "Spelunk" },
    searchBar,
    element("div", { class: "status", role: "status" }, statusDot, statusText),
    ...Object.values(regions),
    announcer,
  );
  root.append(shell);
  return {
    shell,
    input,
    caseButton,
    wordButton,
    regexButton,
    replaceButton,
    askButton,
    replaceRow,
    reposButton,
    repoMenu,
    settingsButton,
    statusDot,
    statusText,
    ...regions,
    announcer,
  };
}
