// Maps the spelunk.* configuration to protocol types.
import { isAbsolute } from "node:path";
import type {
  CaseSetting,
  PinnedQuery,
  Settings,
  UiSettings,
  WelcomeStateMessage as WelcomeState,
} from "./protocol.gen";

/** Anything shaped like VS Code's WorkspaceConfiguration.get. */
export interface ConfigReader {
  get<T>(key: string, defaultValue: T): T;
}

/** Defaults, matching the "default" values declared in package.json. */
const DEFAULTS = {
  caseSensitive: "off",
  wholeWord: false,
  defaultCount: 500,
  typingDelayMs: 120,
  openTrigger: "doubleClick",
  openPreview: true,
  closeOnOpen: true,
  historyDepth: "2y",
  symbols: true,
  exclude: ["**/vendor/**", "**/node_modules/**", "**/*.min.js"],
  includeIgnored: false,
  maxFileSizeKB: 1024,
  location: "~/.spelunk/index",
  showParsedQuery: true,
  recentQueries: 20,
  shortcutPreset: "quickOpen",
  order: "best",
  contextLines: 0,
  watchNotify: true,
  timeLimitMs: 2000,
  previewContextLines: 7,
  vendoredFolders: ["vendor", "node_modules", "third_party", "dist"],
  generatedSuffixes: [
    ".pb.go",
    "_gen.go",
    ".gen.go",
    ".gen.ts",
    "_pb2.py",
    "_pb2_grpc.py",
    "_pb2.pyi",
    ".min.js",
    ".min.css",
  ],
  codeownersFiles: [".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"],
  commitCheckSeconds: 5,
  syntaxHighlighting: true,
  renderWhitespace: "editor",
} as const;

/** VS Code's defaults for the editor.* settings the panel's code follows. */
const EDITOR_DEFAULTS = { renderWhitespace: "selection", tabSize: 4, fontLigatures: false } as const;

/** The hard cap on results per page; the daemon enforces the same limit. */
const MAX_DEFAULT_COUNT = 50_000;
const MAX_TYPING_DELAY_MS = 1000;
/** The daemon reads maxFileSizeKB into an int; this keeps it well inside one (1 GiB). */
const MAX_FILE_SIZE_KB = 1024 * 1024;
const MAX_RECENT_QUERIES = 100;
/** search.timeLimitMs's bounds; the daemon keeps it within the same. */
const MIN_TIME_LIMIT_MS = 100;
const MAX_TIME_LIMIT_MS = 60_000;
/** The most lines the file preview shows on each side of its match. */
const MAX_PREVIEW_CONTEXT_LINES = 100;
/** index.commitCheckSeconds's bounds; the daemon keeps it within the same. */
const MAX_COMMIT_CHECK_SECONDS = 3600;
/** The most context lines a code result shows on each side of its match. */
export const MAX_CONTEXT_LINES = 3;
/** The index.historyDepth values. */
export const HISTORY_DEPTHS: readonly Settings["historyDepth"][] = ["6m", "2y", "all"];
const OPEN_TRIGGERS: readonly UiSettings["openTrigger"][] = ["doubleClick", "singleClick"];
const ORDERS: readonly UiSettings["order"][] = ["best", "path"];
const CASE_SETTINGS: readonly CaseSetting[] = ["off", "on", "smart"];
const RENDER_WHITESPACE: readonly UiSettings["renderWhitespace"][] = [
  "none",
  "boundary",
  "selection",
  "trailing",
  "all",
];
const MAX_TAB_SIZE = 16;
/** The shortcut.preset values. */
export const SHORTCUT_PRESETS: readonly WelcomeState["preset"][] = ["quickOpen", "findInFiles", "none"];

// settings.json can hold anything, whatever type package.json declares, and
// the daemon refuses an initialize whose settings have the wrong JSON types.
// So every value is checked here, and a wrong one falls back to its default.

/** A whole number within min..max; `fallback` for anything that isn't a finite number. */
function clamp(value: unknown, min: number, max: number, fallback: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) return fallback;
  return Math.min(max, Math.max(min, Math.round(value)));
}

/** `value` if it is one of `allowed`, otherwise `fallback`. */
function oneOf<T extends string>(value: unknown, allowed: readonly T[], fallback: T): T {
  return allowed.find((candidate) => candidate === value) ?? fallback;
}

