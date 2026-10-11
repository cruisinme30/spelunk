// Colors lines of code as the editor does: the TextMate grammars the
// installed extensions contribute, run by vscode-textmate (VS Code's own
// grammar engine) with the color theme's token colors. Nothing here imports
// vscode, so tests run it in Node with grammars on disk.
import { readFile } from "node:fs/promises";
import { basename, join } from "node:path";
import { loadWASM, OnigScanner, OnigString } from "vscode-oniguruma";
import { INITIAL, parseRawGrammar, Registry, type IGrammar, type IRawTheme, type StateStack } from "vscode-textmate";
import type { SyntaxLinesMessage } from "./protocol.gen";
import type { FileLines, SyntaxSource } from "./syntaxLines";

/** What the highlighter reads of an installed extension: where it is, and its package.json. */
export interface InstalledExtension {
  extensionPath: string;
  packageJSON: unknown;
}

/** One token-color rule of a theme, as theme files and editor.tokenColorCustomizations write them. */
export type ThemeRule = IRawTheme["settings"][number];

/** A line's tokens: a start and a style per token (SyntaxLine in the protocol). */
export type LineTokens = number[];

/** The style of a token in a font style: index + FONT_STYLE_STEP × style. */
const FONT_STYLE_STEP = 512;
/** Where vscode-textmate's encoded token attributes keep the font style and the foreground color index. */
const FONT_STYLE_SHIFT = 11;
const FONT_STYLE_MASK = 0b1111;
const FOREGROUND_SHIFT = 15;
const FOREGROUND_MASK = 0b1_1111_1111;
/** Longer lines go uncolored: a grammar can take a long time over one, and the panel cuts them short anyway. */
const MAX_LINE_LENGTH = 2000;
/** How long one line may take; a line that takes longer keeps the tokens found so far. */
const LINE_TIME_LIMIT_MS = 50;

interface GrammarContribution {
  language?: string;
  scopeName?: string;
  path?: string;
  injectTo?: string[];
}

interface LanguageContribution {
  id?: string;
  extensions?: string[];
  filenames?: string[];
}

/** Which grammar colors which file, from every extension's contributes.languages and contributes.grammars. */
export class GrammarCatalog {
  private readonly languageByExtension = new Map<string, string>();
  private readonly languageByFilename = new Map<string, string>();
  private readonly scopeByLanguage = new Map<string, string>();
  private readonly fileByScope = new Map<string, string>();
  private readonly injectionsByScope = new Map<string, string[]>();

  /** Reads the languages and grammars `extensions` contribute. */
  constructor(extensions: readonly InstalledExtension[]) {
    for (const { extensionPath, packageJSON } of extensions) {
      const contributes = (packageJSON as { contributes?: unknown } | undefined)?.contributes as
        { languages?: unknown; grammars?: unknown } | undefined;
      for (const language of arrayOf<LanguageContribution>(contributes?.languages)) this.addLanguage(language);
      for (const grammar of arrayOf<GrammarContribution>(contributes?.grammars))
        this.addGrammar(extensionPath, grammar);
    }
  }

  private addLanguage({ id, extensions, filenames }: Partial<LanguageContribution>): void {
    if (typeof id !== "string") return;
    for (const extension of arrayOf<string>(extensions)) {
      if (typeof extension === "string") this.languageByExtension.set(extension.toLowerCase(), id);
    }
    for (const filename of arrayOf<string>(filenames)) {
      if (typeof filename === "string") this.languageByFilename.set(filename, id);
    }
  }

  private addGrammar(
    extensionPath: string,
    { language, scopeName, path, injectTo }: Partial<GrammarContribution>,
  ): void {
    if (typeof scopeName !== "string" || typeof path !== "string") return;
    this.fileByScope.set(scopeName, join(extensionPath, path));
    if (typeof language === "string") this.scopeByLanguage.set(language, scopeName);
    for (const target of arrayOf<string>(injectTo)) {
      if (typeof target !== "string") continue;
      this.injectionsByScope.set(target, [...(this.injectionsByScope.get(target) ?? []), scopeName]);
    }
  }

  /** The scope of the grammar that colors `path`, as VS Code picks its language: by file name, then by extension. */
  scopeFor(path: string): string | undefined {
    const name = basename(path);
    const language = this.languageByFilename.get(name) ?? this.languageByExtensionOf(name);
    return language === undefined ? undefined : this.scopeByLanguage.get(language);
  }

  /** The language of a file name's longest known extension: ".d.ts" before ".ts". */
  private languageByExtensionOf(name: string): string | undefined {
    const lower = name.toLowerCase();
    for (let dot = lower.indexOf("."); dot !== -1; dot = lower.indexOf(".", dot + 1)) {
      const language = this.languageByExtension.get(lower.slice(dot));
      if (language !== undefined) return language;
    }
    return undefined;
  }

