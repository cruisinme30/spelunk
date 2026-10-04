// Maps the unifiedSearch.* configuration to protocol types.
import type { Settings, UiSettings } from "./protocol.gen";

/** Anything shaped like VS Code's WorkspaceConfiguration.get. */
export interface ConfigReader {
  get<T>(key: string, defaultValue: T): T;
}

/** Defaults, matching the "default" values declared in package.json. */
export const DEFAULTS = {
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
  location: "~/.unified-search/index",
  showParsedQuery: true,
  recentQueries: 20,
} as const;

/** The hard cap on results per page; the daemon enforces the same limit. */
const MAX_DEFAULT_COUNT = 50_000;
const MAX_TYPING_DELAY_MS = 1000;
const HISTORY_DEPTHS: readonly Settings["historyDepth"][] = ["6m", "2y", "all"];
const OPEN_TRIGGERS: readonly UiSettings["openTrigger"][] = ["doubleClick", "singleClick"];

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, Math.round(value)));
}

/** `value` if it is one of `allowed`, otherwise `fallback`: settings.json can hold anything. */
function oneOf<T extends string>(value: string, allowed: readonly T[], fallback: T): T {
  return allowed.find((candidate) => candidate === value) ?? fallback;
}

/** Expands a leading `~` or `~/`, but not `~user/`, which names another user's home. */
function expandHome(path: string, home: string): string {
  if (path === "~") return home;
  if (path.startsWith("~/")) return home + path.slice(1);
  return path;
}

/** The settings the daemon needs, validated and clamped. */
export function daemonSettings(config: ConfigReader, home: string): Settings {
  return {
    caseSensitive: config.get("caseSensitive", DEFAULTS.caseSensitive),
    defaultCount: clamp(config.get("defaultCount", DEFAULTS.defaultCount), 1, MAX_DEFAULT_COUNT),
    historyDepth: oneOf(
      config.get<string>("index.historyDepth", DEFAULTS.historyDepth),
      HISTORY_DEPTHS,
      DEFAULTS.historyDepth,
    ),
    symbols: config.get("index.symbols", DEFAULTS.symbols),
    exclude: config.get<string[]>("index.exclude", [...DEFAULTS.exclude]),
    includeIgnored: config.get("index.includeIgnored", DEFAULTS.includeIgnored),
    maxFileSizeKB: Math.max(1, config.get("index.maxFileSizeKB", DEFAULTS.maxFileSizeKB)),
    location: expandHome(config.get<string>("index.location", DEFAULTS.location), home),
  };
}

/** The settings the webview needs. */
export function uiSettings(config: ConfigReader): UiSettings {
  return {
    typingDelayMs: clamp(config.get("typingDelayMs", DEFAULTS.typingDelayMs), 0, MAX_TYPING_DELAY_MS),
    openTrigger: oneOf(config.get<string>("open.trigger", DEFAULTS.openTrigger), OPEN_TRIGGERS, DEFAULTS.openTrigger),
    preview: config.get("open.preview", DEFAULTS.openPreview),
    showParsedQuery: config.get("ui.showParsedQuery", DEFAULTS.showParsedQuery),
    caseSensitive: config.get("caseSensitive", DEFAULTS.caseSensitive),
  };
}