function boolean(value: unknown, fallback: boolean): boolean {
  return typeof value === "boolean" ? value : fallback;
}

/** spelunk.search.caseSensitive. */
function caseSetting(value: unknown): CaseSetting {
  return oneOf(value, CASE_SETTINGS, DEFAULTS.caseSensitive);
}

/**
 * The settings that moved into a section, by their old key: the key each
 * is read from now. The old keys stay declared, deprecated, so a value
 * saved under one can be moved (see movedValue) and then removed.
 */
export const MOVED_SETTINGS: Readonly<Record<string, string>> = {
  caseSensitive: "search.caseSensitive",
  wholeWord: "search.wholeWord",
  typingDelayMs: "search.typingDelayMs",
  defaultCount: "results.count",
  order: "results.order",
};

/**
 * The moved settings' new keys that only the user's settings may hold (scope
 * "application"): VS Code refuses a workspace or folder value for them.
 */
export const USER_ONLY_SETTINGS: ReadonlySet<string> = new Set(["search.typingDelayMs"]);

/**
 * The value to save under a moved setting's new key, at a scope where its
 * old key holds `old` and its new key `current`: undefined when there is
 * nothing to move, or the new key already has a value there, which wins. A
 * caseSensitive saved as a boolean, from before it had a smart value,
 * moves as on or off.
 */
export function movedValue(oldKey: string, old?: unknown, current?: unknown): unknown {
  if (old === undefined || current !== undefined) return undefined;
  if (oldKey === "caseSensitive" && typeof old === "boolean") return old ? "on" : "off";
  return old;
}

function string(value: unknown, fallback: string): string {
  return typeof value === "string" ? value : fallback;
}

/** The strings of an array setting; `fallback` when it isn't an array. */
function strings(value: unknown, fallback: readonly string[]): string[] {
  if (!Array.isArray(value)) return [...fallback];
  return value.filter((item): item is string => typeof item === "string");
}

/** Expands a leading `~`, `~/` or (as Windows writes it) `~\`, but not `~user/`, which names another user's home. */
function expandHome(path: string, home: string): string {
  if (path === "~") return home;
  if (path.startsWith("~/") || path.startsWith("~\\")) return home + path.slice(1);
  return path;
}

/**
 * spelunk.index.location with `~` expanded. Anything that isn't then an
 * absolute path (`relative/idx`, `${workspaceFolder}/idx`, `~user/idx`)
 * would put the index wherever the daemon happens to run, so it falls back to
 * the default, as a blank one does; indexLocationProblem says why.
 */
function indexLocation(config: ConfigReader, home: string): string {
  const saved = string(config.get<unknown>("index.location", DEFAULTS.location), DEFAULTS.location).trim();
  const expanded = expandHome(saved, home);
  return isAbsolute(expanded) ? expanded : expandHome(DEFAULTS.location, home);
}

/** Why spelunk.index.location isn't used as it is written, for the log; undefined when it is. */
export function indexLocationProblem(config: ConfigReader, home: string): string | undefined {
  const saved = config.get<unknown>("index.location", DEFAULTS.location);
  // A blank location, like one of the wrong type, means the default without a word.
  if (typeof saved !== "string" || !saved.trim() || isAbsolute(expandHome(saved.trim(), home))) return undefined;
  return `spelunk.index.location ${JSON.stringify(saved)} is not an absolute path or one starting with ~, so indexes go to ${DEFAULTS.location}`;
}

