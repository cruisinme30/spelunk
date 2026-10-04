// The repo menu behind the "All repos · 3" button: pick one repo to scope the
// search to, or all of them. Picking edits the query's repo: operator, so
// the query text always shows the scope.
import { element } from "../format";
import type { RepoStatus } from "../protocol.gen";

/** What picking a row of the repo menu does. */
export interface RepoMenuHandlers {
  /** A repo name, or undefined for all repos. */
  onPick(repoName: string | undefined): void;
}

/** Renders the menu's rows into `menu`; `selected` is the scoped repo's name. */
export function renderRepoMenu(
  menu: HTMLElement,
  repos: RepoStatus[],
  selected: string | undefined,
  handlers: RepoMenuHandlers,
): void {
  const row = (label: string, detail: string, repoName?: string) => {
    const checked = repoName === selected;
    const item = element(
      "button",
      {
        type: "button",
        class: "menuitem",
        role: "menuitemradio",
        "aria-checked": String(checked),
        "data-testid": "repo-option",
      },
      element("span", { class: "check", "aria-hidden": "true" }, checked ? "✓" : ""),
      element("span", { class: "name" }, label),
      element("span", { class: "muted" }, detail),
    );
    item.addEventListener("click", () => {
      handlers.onPick(repoName);
    });
    return item;
  };
  menu.replaceChildren(
    row("All repos", `${repos.length}`),
    ...repos.map((repo) => row(repo.name, repo.tree === "ready" ? "" : repo.tree, repo.name)),
  );
}
