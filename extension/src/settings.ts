// Maps unifiedSearch.* configuration (Contract 5) to protocol types.
import type { Settings, UiSettings } from "./protocol.gen";

/** Anything with VS Code's WorkspaceConfiguration.get shape. */
export interface ConfigReader {
  get<T>(key: string, defaultValue: T): T;
}

export const DEFAULTS = {
  caseSensitive: false,
  defaultCount: 500,
  typingDelayMs: 120,
  historyDepth: "2y" as const,
  symbols: true,
  exclude: ["**/vendor/**", "**/node_modules/**", "**/*.min.js"],
  includeIgnored: false,
  maxFileSizeKB: 1024,
  location: "~/.unified-search/index",
  recentQueries: 20,
};

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, Math.round(n)));

export function daemonSettings(c: ConfigReader, home: string): Settings {
  const loc = c.get("index.location", DEFAULTS.location);
  const depth = c.get<string>("index.historyDepth", DEFAULTS.historyDepth);
  return {
    caseSensitive: c.get("caseSensitive", DEFAULTS.caseSensitive),
    defaultCount: clamp(c.get("defaultCount", DEFAULTS.defaultCount), 1, 50000),
    historyDepth: depth === "6m" || depth === "all" ? depth : "2y",
    symbols: c.get("index.symbols", DEFAULTS.symbols),
    exclude: c.get("index.exclude", DEFAULTS.exclude),
    includeIgnored: c.get("index.includeIgnored", DEFAULTS.includeIgnored),
    maxFileSizeKB: Math.max(1, c.get("index.maxFileSizeKB", DEFAULTS.maxFileSizeKB)),
    location: loc.startsWith("~") ? home + loc.slice(1) : loc,
  };
}

export function uiSettings(c: ConfigReader): UiSettings {
  return {
    typingDelayMs: clamp(c.get("typingDelayMs", DEFAULTS.typingDelayMs), 0, 1000),
    openTrigger: c.get<string>("open.trigger", "doubleClick") === "singleClick" ? "singleClick" : "doubleClick",
    preview: c.get("open.preview", true),
    showParsedQuery: c.get("ui.showParsedQuery", true),
    caseSensitive: c.get("caseSensitive", DEFAULTS.caseSensitive),
  };
}
