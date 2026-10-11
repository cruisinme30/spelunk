// Plugs Spelunk's index into VS Code's own navigation: Go to Symbol in
// Workspace (⌘T) lists its definitions, and, when spelunk.editor.definitions
// allows, Go to Definition and Find References answer from it. navigation.ts
// decides what to ask the daemon.
import * as vscode from "vscode";
import { CancelSource } from "./jsonRpc";
import {
  answers,
  asksOthersFirst,
  definitions,
  type NameAt,
  type NavigationBackend,
  references,
  workspaceSymbols,
} from "./navigation";
import type { OpenTarget, SymbolKind, SymbolMatch } from "./protocol.gen";
import { type NavigationSettings, navigationSettings } from "./settings";

/** How each kind of definition shows in VS Code's lists. */
const KINDS: Record<SymbolKind, vscode.SymbolKind> = {
  class: vscode.SymbolKind.Class,
  interface: vscode.SymbolKind.Interface,
  function: vscode.SymbolKind.Function,
  method: vscode.SymbolKind.Method,
  type: vscode.SymbolKind.Struct,
  other: vscode.SymbolKind.Module,
};

/**
 * How many of Spelunk's own questions to the language servers are running.
 * While one is, VS Code asks every provider again, this one included, which
 * then stays out so the question gets only the language servers' answer.
 */
let askingOthers = 0;

/** Registers the providers; each reads its settings when asked, so a change applies at once. */
export function registerEditorNavigation(backend: NavigationBackend): vscode.Disposable[] {
  const files: vscode.DocumentSelector = { scheme: "file" };
  return [
    vscode.languages.registerWorkspaceSymbolProvider({
      provideWorkspaceSymbols: (query, token) =>
        withCancel(token, async (cancel) => {
          if (askingOthers > 0) return [];
          const found = await workspaceSymbols(backend, query, settings(), cancel);
          return found.map((match) => symbolInformation(match));
        }),
    }),
    vscode.languages.registerDefinitionProvider(files, {
      provideDefinition: (document, position, token) =>
        whenSpelunkAnswers(
          { document, position, command: "vscode.executeDefinitionProvider" },
          token,
          async (at, cancel) => {
            const found = await definitions(backend, at, settings(), cancel);
            return found.map((match) => symbolInformation(match).location);
          },
        ),
    }),
    vscode.languages.registerReferenceProvider(files, {
      provideReferences: (document, position, _context, token) =>
        whenSpelunkAnswers(
          { document, position, command: "vscode.executeReferenceProvider" },
          token,
          async (at, cancel) => {
            const found = await references(backend, at, settings(), cancel);
            return found.map((target) => location(target));
          },
        ),
    }),
  ];
}

function settings(): NavigationSettings {
  return navigationSettings(vscode.workspace.getConfiguration("spelunk"));
}

/** Go to Definition or Find References at a position, and the command that asks the language servers the same. */
interface Question {
  document: vscode.TextDocument;
  position: vscode.Position;
  command: string;
}

/**
 * Runs `answer` for the name at the question's position, if
 * spelunk.editor.definitions lets Spelunk answer: in fallback, only when the
 * language servers find nothing.
 */
async function whenSpelunkAnswers(
  { document, position, command }: Question,
  token: vscode.CancellationToken,
  answer: (at: NameAt, cancel: CancelSource) => Promise<vscode.Location[]>,
): Promise<vscode.Location[]> {
  const current = settings();
  if (askingOthers > 0 || current.definitions === "off") return [];
  const range = document.getWordRangeAtPosition(position);
  if (!range) return [];
  if (asksOthersFirst(current) && !answers(current, await othersAnswer(command, document.uri, position))) return [];
  if (token.isCancellationRequested) return [];
  return withCancel(token, (cancel) => answer({ name: document.getText(range), path: document.uri.fsPath }, cancel));
}

/** Whether the language servers have an answer for `command` at `position`, asked without Spelunk. */
async function othersAnswer(command: string, uri: vscode.Uri, position: vscode.Position): Promise<boolean> {
  askingOthers += 1;
  try {
    const found = await vscode.commands.executeCommand<unknown[] | undefined>(command, uri, position);
    return (found ?? []).length > 0;
  } finally {
    askingOthers -= 1;
  }
}

/** Runs `work` with a CancelSource that VS Code's token cancels; a cancelled or failed request answers nothing. */
async function withCancel<T>(
  token: vscode.CancellationToken,
  work: (cancel: CancelSource) => Promise<T[]>,
): Promise<T[]> {
  const cancel = new CancelSource();
  const listener = token.onCancellationRequested(() => {
    cancel.cancel();
  });
  try {
    return await work(cancel);
  } catch {
    // The daemon is restarting, the index is gone or VS Code moved on: VS Code's own answers stand.
    return [];
  } finally {
    listener.dispose();
  }
}

/** A definition as VS Code lists it, marked "generated" or "vendored" beside its name when it is one. */
function symbolInformation(match: SymbolMatch): vscode.SymbolInformation {
  const start = new vscode.Position(match.line - 1, match.column - 1);
  const range = new vscode.Range(start, start.translate(0, match.name.length));
  const location = new vscode.Location(vscode.Uri.file(match.path), range);
  return new vscode.SymbolInformation(match.name, KINDS[match.symbolKind], match.rankReason ?? "", location);
}

/** Where a use of a name is, from where opening its result would go. */
function location(target: OpenTarget): vscode.Location {
  const start = new vscode.Position((target.line ?? 1) - 1, (target.column ?? 1) - 1);
  const end = start.translate(0, target.length ?? 0);
  return new vscode.Location(vscode.Uri.file(target.path ?? ""), new vscode.Range(start, end));
}
