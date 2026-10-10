// Entry point of the welcome page webview (bundled to dist/webview/welcome.js),
// shown once after installing and by Spelunk: Show Welcome. It asks
// for the search shortcut and how much to index, and shows each repo's
// indexing while it runs.
import { button, element, isIndexing, percent, progressBar } from "./format";
import { onHostMessage, send } from "./host";
import type { RepoStatus, WelcomeStateMessage as WelcomeState } from "./protocol.gen";

/** The three shortcut choices, in the order the page lists them. */
type ShortcutChoice = WelcomeState["preset"] | "custom";

/** What each choice's card says; keys are written for the platform. */
interface ShortcutCard {
  choice: ShortcutChoice;
  title: string;
  key?: (keys: Keys) => string;
  recommended?: boolean;
  description: (keys: Keys) => string;
}

/** Key names on this platform. */
interface Keys {
  quickOpen: string;
  movedQuickOpen: string;
  findInFiles: string;
}

const MAC_KEYS: Keys = {
  quickOpen: "⌘P",
  movedQuickOpen: "⌥⌘P",
  findInFiles: "⇧⌘F",
};
const OTHER_KEYS: Keys = {
  quickOpen: "Ctrl+P",
  movedQuickOpen: "Ctrl+Alt+P",
  findInFiles: "Ctrl+Shift+F",
};

const SHORTCUT_CARDS: ShortcutCard[] = [
  {
    choice: "quickOpen",
    title: "Take over",
    key: (keys) => keys.quickOpen,
    recommended: true,
    description: (keys) =>
      `One box for files, code and history, on the key your hands already know. VS Code’s Quick Open moves to ${keys.movedQuickOpen}.`,
  },
  {
    choice: "findInFiles",
    title: "Take over",
    key: (keys) => keys.findInFiles,
    description: () => "Leave Quick Open alone and use the find-in-files key instead.",
  },
  {
    choice: "custom",
    title: "Choose my own",
    description: () => "No key from Spelunk: record any key in Keyboard Shortcuts, filtered to Spelunk.",
  },
];

const HISTORY_DEPTHS: [WelcomeState["historyDepth"], string][] = [
  ["2y", "Last 2 years"],
  ["6m", "Last 6 months"],
  ["all", "Everything"],
];

/** The page's state: the settings it shows and each repo's indexing. */
interface PageState {
  settings: WelcomeState | undefined;
  repos: RepoStatus[];
}

const state: PageState = { settings: undefined, repos: [] };

/** One repo's row: its state, and what it is doing or waiting for. */
function repoRow(repo: RepoStatus, index: number): HTMLElement {
  const before = state.repos[index - 1];
  let word = "Ready";
  let detail: Node | string = state.settings?.symbols ? "Files, symbols and history ready" : "Files and history ready";
  if (repo.tree === "error" || repo.history === "error") {
    word = "Problem";
    detail = repo.message ?? "Indexing failed";
  } else if (repo.tree === "indexing" || repo.history === "indexing") {
    word = "Indexing";
    const percentDone = percent(repo.progress);
    const phase = repo.tree === "indexing" ? "Files" : "History";
    detail = element(
      "span",
      { class: "repo-progress" },
      progressBar(percentDone),
      element("span", { class: "muted" }, `${phase} ${percentDone}%`),
    );
  } else if (isIndexing(repo.tree) || isIndexing(repo.history)) {
    word = "Queued";
    detail = before ? `Starts when ${before.name} finishes` : "Starts in a moment";
  } else if (repo.history === "off") {
    detail = "Files ready; not a Git repository, so no history";
  }
  return element(
    "div",
    { class: `repo-row ${word.toLowerCase()}`, "data-testid": "welcome-repo" },
    element("span", { class: "repo-name" }, repo.name),
    element("span", { class: "repo-detail" }, detail),
    element("span", { class: "repo-state" }, word),
  );
}

function keysFor(settings: WelcomeState): Keys {
  return settings.mac ? MAC_KEYS : OTHER_KEYS;
}

/**
 * One shortcut choice. Picking it saves the preset at once, as ↑↓ through
 * the radio group do; "Choose my own" leaves Spelunk without a key, and only
 * its button opens Keyboard Shortcuts, so arrowing past it doesn't.
 */
function shortcutCard(card: ShortcutCard, keys: Keys, chosen: ShortcutChoice): HTMLElement {
  const titleId = `shortcut-${card.choice}-title`;
  const input = element("input", {
    type: "radio",
    name: "shortcut",
    value: card.choice,
    checked: card.choice === chosen,
    // Named by its title and key alone: the custom card's button has a name of its own.
    "aria-labelledby": titleId,
    "aria-describedby": `shortcut-${card.choice}-description`,
    "data-testid": "shortcut",
  });
  input.addEventListener("change", () => {
    send("welcome.choose", { preset: card.choice === "custom" ? "none" : card.choice });
  });
  const record =
    card.choice === "custom"
      ? button(
          { class: "btn", "data-testid": "record-shortcut" },
          () => {
            input.checked = true;
            send("welcome.shortcut", {});
          },
          "Open Keyboard Shortcuts",
        )
      : null;
  return element(
    "label",
    { class: "choice" },
    input,
    element(
      "span",
      { class: "choice-text" },
      element(
        "span",
        { class: "choice-title", id: titleId },
        element("span", { class: "strong" }, card.title),
        card.key ? element("kbd", {}, card.key(keys)) : null,
        card.recommended ? element("span", { class: "pill" }, "Recommended") : null,
      ),
      element("span", { class: "muted", id: `shortcut-${card.choice}-description` }, card.description(keys)),
      record,
    ),
  );
}

