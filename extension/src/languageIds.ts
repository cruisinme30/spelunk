// Language names shared by syntax coloring and notebook replacement validation.

/** VS Code's IDs for the languages Spelunk names otherwise; the rest are the same. */
const VSCODE_LANGUAGE_IDS: Readonly<Record<string, string>> = {
  "c++": "cpp",
  shell: "shellscript",
  make: "makefile",
  docker: "dockerfile",
  text: "plaintext",
};

/** The editor language ID corresponding to a canonical daemon language name. */
export function editorLanguage(language: string): string {
  return VSCODE_LANGUAGE_IDS[language] ?? language;
}
