// The Replace row under the search box: the replacement, what Replace all
// would change (the result rows show each match struck out and replaced),
// and what it did. The host plans and applies a replace; the row only asks
// and shows, so nothing here writes a file.
import { button, element, icon, plural } from "./format";
import { send } from "./host";
import type { Layout } from "./layout";
import type { ReplaceDoneMessage, ReplacePlan, ReplacePlanMessage } from "./protocol.gen";

/** The row's parts, built once by createReplaceRow. */
export interface ReplaceRow {
  root: HTMLElement;
  input: HTMLInputElement;
  /** What Replace all would change, or why it can't. */
  summary: HTMLElement;
  applyButton: HTMLButtonElement;
  /** What the last Replace all did, with Save; or what Replace all changes, before it runs. */
  note: HTMLElement;
}

const REPLACE_ICON =
  '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h13l-3-3"/><path d="M20 17H7l3 3"/></svg>';

/** What Replace all changes, said before it runs. */
const BEFORE_NOTE =
  "Changes code matches in current files, never commits. The files stay unsaved, and ⌘Z in any of them undoes the whole replace.";

/** Builds the Replace row, hidden until the ⇄ toggle opens it. */
export function createReplaceRow(): ReplaceRow {
  const input = element("input", {
    id: "replacement",
    type: "text",
    spellcheck: "false",
    autocomplete: "off",
    placeholder: "Replace with…",
    "aria-label": "Replace with",
    "data-testid": "replacement",
  });
  const summary = element("span", { class: "replace-summary", role: "status", "data-testid": "replace-summary" });
  const applyButton = element(
    "button",
    { type: "button", class: "btn primary", "data-testid": "replace-all", disabled: true },
    "Replace all",
    element("kbd", {}, "⌘↵"),
  );
  const note = element("div", { class: "replace-note", "data-testid": "replace-note" }, BEFORE_NOTE);
  const iconLabel = element("label", { for: "replacement", class: "query-icon" }, icon(REPLACE_ICON));
  const root = element(
    "div",
    { class: "replace-row", "data-testid": "replace-row", hidden: true },
    iconLabel,
    input,
    summary,
    applyButton,
    note,
  );
  return { root, input, summary, applyButton, note };
}

/** What the row needs from the panel. */
interface ReplaceHooks {
  /** The query the results on screen are for, or undefined while there is none to replace. */
  searchedText(): string | undefined;
  typingDelayMs(): number;
  /** The preview changed: the result rows and file preview show it, or stop showing it. */
  onPreviewChanged(): void;
  /** The replacement gained or lost the focus, which changes the key hints. */
  onFocusChanged(): void;
}

/** ⌥⌘F, or Ctrl+H, opens and closes the Replace row, as in VS Code's own search. */
function isReplaceShortcut(event: KeyboardEvent): boolean {
  return (event.code === "KeyF" && event.metaKey && event.altKey) || (event.code === "KeyH" && event.ctrlKey);
}

/** The key a line's replacements are filed under. */
const lineKey = (repoId: string, path: string, line: number) => `${repoId}\n${path}\n${String(line)}`;

/** "Replace needs exactly one search term." from the daemon's "replace needs exactly one search term". */
function sentence(text: string): string {
  const trimmed = text.trim();
  if (!trimmed) return trimmed;
  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1) + (/[.!?]$/.test(trimmed) ? "" : ".");
}

/** The Replace row's controller: one per panel. */
export class ReplaceBox {
  /** Numbers each preview and apply; an answer to an older one is dropped. */
  private seq = 0;
  /** The query the preview is for. */
  private text = "";
  private plan: ReplacePlan | undefined;
  /** Each previewed line's replacements, in text order, by lineKey. */
  private readonly byLine = new Map<string, string[]>();
  private applying = false;
  private timer: ReturnType<typeof setTimeout> | undefined;

  private readonly row: ReplaceRow;

