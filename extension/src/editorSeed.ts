// Takes the search panel's query from the editor: ⌘P's selection, and the
// name at the cursor for Find Definitions and Find Uses.
import * as vscode from "vscode";
import type { SearchController } from "./controller";
import type { SearchPanel } from "./panel";
import { definitionsQuery, seedFromSelection, usesQuery } from "./seedQuery";
import { seedsFromSelection } from "./settings";

/**
 * spelunk.open, which searches the editor's selection when its key is pressed
 * in the editor, and Find Definitions and Find Uses for the name at the cursor.
 */
export function editorCommands(
  panel: SearchPanel,
  controller: SearchController,
): Record<string, (argument?: unknown) => unknown> {
  return {
    "spelunk.open": (argument) => {
      const given = queryArgument(argument);
      const seeded = given === undefined && fromEditor(argument) ? selectionQuery(controller.text) : undefined;
      panel.show();
      controller.restore(given ?? seeded, seeded !== undefined);
    },
    "spelunk.findDefinitions": () => {
      searchNameAtCursor(panel, controller, definitionsQuery);
    },
    "spelunk.findUses": () => {
      searchNameAtCursor(panel, controller, usesQuery);
    },
  };
}

/** The query in spelunk.open's optional `{ query }` argument, which the help page's Try passes. */
function queryArgument(argument: unknown): string | undefined {
  if (typeof argument !== "object" || argument === null || !("query" in argument)) return undefined;
  return typeof argument.query === "string" ? argument.query : undefined;
}

/** Whether spelunk.open was pressed in the editor: the search key's binding there passes `{ fromEditor: true }`. */
function fromEditor(argument: unknown): boolean {
  return typeof argument === "object" && argument !== null && "fromEditor" in argument && argument.fromEditor === true;
}

/** How long a status bar note about the editor's text stays. */
const NOTE_MS = 5000;

/** The query the active editor's selection seeds, if spelunk.search.seedFromSelection is on and it has one. */
function selectionQuery(lastQuery: string): string | undefined {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.selection.isEmpty || !seedsFromSelection(vscode.workspace.getConfiguration("spelunk")))
    return undefined;
  const seed = seedFromSelection(editor.document.getText(editor.selection), lastQuery);
  if ("query" in seed) return seed.query;
  if (seed.skipped === "tooLong") {
    vscode.window.setStatusBarMessage(
      "$(info) Spelunk: the selection is too long to search, so the last query stays",
      NOTE_MS,
    );
  }
  return undefined;
}

/** Opens the panel on Find Definitions' or Find Uses' query for the name at the cursor, or the one-line selection. */
function searchNameAtCursor(panel: SearchPanel, controller: SearchController, query: (name: string) => string): void {
  const editor = vscode.window.activeTextEditor;
  const name = editor && nameAtCursor(editor);
  if (!name) {
    vscode.window.setStatusBarMessage("$(info) Spelunk: put the cursor on a name to search for it", NOTE_MS);
    return;
  }
  panel.show();
  controller.restore(query(name), true);
}

/** The one-line selection, trimmed, or else the word under the cursor. */
function nameAtCursor(editor: vscode.TextEditor): string | undefined {
  const { document, selection } = editor;
  if (!selection.isEmpty) {
    const text = document.getText(selection).trim();
    return text && !/[\r\n]/.test(text) ? text : undefined;
  }
  const range = document.getWordRangeAtPosition(selection.active);
  return range && document.getText(range);
}
