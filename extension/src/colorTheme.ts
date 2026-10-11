// Reads a color theme's token colors the way VS Code does: from the theme
// file an extension contributes, the files it includes, and the user's
// editor.tokenColorCustomizations. Nothing here imports vscode.
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { parseRawGrammar } from "vscode-textmate";
import type { InstalledExtension, ThemeRule } from "./syntaxColors";

/** A theme as an extension contributes it in contributes.themes. */
interface ThemeContribution {
  id?: string;
  label?: string;
  uiTheme?: string;
  path?: string;
}

/** What a theme file holds that colors code. */
interface ThemeFile {
  include?: unknown;
  colors?: Record<string, unknown>;
  tokenColors?: unknown;
}

/** Includes deeper than this are a loop, or not worth following. */
const MAX_INCLUDES = 10;

/**
 * The scopes each shorthand of editor.tokenColorCustomizations colors, as
 * VS Code maps them.
 */
const CUSTOMIZATION_SCOPES: Record<string, string[]> = {
  comments: ["comment", "punctuation.definition.comment"],
  strings: ["string", "meta.embedded.assembly"],
  keywords: ["keyword - keyword.operator", "keyword.control", "storage", "storage.type"],
  numbers: ["constant.numeric"],
  types: ["entity.name.type", "entity.name.class", "support.type", "support.class"],
  functions: ["entity.name.function", "support.function"],
  variables: ["variable", "entity.name.variable"],
};

/**
 * The names VS Code moved its own themes to, which a setting saved before
 * may still hold (VS Code maps them the same way).
 */
const RENAMED_THEMES: Record<string, string> = {
  "Default Dark Modern": "Dark Modern",
  "Default Light Modern": "Light Modern",
  "Default Dark+": "Dark+",
  "Default Light+": "Light+",
  "Experimental Dark": "Dark 2026",
  "VS Code Dark": "Dark 2026",
  "Experimental Light": "Light 2026",
  "VS Code Light": "Light 2026",
};

/** A color theme, as reading its file gives it. */
export interface ColorTheme {
  /** Its token-color rules, as vscode-textmate takes them. */
  rules: ThemeRule[];
  /** Its workbench colors, such as "editor.background". */
  colors: Record<string, string>;
}

/**
 * The file of the theme `name` (an id, or a label as older settings hold),
 * among every extension's themes, and its uiTheme ("vs", "vs-dark",
 * "hc-black" or "hc-light").
 */
export function themeFile(
  extensions: readonly InstalledExtension[],
  name: string,
): { file: string; uiTheme: string } | undefined {
  const themes = extensions.flatMap(({ extensionPath, packageJSON }) => {
    const contributed = (packageJSON as { contributes?: { themes?: unknown } } | undefined)?.contributes?.themes;
    return (Array.isArray(contributed) ? (contributed as ThemeContribution[]) : [])
      .filter((theme) => typeof theme.path === "string")
      .map((theme) => ({ ...theme, file: join(extensionPath, theme.path ?? "") }));
  });
  const renamed = RENAMED_THEMES[name];
  const theme =
    themes.find((candidate) => candidate.id === name) ??
    themes.find((candidate) => candidate.label === name) ??
    themes.find((candidate) => renamed !== undefined && candidate.id === renamed);
  return theme && { file: theme.file, uiTheme: typeof theme.uiTheme === "string" ? theme.uiTheme : "vs-dark" };
}

/**
 * The theme in `file`, after the files it includes: its token-color
 * rules, led by its editor.foreground (the color of code no rule colors)
 * and followed by the user's `customizations` for it, and its colors.
 */
export async function readColorTheme(file: string, name: string, customizations: unknown): Promise<ColorTheme> {
  const { rules, colors } = await readTheme(file, 0);
  const foreground = colors["editor.foreground"];
  const base: ThemeRule[] = foreground ? [{ settings: { foreground } }] : [];
  return { rules: [...base, ...rules, ...customizationRules(customizations, name)], colors };
}

async function readTheme(file: string, depth: number): Promise<ColorTheme> {
  const theme = parseJsonc(await readFile(file, "utf8")) as ThemeFile;
  const included =
    typeof theme.include === "string" && depth < MAX_INCLUDES
      ? await readTheme(join(dirname(file), theme.include), depth + 1)
      : { rules: [], colors: {} };
  const own = Object.entries(theme.colors ?? {}).filter(
    (entry): entry is [string, string] => typeof entry[1] === "string",
  );
  return {
    rules: [...included.rules, ...(await tokenColors(theme.tokenColors, file))],
    colors: { ...included.colors, ...Object.fromEntries(own) },
  };
}

/** A theme's tokenColors: rules in the file, or the path of a .tmTheme or JSON file holding them. */
async function tokenColors(value: unknown, file: string): Promise<ThemeRule[]> {
  if (Array.isArray(value)) return value as ThemeRule[];
  if (typeof value !== "string") return [];
  const path = join(dirname(file), value);
  const content = await readFile(path, "utf8");
  // A .tmTheme is a plist, which vscode-textmate's grammar reader parses as well as a grammar.
  const parsed = path.endsWith(".json") ? parseJsonc(content) : (parseRawGrammar(content, path) as unknown);
  if (Array.isArray(parsed)) return parsed as ThemeRule[];
  const settings =
    (parsed as { settings?: unknown; tokenColors?: unknown }).settings ?? (parsed as ThemeFile).tokenColors;
  return Array.isArray(settings) ? (settings as ThemeRule[]) : [];
}

/** editor.tokenColorCustomizations as rules: its textMateRules and shorthands, then those under "[name]". */
function customizationRules(customizations: unknown, name: string): ThemeRule[] {
  if (!isRecord(customizations)) return [];
  const forTheme = customizations[`[${name}]`];
  return [...rulesOf(customizations), ...(isRecord(forTheme) ? rulesOf(forTheme) : [])];
}

function rulesOf(customizations: Record<string, unknown>): ThemeRule[] {
  const rules: ThemeRule[] = [];
  for (const [key, scopes] of Object.entries(CUSTOMIZATION_SCOPES)) {
    const value = customizations[key];
    if (typeof value === "string") rules.push({ scope: scopes, settings: { foreground: value } });
    else if (isRecord(value)) rules.push({ scope: scopes, settings: value });
  }
  const textMateRules = customizations["textMateRules"];
  if (Array.isArray(textMateRules)) rules.push(...(textMateRules as ThemeRule[]));
  return rules;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** JSON with comments and trailing commas, as theme files are written. */
function parseJsonc(text: string): unknown {
  let out = "";
  for (let index = 0; index < text.length; index++) {
    const char = text.charAt(index);
    if (char === '"') {
      const end = endOfString(text, index);
      out += text.slice(index, end);
      index = end - 1;
    } else if (char === "/" && text[index + 1] === "/") {
      const end = text.indexOf("\n", index);
      index = (end === -1 ? text.length : end) - 1;
    } else if (char === "/" && text[index + 1] === "*") {
      const end = text.indexOf("*/", index + 2);
      index = end === -1 ? text.length : end + 1;
    } else {
      out += char;
    }
  }
  return JSON.parse(out.replaceAll(/,(\s*[\]}])/g, "$1"));
}

/** The index just past the string literal that opens at `start`. */
function endOfString(text: string, start: number): number {
  for (let index = start + 1; index < text.length; index++) {
    if (text[index] === "\\") index++;
    else if (text[index] === '"') return index + 1;
  }
  return text.length;
}