  /** Binds the row, ⇄ (which says whether the row is open) and the shortcut in the query box. */
  constructor(
    private readonly layout: Pick<Layout, "replaceRow" | "replaceButton" | "input">,
    private readonly hooks: ReplaceHooks,
  ) {
    const row = layout.replaceRow;
    this.row = row;
    layout.replaceButton.addEventListener("click", () => {
      this.setOpen(!this.isOpen);
    });
    layout.input.addEventListener("keydown", (event) => {
      if (!isReplaceShortcut(event)) return;
      event.preventDefault();
      this.setOpen(!this.isOpen);
    });
    row.input.addEventListener("input", () => {
      clearTimeout(this.timer);
      this.timer = setTimeout(() => {
        this.refresh();
      }, hooks.typingDelayMs());
    });
    row.input.addEventListener("keydown", (event) => {
      if (event.isComposing) return;
      if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) this.apply();
      else if (event.key === "Escape" || isReplaceShortcut(event)) this.setOpen(false);
      else return;
      event.preventDefault();
    });
    row.applyButton.addEventListener("click", () => {
      this.apply();
    });
    for (const type of ["focus", "blur"]) {
      row.input.addEventListener(type, () => {
        hooks.onFocusChanged();
      });
    }
  }

  /** Whether the row is showing. */
  get isOpen(): boolean {
    return !this.row.root.hidden;
  }

  /** Opens the row and focuses the replacement, or closes it and drops its preview. */
  setOpen(open: boolean): void {
    this.row.root.hidden = !open;
    this.layout.replaceButton.setAttribute("aria-expanded", String(open));
    if (open) {
      this.row.input.focus();
      this.row.input.select();
      this.refresh();
      return;
    }
    clearTimeout(this.timer);
    this.seq++; // drops answers still on their way
    this.clearPlan();
    this.hooks.onPreviewChanged();
    this.layout.input.focus();
  }

  /** Asks for a fresh preview, for a new query or replacement. */
  refresh(): void {
    if (!this.isOpen) return;
    clearTimeout(this.timer);
    this.seq++;
    this.applying = false;
    this.clearPlan();
    this.row.note.replaceChildren(BEFORE_NOTE);
    const text = this.hooks.searchedText();
    if (text === undefined) {
      this.show("Type a search to replace its matches.");
    } else {
      this.text = text;
      this.show("Finding matches…");
      send("replace.preview", { seq: this.seq, text, replacement: this.row.input.value });
    }
    this.hooks.onPreviewChanged();
  }

  /** The replacements of a line's matches, in text order, while the preview shows them. */
  replacementsFor(repoId: string, path: string, line: number): string[] | undefined {
    return this.byLine.get(lineKey(repoId, path, line));
  }

  /** The host's preview: every edit Replace all would make, or why it can't. */
  onPlan({ seq, plan, error }: ReplacePlanMessage): void {
    if (seq !== this.seq || !this.isOpen) return;
    this.clearPlan();
    if (error !== undefined || !plan) {
      this.show(sentence(error ?? "Replace isn't available."));
    } else if (plan.truncated) {
      this.show("Too many matches to replace at once. Narrow the search.");
    } else if (plan.matches === 0) {
      this.show("No code matches to replace.");
    } else {
      this.plan = plan;
      for (const file of plan.files) {
        for (const line of file.lines) {
          this.byLine.set(
            lineKey(file.repoId, file.path, line.line),
            line.edits.map((edit) => edit.newText),
          );
        }
      }
      this.show(`${plural(plan.matches, "match", "matches")} in ${plural(plan.files.length, "file")}`, true);
    }
    this.hooks.onPreviewChanged();
  }

  /** What Replace all did. */
  onDone(done: ReplaceDoneMessage): void {
    if (done.seq !== this.seq) return;
    this.applying = false;
    if (done.error !== undefined) {
      this.show(sentence(done.error), this.plan !== undefined);
      return;
    }
    if (done.changed === true) {
      this.row.note.replaceChildren("The matches changed since the preview. Check them and replace again.");
      return; // the new preview came with it
    }
    this.clearPlan();
    this.hooks.onPreviewChanged();
    this.show(`Replaced ${plural(done.replaced, "match", "matches")} in ${plural(done.files, "file")}`);
    const parts: (Node | string)[] = [];
    if (done.files > 0) {
      parts.push(
        "They're unsaved, and ⌘Z in any of them undoes the replace. ",
        button(
          { class: "btn link", "data-testid": "replace-save" },
          () => {
            send("replace.save", {});
          },
          "Save them",
        ),
      );
    }
    if (done.skipped.length > 0) {
      const skipped = `Skipped ${plural(done.skipped.length, "file")} that changed since they were searched: `;
      parts.push(element("div", {}, skipped, element("code", {}, done.skipped.join(", "))));
    }
    this.row.note.replaceChildren(...parts);
  }

  /** Replace all: asks the host to apply the replace the preview shows. */
  private apply(): void {
    const plan = this.plan;
    if (!plan || this.applying) return;
    this.applying = true;
    this.seq++;
    this.show(`Replacing ${plural(plan.matches, "match", "matches")}…`);
    send("replace.apply", { seq: this.seq, text: this.text, replacement: this.row.input.value, matches: plan.matches });
  }

  private clearPlan(): void {
    this.plan = undefined;
    this.byLine.clear();
  }

  /** Says `text` in the summary; Replace all works only while there is a plan to apply. */
  private show(text: string, canApply = false): void {
    this.row.summary.textContent = text;
    this.row.applyButton.disabled = !canApply;
  }
}