function shortcutSection(settings: WelcomeState): HTMLElement {
  const keys = keysFor(settings);
  const chosen: ShortcutChoice = settings.preset === "none" ? "custom" : settings.preset;
  return element(
    "fieldset",
    { class: "choices" },
    element("legend", {}, "1 · Pick your search shortcut"),
    ...SHORTCUT_CARDS.map((card) => shortcutCard(card, keys, chosen)),
    settings.mac ? null : element("p", { class: "muted" }, "On a Mac these are ⌘P, ⌥⌘P and ⇧⌘F."),
  );
}

function indexSection(settings: WelcomeState): HTMLElement {
  const depth = element("select", {
    id: "history-depth",
    "data-testid": "history-depth",
  });
  for (const [value, label] of HISTORY_DEPTHS) {
    depth.append(element("option", { value, selected: value === settings.historyDepth }, label));
  }
  depth.addEventListener("change", () => {
    const chosen = HISTORY_DEPTHS.find(([value]) => value === depth.value);
    if (chosen) send("welcome.choose", { historyDepth: chosen[0] });
  });
  const symbols = element("input", {
    type: "checkbox",
    checked: settings.symbols,
    "data-testid": "symbols",
  });
  symbols.addEventListener("change", () => {
    send("welcome.choose", { symbols: symbols.checked });
  });
  return element(
    "section",
    { "aria-labelledby": "index-title" },
    element("h2", { id: "index-title" }, "2 · Index your workspace"),
    element(
      "p",
      { class: "muted" },
      "You can search right away. Repos that are still indexing return partial results until they finish.",
    ),
    element("div", { class: "repo-rows" }, ...repoRows()),
    element(
      "div",
      { class: "index-options" },
      element(
        "div",
        { class: "field" },
        element("label", { for: "history-depth" }, "How much history to index"),
        depth,
      ),
      element("label", { class: "check" }, symbols, "Index symbols for ", element("code", {}, "sym:")),
    ),
  );
}

function actions(settings: WelcomeState): HTMLElement {
  const key = SHORTCUT_CARDS.find((card) => card.choice === settings.preset)?.key?.(keysFor(settings)) ?? "";
  const start = button(
    { class: "btn primary", "data-testid": "start" },
    () => {
      send("welcome.start", {});
    },
    "Start searching",
    key ? element("kbd", {}, key) : null,
  );
  const settingsButton = button(
    { class: "btn", "data-testid": "open-settings" },
    () => {
      send("settings.open", {});
    },
    "Open settings",
  );
  const guide = button(
    { class: "btn link", "data-testid": "open-help" },
    () => {
      send("help.open", {});
    },
    "Read the search guide",
  );
  return element("div", { class: "welcome-actions" }, start, settingsButton, guide);
}

/** A row per repo with its indexing progress. */
function repoRows(): HTMLElement[] {
  return state.repos.map((repo, index) => repoRow(repo, index));
}

function render(root: HTMLElement): void {
  const settings = state.settings;
  if (!settings) return;
  root.replaceChildren(
    element(
      "article",
      { class: "welcome" },
      element("h1", {}, "Spelunk is installed"),
      element("p", { class: "lead" }, "Two quick choices and you’re ready. You can change both later in Settings."),
      shortcutSection(settings),
      indexSection(settings),
      actions(settings),
    ),
  );
}

const root = document.querySelector<HTMLElement>("#app");

/**
 * Draws the page again, with the focus back on the control that had it:
 * each choice saves a setting, whose change comes back as welcome.state,
 * and ↑↓ through the shortcuts must go on from the one just picked.
 */
function renderKeepingFocus(into: HTMLElement): void {
  const focused = document.activeElement instanceof HTMLElement ? document.activeElement : undefined;
  const testId = focused?.dataset["testid"];
  const value = focused?.getAttribute("value");
  render(into);
  if (!testId) return;
  const selector = `[data-testid="${CSS.escape(testId)}"]${value === null || value === undefined ? "" : `[value="${CSS.escape(value)}"]`}`;
  into.querySelector<HTMLElement>(selector)?.focus();
}

/**
 * Redraws the repo rows alone, if the page shows them: progress comes often
 * while the page is open, and redrawing it all would take the focus away.
 */
function redrawRepoRows(): boolean {
  const rows = root?.querySelector(".repo-rows");
  rows?.replaceChildren(...repoRows());
  return rows !== undefined && rows !== null;
}
onHostMessage((message) => {
  switch (message.type) {
    case "welcome.state": {
      state.settings = message.payload;
      break;
    }
    case "index.status": {
      state.repos = message.payload.repos;
      if (redrawRepoRows()) return;
      break;
    }
    case "banner":
    case "focus":
    case "parse.result":
    case "preview.result":
    case "replace.done":
    case "replace.plan":
    case "search.batch":
    case "search.done":
    case "state.restore":
    case "watch.state": {
      return; // the search panel's messages
    }
  }
  if (root) renderKeepingFocus(root);
});
send("ready", {});
