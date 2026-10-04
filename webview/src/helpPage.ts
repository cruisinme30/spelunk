// Entry point of the help page webview (bundled to dist/webview/help.js):
// the search guide (mock 17). Every example has a Try button that runs it in
// the search panel.
import { el } from "./format";
import { BASICS, COMBINING, EXAMPLES, KEYS, OPERATOR_GROUPS, type OperatorGroup } from "./helpContent";
import { send } from "./host";

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
    .map((part, index) => (index % 2 ? el("code", {}, part) : document.createTextNode(part)));
}

function tryButton(query: string): HTMLElement {
  const button = el("button", { type: "button", class: "btn", "data-testid": "try", "data-query": query }, "Try");
  button.addEventListener("click", () => send("help.try", { query }));
  return button;
}

function operatorTable(groups: OperatorGroup[]): HTMLElement {
  const body = el("tbody");
  for (const group of groups) {
    body.append(
      el(
        "tr",
        { class: "opgroup" },
        el("th", { colspan: 4 }, el("span", { class: `group-name ${group.tone}` }, group.name)),
      ),
    );
    for (const row of group.rows) {
      body.append(
        el(
          "tr",
          { "data-testid": "operator-row" },
          el("td", {}, el("code", { class: `group-name ${group.tone}` }, row.operator)),
          el("td", {}, row.what),
          el("td", {}, el("code", {}, row.example)),
          el("td", {}, tryButton(row.example)),
        ),
      );
    }
  }
  return el(
    "table",
    { class: "optable" },
    el(
      "thead",
      {},
      el("tr", {}, el("th", {}, "Operator"), el("th", {}, "What it does"), el("th", {}, "Example"), el("th", {}, "")),
    ),
    body,
  );
}

function section(id: string, title: string, ...children: (Node | null)[]): HTMLElement {
  return el("section", { id, "aria-labelledby": `${id}-title` }, el("h2", { id: `${id}-title` }, title), ...children);
}

function render(root: HTMLElement): void {
  const settingsLink = el(
    "button",
    { type: "button", class: "btn", "data-testid": "open-settings" },
    "Open search settings",
  );
  settingsLink.addEventListener("click", () => send("settings.open", {}));
  const nav = el(
    "nav",
    { class: "toc", "aria-label": "On this page" },
    el("div", { class: "section-title" }, "On this page"),
    ...SECTIONS.map(([id, title]) => el("a", { href: `#${id}` }, title)),
  );
  const article = el(
    "article",
    { class: "guide" },
    el("h1", {}, "Search guide"),
    el(
      "p",
      { class: "lead" },
      "One box searches file names, code and Git history across every repo in your workspace. Type words to search, then add operators to narrow it down.",
    ),
    section("basics", "Basics", el("ul", {}, ...BASICS.map((line) => el("li", {}, ...prose(line))))),
    section(
      "operators",
      "Operators",
      operatorTable(OPERATOR_GROUPS),
      el(
        "p",
        { class: "muted" },
        ...prose("Time windows for `since:` use a number and a unit: d days, w weeks, m months, y years."),
      ),
    ),
    section("combining", "Combining", el("ul", {}, ...COMBINING.map((line) => el("li", {}, ...prose(line))))),
    section(
      "examples",
      "Examples",
      el(
        "div",
        { class: "examples" },
        ...EXAMPLES.map((example) =>
          el(
            "div",
            { class: "example", "data-testid": "example" },
            el("span", {}, example.title),
            el("code", {}, example.query),
            tryButton(example.query),
          ),
        ),
      ),
    ),
    section(
      "keys",
      "Keyboard and mouse",
      el(
        "table",
        { class: "keytable" },
        el(
          "tbody",
          {},
          ...KEYS.map(([what, keys]) => el("tr", {}, el("td", {}, what), el("td", {}, el("kbd", {}, keys)))),
        ),
      ),
    ),
    section(
      "settings",
      "Settings",
      el(
        "p",
        {},
        "Every option lives in VS Code’s Settings under Unified Search, so it syncs with your other settings and can be set per workspace.",
      ),
      settingsLink,
    ),
  );
  root.replaceChildren(el("div", { class: "help" }, nav, article));
}

const root = document.getElementById("app");
if (root) render(root);
