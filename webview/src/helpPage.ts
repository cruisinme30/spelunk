// Entry point of the help page webview (bundled to dist/webview/help.js):
// the search guide. Every example has a Try button that runs it in
// the search panel.
import { element } from "./format";
import { BASICS, COMBINING, EXAMPLES, KEYS } from "./helpContent";
import { send } from "./host";
import { OPERATOR_GROUPS, type OperatorGroup } from "./operators";

const SECTIONS: [id: string, title: string][] = [
  ["basics", "Basics"],
  ["operators", "Operators"],
  ["combining", "Combining"],
  ["examples", "Examples"],
  ["keys", "Keyboard and mouse"],
  ["settings", "Settings"],
];

/** Turns `code` spans in a sentence into <code> elements. */
function prose(text: string): Node[] {
  return text
    .split(/`([^`]+)`/)
    .map((part, index) => (index % 2 ? element("code", {}, part) : document.createTextNode(part)));
}

function bulletList(lines: string[]): HTMLElement {
  return element("ul", {}, ...lines.map((line) => element("li", {}, ...prose(line))));
}

function tryButton(query: string): HTMLElement {
  const button = element("button", { type: "button", class: "btn", "data-testid": "try", "data-query": query }, "Try");
  button.addEventListener("click", () => {
    send("help.try", { query });
  });
  return button;
}

function operatorTable(groups: OperatorGroup[]): HTMLElement {
  const body = element("tbody");
  for (const group of groups) {
    const tone = `tone-${group.tone}`;
    body.append(
      element(
        "tr",
        { class: "operator-group" },
        element("th", { colspan: 4 }, element("span", { class: tone }, group.name)),
      ),
    );
    for (const entry of group.entries) {
      body.append(
        element(
          "tr",
          { "data-testid": "operator-row" },
          element("td", {}, element("code", { class: tone }, entry.label)),
          element("td", {}, entry.description),
          element("td", {}, element("code", {}, entry.example)),
          element("td", {}, tryButton(entry.example)),
        ),
      );
    }
  }
  const headings = ["Operator", "What it does", "Example", ""].map((heading) => element("th", {}, heading));
  return element("table", { class: "operator-table" }, element("thead", {}, element("tr", {}, ...headings)), body);
}

function section(id: string, title: string, ...children: (Node | null)[]): HTMLElement {
  return element(
    "section",
    { id, "aria-labelledby": `${id}-title` },
    element("h2", { id: `${id}-title` }, title),
    ...children,
  );
}

function tableOfContents(): HTMLElement {
  return element(
    "nav",
    { class: "toc", "aria-label": "On this page" },
    element("div", { class: "section-title" }, "On this page"),
    ...SECTIONS.map(([id, title]) => element("a", { href: `#${id}` }, title)),
  );
}

function operatorsSection(): HTMLElement {
  return section(
    "operators",
    "Operators",
    operatorTable(OPERATOR_GROUPS),
    element(
      "p",
      { class: "muted" },
      ...prose("Time windows for `since:` use a number and a unit: d days, w weeks, m months, y years."),
    ),
  );
}

function examplesSection(): HTMLElement {
  const examples = EXAMPLES.map((example) =>
    element(
      "div",
      { class: "example", "data-testid": "example" },
      element("span", {}, example.title),
      element("code", {}, example.query),
      tryButton(example.query),
    ),
  );
  return section("examples", "Examples", element("div", { class: "examples" }, ...examples));
}

function keysSection(): HTMLElement {
  const rows = KEYS.map(([what, keys]) =>
    element("tr", {}, element("td", {}, what), element("td", {}, element("kbd", {}, keys))),
  );
  return section("keys", "Keyboard and mouse", element("table", { class: "key-table" }, element("tbody", {}, ...rows)));
}

function settingsSection(): HTMLElement {
  const openSettings = element(
    "button",
    { type: "button", class: "btn", "data-testid": "open-settings" },
    "Open search settings",
  );
  openSettings.addEventListener("click", () => {
    send("settings.open", {});
  });
  return section(
    "settings",
    "Settings",
    element(
      "p",
      {},
      "Every option lives in VS Code’s Settings under Unified Search, so it syncs with your other settings and can be set per workspace.",
    ),
    openSettings,
  );
}

function render(root: HTMLElement): void {
  const article = element(
    "article",
    { class: "guide" },
    element("h1", {}, "Search guide"),
    element(
      "p",
      { class: "lead" },
      "One box searches file names, code and Git history across every repo in your workspace. Type words to search, then add operators to narrow it down.",
    ),
    section("basics", "Basics", bulletList(BASICS)),
    operatorsSection(),
    section("combining", "Combining", bulletList(COMBINING)),
    examplesSection(),
    keysSection(),
    settingsSection(),
  );
  root.replaceChildren(element("div", { class: "help" }, tableOfContents(), article));
}

const root = document.querySelector<HTMLElement>("#app");
if (root) render(root);
