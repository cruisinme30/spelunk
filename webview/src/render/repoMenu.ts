// The repo menu behind the "All repos · 3" button: pick one repo to scope the
// search to, or all of them. Picking edits the query's repo: operator, so
// the query text always shows the scope.
import { element } from "../format";
import { type Layout, REPO_MENU_ANCHOR_CLASS } from "../layout";
import type { RepoStatus } from "../protocol.gen";

/** What picking a row of the repo menu does. */
interface RepoMenuHandlers {
  /** A repo name, or undefined for all repos. */
  onPick(repoName: string | undefined): void;
}

/** Renders the menu's rows into `menu`; `selected` is the scoped repo's name. */
function renderRepoMenu(
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
        tabindex: "-1", // ↑↓ move through the rows; Tab leaves the menu
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

/** What the repo menu reads when it opens, and does with a pick. */
interface RepoMenuHooks extends RepoMenuHandlers {
  repos(): RepoStatus[];
  /** The scoped repo's name, undefined for all repos. */
  selected(): string | undefined;
}

/**
 * The repo menu's opening and closing, and its keys: opened, the picked row
 * has the focus; ↑↓, Home and End move it, and Esc closes the menu, back to
 * the button. Leaving it, by Tab or a click elsewhere, closes it too.
 */
export class RepoMenu {
  /** Binds the button, the menu's keys, and closing it on a click or focus elsewhere. */
  constructor(
    private readonly layout: Pick<Layout, "repoMenu" | "reposButton">,
    private readonly hooks: RepoMenuHooks,
  ) {
    const { repoMenu, reposButton } = layout;
    reposButton.addEventListener("click", () => {
      this.toggle();
    });
    document.addEventListener("mousedown", (event) => {
      if (!(event.target as HTMLElement).closest(`.${REPO_MENU_ANCHOR_CLASS}`)) this.toggle(false);
    });
    repoMenu.addEventListener("keydown", (event) => {
      this.onKey(event);
    });
    repoMenu.addEventListener("focusout", (event) => {
      if (!repoMenu.contains(event.relatedTarget as Node | null) && event.relatedTarget !== reposButton)
        this.toggle(false);
    });
  }

  /** Whether the menu shows. */
  get isOpen(): boolean {
    return !this.layout.repoMenu.hidden;
  }

  /** Opens or closes the menu, or switches it with no `open`. */
  toggle(open = !this.isOpen): void {
    const { repoMenu, reposButton } = this.layout;
    repoMenu.hidden = !open;
    reposButton.setAttribute("aria-expanded", String(open));
    if (!open) return;
    renderRepoMenu(repoMenu, this.hooks.repos(), this.hooks.selected(), {
      onPick: (repoName) => {
        this.toggle(false);
        this.hooks.onPick(repoName);
      },
    });
    (repoMenu.querySelector<HTMLElement>('[aria-checked="true"]') ?? this.items()[0])?.focus();
  }

  private items(): HTMLElement[] {
    return [...this.layout.repoMenu.querySelectorAll<HTMLElement>('[role="menuitemradio"]')];
  }

  private onKey(event: KeyboardEvent): void {
    const items = this.items();
    const at = items.indexOf(document.activeElement as HTMLElement);
    const next: Record<string, number> = {
      ArrowDown: (at + 1) % items.length,
      ArrowUp: (at - 1 + items.length) % items.length,
      Home: 0,
      End: items.length - 1,
    };
    if (event.key === "Escape") {
      this.toggle(false);
      this.layout.reposButton.focus();
    } else if (event.key in next) {
      items[next[event.key] ?? 0]?.focus();
    } else {
      return;
    }
    event.preventDefault();
    event.stopPropagation(); // Esc closes the menu, not the panel
  }
}
