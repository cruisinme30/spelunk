// Maps the spelunk.* configuration to protocol types.
import type { Settings, UiSettings, WelcomeStateMessage as WelcomeState } from "./protocol.gen";

/** Anything shaped like VS Code's WorkspaceConfiguration.get. */
export interface ConfigReader {
  get<T>(key: string, defaultValue: T): T;
}

/** Defaults, matching the "default" values declared in package.json. */
const DEFAULTS = {
  caseSensitive: false,
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
} as const;

/** The hard cap on results per page; the daemon enforces the same limit. */
const MAX_DEFAULT_COUNT = 50_000;
const MAX_TYPING_DELAY_MS = 1000;
/** The daemon reads maxFileSizeKB into an int; this keeps it well inside one (1 GiB). */
const MAX_FILE_SIZE_KB = 1024 * 1024;
const MAX_RECENT_QUERIES = 100;
/** The index.historyDepth values. */
export const HISTORY_DEPTHS: readonly Settings["historyDepth"][] = ["6m", "2y", "all"];
const OPEN_TRIGGERS: readonly UiSettings["openTrigger"][] = ["doubleClick", "singleClick"];
const ORDERS: readonly UiSettings["order"][] = ["best", "path"];
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

function string(value: unknown, fallback: string): string {
  return typeof value === "string" ? value : fallback;
}

/** The strings of an array setting; `fallback` when it isn't an array. */
function strings(value: unknown, fallback: readonly string[]): string[] {
  if (!Array.isArray(value)) return [...fallback];
  return value.filter((item): item is string => typeof item === "string");
}

/** Expands a leading `~` or `~/`, but not `~user/`, which names another user's home. */
function expandHome(path: string, home: string): string {
  if (path === "~") return home;
  if (path.startsWith("~/")) return home + path.slice(1);
  return path;
}

/** The settings the daemon needs, validated and clamped. */
export function daemonSettings(config: ConfigReader, home: string): Settings {
  const read = (key: string, fallback: unknown) => config.get<unknown>(key, fallback);
  return {
    caseSensitive: boolean(read("caseSensitive", DEFAULTS.caseSensitive), DEFAULTS.caseSensitive),
    defaultCount: clamp(read("defaultCount", DEFAULTS.defaultCount), 1, MAX_DEFAULT_COUNT, DEFAULTS.defaultCount),
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
    location: expandHome(
      string(read("index.location", DEFAULTS.location), DEFAULTS.location) || DEFAULTS.location,
      home,
    ),
    order: oneOf(read("order", DEFAULTS.order), ORDERS, DEFAULTS.order),
  };
}

/** The settings the webview needs. */
export function uiSettings(config: ConfigReader): UiSettings {
  const read = (key: string, fallback: unknown) => config.get<unknown>(key, fallback);
  return {
    typingDelayMs: clamp(read("typingDelayMs", DEFAULTS.typingDelayMs), 0, MAX_TYPING_DELAY_MS, DEFAULTS.typingDelayMs),
    openTrigger: oneOf(read("open.trigger", DEFAULTS.openTrigger), OPEN_TRIGGERS, DEFAULTS.openTrigger),
    preview: boolean(read("open.preview", DEFAULTS.openPreview), DEFAULTS.openPreview),
    showParsedQuery: boolean(read("ui.showParsedQuery", DEFAULTS.showParsedQuery), DEFAULTS.showParsedQuery),
    caseSensitive: boolean(read("caseSensitive", DEFAULTS.caseSensitive), DEFAULTS.caseSensitive),
    order: oneOf(read("order", DEFAULTS.order), ORDERS, DEFAULTS.order),
  };
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
