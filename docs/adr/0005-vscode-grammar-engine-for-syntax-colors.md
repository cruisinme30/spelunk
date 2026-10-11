# 0005. Color code with VS Code's own grammar engine, in the extension host

- Status: Accepted
- Date: 2026-10-11

## Context

Results and the preview show code, and users want it colored as their editor colors it. VS Code gives a webview its
theme's workbench colors as CSS variables, but not its syntax colors, and no API colors a line of text for an
extension. The colors have to be worked out by the extension, with whatever theme and languages the user has.

## Options considered

1. **A small lexer per language in the panel**: comments, strings, numbers, keywords and names, in a few KB. Fast, but
   its tokens differ from the editor's wherever a grammar knows more, and every language is code to write.
2. **VS Code's grammar engine** (vscode-textmate, with the vscode-oniguruma regex engine) running the TextMate grammars
   the installed extensions contribute, with the active theme's token colors. The colors match the editor for every
   language the user has a grammar for, at the cost of two MIT dependencies and a 462 KB WebAssembly file.
3. **The same engine in the webview.** The webview would need every grammar file served to it and WebAssembly allowed
   by its content security policy, and it would color the same line again in each panel.

## Decision

Option 2, in the extension host (`syntaxColors.ts`, `colorTheme.ts`), as the user chose: it is consistent with the rest
of the IDE. The host posts each line's tokens by its file and text (`syntax.lines`) after the batch or preview that
shows it, so coloring never holds results up, and the panel only wraps the text it already drew.

## Consequences

- The extension now bundles runtime dependencies, and `dist/onig.wasm` ships in the package.
- A result's lines, and a preview's, are colored from the first one shown, so a match inside a comment or string that
  opened above them can be colored as code. A diff's lines are colored one by one.
- Semantic highlighting (colors from a language server) isn't included: those tokens come from language servers, not
  from grammars.
- The theme is read as VS Code reads it (includes, a tmTheme `tokenColors` file, `editor.tokenColorCustomizations`, and
  the names VS Code moved its own themes to). A theme it can't find or read leaves code plain.