  /** The grammar file of `scope`. */
  fileOf(scope: string): string | undefined {
    return this.fileByScope.get(scope);
  }

  /** The grammars that inject themselves into `scope`. */
  injectionsInto(scope: string): string[] {
    return this.injectionsByScope.get(scope) ?? [];
  }
}

function arrayOf<T>(value: unknown): Partial<T>[] {
  return Array.isArray(value) ? (value as Partial<T>[]) : [];
}

/** Colors lines of code with a theme; grammars load on first use, and once each. */
export class SyntaxColors implements SyntaxSource {
  private readonly registry: Registry;
  private readonly grammars = new Map<string, Promise<IGrammar | null>>();

  /**
   * `wasm` reads vscode-oniguruma's onig.wasm, the regex engine the grammars
   * need; it is read the first time a line is colored.
   */
  constructor(
    private readonly catalog: GrammarCatalog,
    rules: readonly ThemeRule[],
    wasm: () => Promise<ArrayBuffer | Uint8Array>,
  ) {
    let engine: Promise<void> | undefined;
    this.registry = new Registry({
      onigLib: Promise.resolve().then(async () => {
        engine ??= wasm().then((data) => loadWASM(data));
        await engine;
        return {
          createOnigScanner: (patterns) => new OnigScanner(patterns),
          createOnigString: (text) => new OnigString(text),
        };
      }),
      loadGrammar: async (scope) => {
        const file = this.catalog.fileOf(scope);
        return file === undefined ? null : parseRawGrammar(await readFile(file, "utf8"), file);
      },
      getInjections: (scope) => this.catalog.injectionsInto(scope),
    });
    this.registry.setTheme({ settings: [...rules] });
  }

  /** The theme's colors by index, as the styles of LineTokens index them; index 0 is unused. */
  colors(): string[] {
    // vscode-textmate leaves index 0 empty, and JSON would send the hole as null.
    return Array.from(this.registry.getColorMap(), (color: string | undefined) => color ?? "");
  }

  /** The colors of the lines `files` hold, as the panel takes them; undefined when no grammar colors any of them. */
  async color(files: readonly FileLines[]): Promise<SyntaxLinesMessage | undefined> {
    const lines: SyntaxLinesMessage["lines"] = [];
    for (const file of files) {
      const tokens = await this.tokenize(file.path, file.lines, file.continued);
      if (!tokens) continue;
      for (const [index, text] of file.lines.entries())
        lines.push({ path: file.path, text, tokens: tokens[index] ?? [] });
    }
    return lines.length > 0 ? { colors: this.colors(), lines } : undefined;
  }

  /**
   * The tokens of `lines` of `path`, or undefined when no grammar colors it.
   * With `continued`, the lines are consecutive and each picks up where the
   * one above left off (so a comment opened above stays a comment); otherwise
   * each is colored on its own.
   */
  async tokenize(path: string, lines: readonly string[], continued: boolean): Promise<LineTokens[] | undefined> {
    const scope = this.catalog.scopeFor(path);
    if (scope === undefined) return undefined;
    let grammar = this.grammars.get(scope);
    if (!grammar) {
      grammar = this.registry.loadGrammar(scope).catch(() => null);
      this.grammars.set(scope, grammar);
    }
    const loaded = await grammar;
    if (!loaded) return undefined;
    let state: StateStack = INITIAL;
    return lines.map((text) => {
      if (text.length > MAX_LINE_LENGTH) {
        state = INITIAL;
        return [];
      }
      const result = loaded.tokenizeLine2(text, continued ? state : INITIAL, LINE_TIME_LIMIT_MS);
      state = result.stoppedEarly ? INITIAL : result.ruleStack;
      return lineTokens(result.tokens);
    });
  }
}

/** vscode-textmate's [start, attributes, …] as LineTokens, merging neighbours of the same style. */
function lineTokens(encoded: Uint32Array): LineTokens {
  const tokens: LineTokens = [];
  for (let index = 0; index < encoded.length; index += 2) {
    const attributes = encoded[index + 1] ?? 0;
    const style =
      ((attributes >>> FOREGROUND_SHIFT) & FOREGROUND_MASK) +
      FONT_STYLE_STEP * ((attributes >>> FONT_STYLE_SHIFT) & FONT_STYLE_MASK);
    if (tokens.length > 0 && tokens.at(-1) === style) continue;
    tokens.push(encoded[index] ?? 0, style);
  }
  return tokens;
}
