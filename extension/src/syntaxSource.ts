// Colors the panel's code with the active color theme and the grammars of
// the installed extensions, and says when that coloring changes: the theme
// switches, extensions come or go, or the colors are customized or turned off.
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import * as vscode from "vscode";
import { readColorTheme, themeFile } from "./colorTheme";
import type { SyntaxLinesMessage } from "./protocol.gen";
import { syntaxHighlighting } from "./settings";
import { GrammarCatalog, SyntaxColors } from "./syntaxColors";
import { type FileLines, SyntaxPosts, type SyntaxSource } from "./syntaxLines";

/** The settings that change how code is colored. */
const COLOR_SETTINGS = [
  "spelunk.ui.syntaxHighlighting",
  "workbench.colorTheme",
  "workbench.preferredDarkColorTheme",
  "workbench.preferredLightColorTheme",
  "workbench.preferredHighContrastColorTheme",
  "workbench.preferredHighContrastLightColorTheme",
  "window.autoDetectColorScheme",
  "editor.tokenColorCustomizations",
];

/** The editor.* settings the panel draws code with (see uiSettings). */
const EDITOR_SETTINGS = ["editor.renderWhitespace", "editor.tabSize", "editor.fontLigatures"];

/** Anything that shows the search panel and can post to it. */
interface PanelLike {
  isOpen: boolean;
  post(type: "syntax.lines", payload: SyntaxLinesMessage): void;
}

/**
 * How the panel draws code: syntax colors for what is posted to `panel`,
 * colored again when the theme changes, and `onEditorSettings` when the
 * editor settings it draws code with change.
 */
export function codeColors(
  extensionPath: string,
  panel: PanelLike,
  onEditorSettings: () => void,
): { posts: SyntaxPosts } & vscode.Disposable {
  const source = themeSyntaxSource(extensionPath, () => {
    if (panel.isOpen) posts.recolor();
  });
  const posts = new SyntaxPosts(source, (message) => {
    panel.post("syntax.lines", message);
  });
  const editorSettings = vscode.workspace.onDidChangeConfiguration((event) => {
    if (EDITOR_SETTINGS.some((setting) => event.affectsConfiguration(setting))) onEditorSettings();
  });
  return {
    posts,
    dispose: () => {
      source.dispose();
      editorSettings.dispose();
    },
  };
}

/** The SyntaxSource of the extension host; `onChange` runs when code should be colored again. */
function themeSyntaxSource(extensionPath: string, onChange: () => void): SyntaxSource & vscode.Disposable {
  let colors: Promise<SyntaxColors | undefined> | undefined;
  const changed = (): void => {
    colors = undefined;
    onChange();
  };
  const subscriptions = [
    vscode.window.onDidChangeActiveColorTheme(changed),
    vscode.extensions.onDidChange(changed),
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (COLOR_SETTINGS.some((setting) => event.affectsConfiguration(setting))) changed();
    }),
  ];
  const wasm = (): Promise<Buffer> => readFile(join(extensionPath, "dist", "onig.wasm"));
  return {
    async color(files: readonly FileLines[]): Promise<SyntaxLinesMessage | undefined> {
      if (!syntaxHighlighting(vscode.workspace.getConfiguration("spelunk"))) return undefined;
      colors ??= themeColors(wasm);
      const syntax = await colors;
      return syntax?.color(files);
    },
    dispose(): void {
      for (const subscription of subscriptions) subscription.dispose();
    },
  };
}

/** Colors for the active theme, or undefined when its file can't be found or read. */
async function themeColors(wasm: () => Promise<Buffer>): Promise<SyntaxColors | undefined> {
  const extensions = vscode.extensions.all;
  const name = activeThemeName();
  const theme = themeFile(extensions, name);
  if (theme === undefined) return undefined;
  try {
    const customizations = vscode.workspace.getConfiguration("editor").get<unknown>("tokenColorCustomizations");
    const { rules } = await readColorTheme(theme.file, name, customizations);
    return new SyntaxColors(new GrammarCatalog(extensions), rules, wasm);
  } catch {
    return undefined;
  }
}

/** The theme VS Code shows: workbench.colorTheme, or the preferred theme of the active kind when it picks by the OS. */
function activeThemeName(): string {
  const workbench = vscode.workspace.getConfiguration("workbench");
  const kind = vscode.window.activeColorTheme.kind;
  const preferred = {
    [vscode.ColorThemeKind.Dark]: "preferredDarkColorTheme",
    [vscode.ColorThemeKind.Light]: "preferredLightColorTheme",
    [vscode.ColorThemeKind.HighContrast]: "preferredHighContrastColorTheme",
    [vscode.ColorThemeKind.HighContrastLight]: "preferredHighContrastLightColorTheme",
  }[kind];
  const highContrast = kind === vscode.ColorThemeKind.HighContrast || kind === vscode.ColorThemeKind.HighContrastLight;
  const autoDetect = vscode.workspace.getConfiguration("window").get<boolean>("autoDetectColorScheme", false);
  const chosen = workbench.get<string>("colorTheme", "Default Dark Modern");
  // VS Code switches to a high contrast theme by itself when the OS asks for one.
  if ((autoDetect || highContrast) && preferred) return workbench.get<string>(preferred, chosen);
  return chosen;
}