/** The settings the daemon needs, validated and clamped. */
export function daemonSettings(config: ConfigReader, home: string): Settings {
  const read = (key: string, fallback: unknown) => config.get<unknown>(key, fallback);
  return {
    caseSensitive: caseSetting(read("search.caseSensitive", DEFAULTS.caseSensitive)),
    wholeWord: boolean(read("search.wholeWord", DEFAULTS.wholeWord), DEFAULTS.wholeWord),
    defaultCount: clamp(read("results.count", DEFAULTS.defaultCount), 1, MAX_DEFAULT_COUNT, DEFAULTS.defaultCount),
    historyDepth: oneOf(read("index.historyDepth", DEFAULTS.historyDepth), HISTORY_DEPTHS, DEFAULTS.historyDepth),
    symbols: boolean(read("index.symbols", DEFAULTS.symbols), DEFAULTS.symbols),
    exclude: strings(read("index.exclude", DEFAULTS.exclude), DEFAULTS.exclude),
    includeIgnored: boolean(read("index.includeIgnored", DEFAULTS.includeIgnored), DEFAULTS.includeIgnored),
    maxFileSizeKB: clamp(
      read("index.maxFileSizeKB", DEFAULTS.maxFileSizeKB),
      1,
      MAX_FILE_SIZE_KB,
      DEFAULTS.maxFileSizeKB,
    ),
    location: indexLocation(config, home),
    order: oneOf(read("results.order", DEFAULTS.order), ORDERS, DEFAULTS.order),
    contextLines: contextLines(config),
    timeLimitMs: clamp(
      read("search.timeLimitMs", DEFAULTS.timeLimitMs),
      MIN_TIME_LIMIT_MS,
      MAX_TIME_LIMIT_MS,
      DEFAULTS.timeLimitMs,
    ),
    vendoredFolders: strings(read("index.vendoredFolders", DEFAULTS.vendoredFolders), DEFAULTS.vendoredFolders),
    generatedSuffixes: strings(read("index.generatedSuffixes", DEFAULTS.generatedSuffixes), DEFAULTS.generatedSuffixes),
    codeownersFiles: strings(read("index.codeownersFiles", DEFAULTS.codeownersFiles), DEFAULTS.codeownersFiles),
    commitCheckSeconds: clamp(
      read("index.commitCheckSeconds", DEFAULTS.commitCheckSeconds),
      1,
      MAX_COMMIT_CHECK_SECONDS,
      DEFAULTS.commitCheckSeconds,
    ),
  };
}

/** spelunk.results.contextLines, clamped to 0..3. */
function contextLines(config: ConfigReader): number {
  const value = config.get<unknown>("results.contextLines", DEFAULTS.contextLines);
  return clamp(value, 0, MAX_CONTEXT_LINES, DEFAULTS.contextLines);
}

/** A ConfigReader with nothing set: every setting reads as its default. */
const NOTHING_SET: ConfigReader = { get: (_key, defaultValue) => defaultValue };

/** The settings the webview needs; `editor` reads the editor.* settings its code follows. */
export function uiSettings(config: ConfigReader, editor: ConfigReader = NOTHING_SET): UiSettings {
  const read = (key: string, fallback: unknown) => config.get<unknown>(key, fallback);
  return {
    typingDelayMs: clamp(
      read("search.typingDelayMs", DEFAULTS.typingDelayMs),
      0,
      MAX_TYPING_DELAY_MS,
      DEFAULTS.typingDelayMs,
    ),
    openTrigger: oneOf(read("open.trigger", DEFAULTS.openTrigger), OPEN_TRIGGERS, DEFAULTS.openTrigger),
    preview: boolean(read("open.preview", DEFAULTS.openPreview), DEFAULTS.openPreview),
    showParsedQuery: boolean(read("ui.showParsedQuery", DEFAULTS.showParsedQuery), DEFAULTS.showParsedQuery),
    caseSensitive: caseSetting(read("search.caseSensitive", DEFAULTS.caseSensitive)),
    wholeWord: boolean(read("search.wholeWord", DEFAULTS.wholeWord), DEFAULTS.wholeWord),
    order: oneOf(read("results.order", DEFAULTS.order), ORDERS, DEFAULTS.order),
    contextLines: contextLines(config),
    renderWhitespace: renderWhitespace(config, editor),
    tabSize: clamp(editor.get<unknown>("tabSize", EDITOR_DEFAULTS.tabSize), 1, MAX_TAB_SIZE, EDITOR_DEFAULTS.tabSize),
    fontLigatures: fontFeatures(editor.get<unknown>("fontLigatures", EDITOR_DEFAULTS.fontLigatures)),
  };
}

/** spelunk.ui.renderWhitespace, or editor.renderWhitespace when it is "editor" (its default). */
function renderWhitespace(config: ConfigReader, editor: ConfigReader): UiSettings["renderWhitespace"] {
  const own = config.get<unknown>("ui.renderWhitespace", DEFAULTS.renderWhitespace);
  if (own !== DEFAULTS.renderWhitespace && RENDER_WHITESPACE.includes(own as UiSettings["renderWhitespace"])) {
    return own as UiSettings["renderWhitespace"];
  }
  return oneOf(
    editor.get<unknown>("renderWhitespace", EDITOR_DEFAULTS.renderWhitespace),
    RENDER_WHITESPACE,
    "selection",
  );
}

