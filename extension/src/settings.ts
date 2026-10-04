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

const MAX_DEFAULT_COUNT = 50_000; // the hard cap on results, as in the daemon
const MAX_TYPING_DELAY_MS = 1000;

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, Math.round(value)));
}

/** Expands a leading `~` or `~/`, but not `~user/`, which names another user's home. */
function expandHome(path: string, home: string): string {
  if (path === "~") return home;
  if (path.startsWith("~/")) return home + path.slice(1);
  return path;
}

/** The settings the daemon needs, validated and clamped. */
export function daemonSettings(config: ConfigReader, home: string): Settings {
  const depth = config.get<string>("index.historyDepth", DEFAULTS.historyDepth);
  return {
    caseSensitive: config.get("caseSensitive", DEFAULTS.caseSensitive),
    defaultCount: clamp(config.get("defaultCount", DEFAULTS.defaultCount), 1, MAX_DEFAULT_COUNT),
    historyDepth: depth === "6m" || depth === "all" ? depth : "2y",
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
    openTrigger:
      config.get<string>("open.trigger", DEFAULTS.openTrigger) === "singleClick" ? "singleClick" : "doubleClick",
    preview: config.get("open.preview", DEFAULTS.openPreview),
    showParsedQuery: config.get("ui.showParsedQuery", DEFAULTS.showParsedQuery),
    caseSensitive: config.get("caseSensitive", DEFAULTS.caseSensitive),
  };
}
