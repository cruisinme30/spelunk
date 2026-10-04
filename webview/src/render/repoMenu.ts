// The repo menu behind the "All repos · 3" button: pick one repo to scope the
// search to, or all of them. Picking edits the query's repo: operator, so
// the query text always shows the scope.
import { el } from "../format";
import type { RepoStatus } from "../protocol.gen";

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
  const row = (label: string, detail: string, repoName: string | undefined) => {
    const checked = repoName === selected;
    const item = el(
      "button",
      {
        type: "button",
        class: "menuitem",
        role: "menuitemradio",
        "aria-checked": String(checked),
        "data-testid": "repo-option",
      },
      el("span", { class: "check", "aria-hidden": "true" }, checked ? "✓" : ""),
      el("span", { class: "name" }, label),
      el("span", { class: "muted" }, detail),
    );
    item.addEventListener("click", () => handlers.onPick(repoName));
    return item;
  };
  menu.replaceChildren(
    row("All repos", `${repos.length}`, undefined),
    ...repos.map((repo) => row(repo.name, repo.tree === "ready" ? "" : repo.tree, repo.name)),
  );
}