/**
 * editor.fontLigatures as CSS font-feature-settings, as VS Code reads it:
 * true turns ligatures on, false off, and a string is the feature settings themselves.
 */
function fontFeatures(value: unknown): string {
  if (typeof value === "string" && value.trim() !== "" && value !== "false") {
    return value === "true" ? '"liga" on, "calt" on' : value;
  }
  return value === true ? '"liga" on, "calt" on' : '"liga" off, "calt" off';
}

/** Whether code is colored with the color theme's syntax colors (spelunk.ui.syntaxHighlighting). */
export function syntaxHighlighting(config: ConfigReader): boolean {
  return boolean(
    config.get<unknown>("ui.syntaxHighlighting", DEFAULTS.syntaxHighlighting),
    DEFAULTS.syntaxHighlighting,
  );
}

/** Lines the file preview shows above and below its match (spelunk.ui.previewContextLines). */
export function previewContextLines(config: ConfigReader): number {
  return clamp(
    config.get<unknown>("ui.previewContextLines", DEFAULTS.previewContextLines),
    0,
    MAX_PREVIEW_CONTEXT_LINES,
    DEFAULTS.previewContextLines,
  );
}

/** How many recent queries to keep (0 keeps none). */
export function recentQueriesLimit(config: ConfigReader): number {
  return clamp(
    config.get<unknown>("ui.recentQueries", DEFAULTS.recentQueries),
    0,
    MAX_RECENT_QUERIES,
    DEFAULTS.recentQueries,
  );
}

/**
 * The pinned queries, in the order they were pinned. An entry without a
 * query is dropped, a blank name counts as none, and only `watch: true`
 * watches. A query pinned twice is kept once, where it was first pinned,
 * with the first name it has and watched if either entry is.
 */
export function pinnedQueries(config: ConfigReader): PinnedQuery[] {
  const saved = config.get<unknown>("ui.pinnedQueries", []);
  if (!Array.isArray(saved)) return [];
  const pinned = new Map<string, PinnedQuery>();
  for (const entry of saved as unknown[]) {
    if (typeof entry !== "object" || entry === null) continue;
    const { query, name, watch } = entry as Record<string, unknown>;
    const pin = pinnedEntry(
      typeof query === "string" ? query : "",
      typeof name === "string" ? name : undefined,
      watch === true,
    );
    if (!pin) continue;
    const earlier = pinned.get(pin.query);
    pinned.set(
      pin.query,
      earlier ? (pinnedEntry(pin.query, earlier.name ?? pin.name, earlier.watch ?? pin.watch) ?? pin) : pin,
    );
  }
  return [...pinned.values()];
}

/**
 * A pinned query as it is saved: trimmed, a blank name dropped, `watch` only
 * when watched; undefined for a blank query.
 */
export function pinnedEntry(query: string, name: string | undefined, watch = false): PinnedQuery | undefined {
  const text = query.trim();
  if (!text) return undefined;
  const label = name?.trim();
  return { query: text, ...(label ? { name: label } : {}), ...(watch ? { watch } : {}) };
}

/** The pinned queries that are watched, for the daemon's watch/set. */
export function watchedQueries(config: ConfigReader): string[] {
  return pinnedQueries(config)
    .filter((pin) => pin.watch)
    .map((pin) => pin.query);
}

/** Whether a watch's new commits pop up a notification (spelunk.watch.notify). */
export function watchNotify(config: ConfigReader): boolean {
  return boolean(config.get<unknown>("watch.notify", DEFAULTS.watchNotify), DEFAULTS.watchNotify);
}

/** Whether opening a result closes the search panel. */
export function closeOnOpen(config: ConfigReader): boolean {
  return boolean(config.get<unknown>("open.closeOnOpen", DEFAULTS.closeOnOpen), DEFAULTS.closeOnOpen);
}

/** What the welcome page shows; `mac` writes keys as ⌘P rather than Ctrl+P. */
export function welcomeSettings(config: ConfigReader, mac: boolean): WelcomeState {
  const settings = daemonSettings(config, "");
  return {
    preset: oneOf(
      config.get<unknown>("shortcut.preset", DEFAULTS.shortcutPreset),
      SHORTCUT_PRESETS,
      DEFAULTS.shortcutPreset,
    ),
    historyDepth: settings.historyDepth,
    symbols: settings.symbols,
    mac,
  };
}
