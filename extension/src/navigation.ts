// What VS Code's Go to Symbol in Workspace (⌘T), Go to Definition and Find
// References ask Spelunk, and how its answers come back. No vscode here, so
// tests run it against the real daemon; editorNavigation.ts plugs it in.
import type { CancelSource } from "./jsonRpc";
import type { OpenTarget, RpcRequests, SearchBatchParams, SymbolMatch } from "./protocol.gen";
import type { NavigationSettings } from "./settings";

/** What navigation needs from the daemon: requests, and a search's streamed batches. */
export interface NavigationBackend {
  request<M extends keyof RpcRequests>(
    method: M,
    params: RpcRequests[M][0],
    cancel?: CancelSource,
  ): Promise<RpcRequests[M][1]>;
  on(event: "batch", listener: (batch: SearchBatchParams) => void): unknown;
  off(event: "batch", listener: (batch: SearchBatchParams) => void): unknown;
}

/** The most uses Find References lists, best first, like the definitions symbols/find returns by default. */
const MAX_REFERENCES = 200;

/** A name Find References can search with ref:: letters, digits, _ and $, as code names things. */
const REFERENCE_NAME = /^[\p{L}\p{N}_$]+$/u;

/** The definitions ⌘T lists for what was typed: none when it is blank or the setting is off. */
export async function workspaceSymbols(
  backend: NavigationBackend,
  text: string,
  settings: NavigationSettings,
  cancel?: CancelSource,
): Promise<SymbolMatch[]> {
  if (!settings.workspaceSymbols || text.trim() === "") return [];
  const params = { text, includeGeneratedAndVendored: settings.includeGeneratedAndVendored };
  const { symbols } = await backend.request("symbols/find", params, cancel);
  return symbols;
}

/**
 * Whether Spelunk answers Go to Definition or Find References: never when
 * spelunk.editor.definitions is off, only when no language server answered
 * when it is fallback, and always when it is always.
 */
export function answers(settings: NavigationSettings, othersAnswered: boolean): boolean {
  switch (settings.definitions) {
    case "always": {
      return true;
    }
    case "fallback": {
      return !othersAnswered;
    }
    case "off": {
      return false;
    }
  }
}

/** Whether answers() needs to know what the language servers say first. */
export function asksOthersFirst(settings: NavigationSettings): boolean {
  return settings.definitions === "fallback";
}

/** A name at the cursor, and the absolute path of the file it is in. */
export interface NameAt {
  name: string;
  path: string;
}

/** Where the name at the cursor is defined: in its file's language when that has any. */
export async function definitions(
  backend: NavigationBackend,
  at: NameAt,
  settings: NavigationSettings,
  cancel?: CancelSource,
): Promise<SymbolMatch[]> {
  const params = {
    text: at.name,
    exact: true,
    languageOf: at.path,
    includeGeneratedAndVendored: settings.includeGeneratedAndVendored,
  };
  const { symbols } = await backend.request("symbols/find", params, cancel);
  return symbols;
}

/**
 * The query that finds the lines using `name` as a whole word, without the
 * lines defining it: in `language` when given, case and all, and leaving out
 * generated and vendored files unless the settings keep them. Undefined for a
 * name ref: can't take, such as one with a quote in it.
 */
export function referencesQuery(
  name: string,
  language: string | undefined,
  settings: NavigationSettings,
): string | undefined {
  if (!REFERENCE_NAME.test(name)) return undefined;
  const parts = [`ref:"${name}"`, "case:yes", `count:${String(MAX_REFERENCES)}`];
  if (language) parts.push(`lang:${language}`);
  if (!settings.includeGeneratedAndVendored) parts.push("-is:generated", "-is:vendored");
  return parts.join(" ");
}

/**
 * Where the name at the cursor is used: the lines ref: finds in its file's
 * language, or in every language when it has none there, each at the name's
 * place on its line.
 */
export async function references(
  backend: NavigationBackend,
  at: NameAt,
  settings: NavigationSettings,
  cancel?: CancelSource,
): Promise<OpenTarget[]> {
  const { name } = at;
  const found = await backend.request("symbols/find", { text: name, exact: true, languageOf: at.path }, cancel);
  const inLanguage = referencesQuery(name, found.language, settings);
  if (inLanguage === undefined) return [];
  let refs = await searchLines(backend, inLanguage, cancel);
  if (refs.length === 0 && found.language) {
    refs = await searchLines(backend, referencesQuery(name, undefined, settings) ?? "", cancel);
  }
  const targets = await Promise.all(refs.map((ref) => backend.request("open/resolve", { ref }, cancel)));
  return targets.filter((target) => target.path !== undefined && target.line !== undefined);
}

/** Counts searches, so each has its own id among the daemon's batches. */
let searches = 0;

/** Runs `text` and returns the refs of its code lines, in the order they came. */
async function searchLines(backend: NavigationBackend, text: string, cancel?: CancelSource): Promise<string[]> {
  searches += 1;
  const searchId = `navigation-${String(searches)}`;
  const refs: string[] = [];
  const onBatch = (batch: SearchBatchParams): void => {
    if (batch.searchId !== searchId) return;
    for (const item of batch.items) if (item.kind === "line") refs.push(item.ref);
  };
  backend.on("batch", onBatch);
  try {
    await backend.request("search/start", { searchId, text }, cancel);
  } finally {
    backend.off("batch", onBatch);
  }
  return refs.slice(0, MAX_REFERENCES);
}
