// The search box's keys: ? for the operators, ⌘. for the first fix, Tab
// for the suggestion, Esc, the arrows, Enter, and the empty box's own keys.
import type { AskFlow } from "./askFlow";
import type { EmptyBox } from "./emptyBox";
import { wrapIndex } from "./format";
import { send } from "./host";
import type { Layout } from "./layout";
import type { RepoMenu } from "./render/repoMenu";
import type { ViewState } from "./state";

/** The panel's parts the keys reach. */
export interface KeyDeps {
  layout: Layout;
  state: ViewState;
  emptyBox: EmptyBox;
  repoMenu: RepoMenu;
  askFlow: AskFlow;
  completionsVisible: () => boolean;
  acceptCompletion: (index: number) => void;
  renderCompletions: () => void;
  /** Closes the suggestions and searches the box exactly as typed. */
  searchAsTyped: () => void;
  /** ⌘.: applies the first fix of the first diagnostic that has one. */
  applyFirstFix: () => void;
  /** ↑↓ through the results. */
  moveSelection: (step: 1 | -1) => void;
  /** ↵ on a selected result (⌘ or Ctrl opens it to the side). */
  openSelected: (event: KeyboardEvent) => void;
}

/** What the box's keys do. */
export class PanelKeys {
  /** The keys over `deps`: the box, the lists ↑↓ move through, and what ↵ runs. */
  constructor(private readonly deps: KeyDeps) {}

  /** Handles a key pressed in the box, and prevents its default when the key was one of ours. */
  onKeyDown(event: KeyboardEvent): void {
    // While an input method composes (Japanese, Chinese, Korean…), Enter, Esc and the arrows are its keys.
    if (event.isComposing) return;
    const handled = this.handleKey(event);
    if (handled) event.preventDefault();
  }

  /** Returns whether the key was handled (and its default should be prevented). */
  private handleKey(event: KeyboardEvent): boolean {
    const { deps } = this;
    const modifier = event.metaKey || event.ctrlKey;
    switch (event.key) {
      case "?": {
        return this.showOperators();
      }
      case ".": {
        if (!modifier) return false;
        deps.applyFirstFix();
        return true;
      }
      case "Tab": {
        if (!deps.completionsVisible()) return false;
        deps.acceptCompletion(deps.state.completionIndex);
        return true;
      }
      case "Escape": {
        this.onEscape();
        return true;
      }
      case "ArrowDown":
      case "ArrowUp": {
        this.onArrow(event.key === "ArrowDown" ? 1 : -1);
        return true;
      }
      case "Enter": {
        this.onEnter(event, modifier);
        return true;
      }
      case "Backspace": {
        return deps.emptyBox.removeSelected(event);
      }
      default: {
        return deps.emptyBox.watchSelected(event);
      }
    }
  }

  /**
   * ? while operators are suggested opens the help page; in an empty box it
   * brings the operator sheet into view, which a narrow panel stacks below
   * the recent queries. Returns false for a literal ?, as in a value: f:'s
   * globs and regexes and a /regex/ use it.
   */
  private showOperators(): boolean {
    const { layout, state } = this.deps;
    if (this.deps.completionsVisible() && state.completions[0]?.group === "operator") {
      send("help.open", {}); // every operator, explained
      return true;
    }
    if (layout.input.value) return false;
    layout.body.querySelector(".sheet")?.scrollIntoView({ block: "nearest" });
    return true;
  }

  /** Esc closes the suggestions first, keeping the word as typed, then the panel. */
  private onEscape(): void {
    const { deps } = this;
    if (deps.repoMenu.isOpen) deps.repoMenu.toggle(false);
    else if (deps.completionsVisible()) deps.searchAsTyped();
    else send("panel.close", {});
  }

  /** ↑↓ move through suggestions when they are open, otherwise through rows. */
  private onArrow(step: 1 | -1): void {
    const { deps } = this;
    const { state } = deps;
    if (deps.completionsVisible()) {
      state.completionIndex = wrapIndex(state.completionIndex, step, state.completions.length);
      deps.renderCompletions();
    } else if (deps.layout.input.value) {
      deps.moveSelection(step);
    } else {
      deps.emptyBox.moveSelection(step);
    }
  }

  /**
   * Enter asks a question in plain words, searches as typed while
   * suggesting (Tab accepts), runs a pinned or recent query, or opens the
   * selected result.
   */
  private onEnter(event: KeyboardEvent, modifier: boolean): void {
    const { deps } = this;
    if (!modifier && deps.askFlow.boxIsQuestion()) deps.askFlow.askBox();
    else if (deps.completionsVisible() && !modifier) deps.searchAsTyped();
    else if (!deps.layout.input.value) deps.emptyBox.runSelected();
    else if (deps.state.selectedRef) deps.openSelected(event);
  }
}
