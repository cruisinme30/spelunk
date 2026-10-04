// ESLint flat config for the TypeScript in extension/ and webview/.
// Type-aware rules use each package's tsconfig.json via the project service.
// Formatting is Prettier's job, so eslint-config-prettier turns off style rules.
import js from "@eslint/js";
import prettier from "eslint-config-prettier";
import { defineConfig } from "eslint/config";
import globals from "globals";
import tseslint from "typescript-eslint";

export default defineConfig(
  {
    ignores: ["**/node_modules/", "**/dist/", "**/dist-test/", "**/out/", "**/*.gen.ts", "daemon/", "extension/types/"],
  },
  js.configs.recommended,
  tseslint.configs.recommendedTypeChecked,
  {
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    rules: {
      "@typescript-eslint/consistent-type-definitions": ["error", "interface"],
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
      curly: ["error", "multi-line"],
      eqeqeq: ["error", "always"],
      "no-restricted-syntax": [
        "error",
        { selector: "ExportDefaultDeclaration", message: "Use named exports." },
        { selector: "TSEnumDeclaration[const=true]", message: "const enum breaks per-file compilation; use a union." },
      ],
    },
  },
  {
    files: ["**/*.mjs", "**/*.js"],
    extends: [tseslint.configs.disableTypeChecked],
    languageOptions: { globals: { ...globals.node } },
  },
  {
    files: ["webview/test/**/*.mjs"],
    languageOptions: { globals: { ...globals.node, ...globals.browser } },
  },
  prettier,
);
