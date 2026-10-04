// ESLint for the TypeScript and JavaScript outside daemon/ (Go has golangci-lint).
//
// The strictest standard presets, type-aware through each package's tsconfig:
//   - typescript-eslint strict-type-checked and stylistic-type-checked
//   - eslint-plugin-unicorn's recommended set
//   - eslint-plugin-jsdoc, so every exported function, class and type says what it is for
// plus size limits that keep functions reviewable. Formatting is Prettier's job:
// eslint-config-prettier switches off the rules that would fight it.
//
// Run: npm run lint:ts
import js from "@eslint/js";
import prettier from "eslint-config-prettier";
import jsdoc from "eslint-plugin-jsdoc";
import unicorn from "eslint-plugin-unicorn";
import { defineConfig } from "eslint/config";
import globals from "globals";
import tseslint from "typescript-eslint";

export default defineConfig(
  {
    ignores: [
      "**/node_modules/",
      "**/dist/",
      "**/dist-test/",
      "**/out/",
      "**/*.gen.ts", // generated from protocol/protocol.schema.json
      "daemon/",
      "testdata/",
      "extension/types/", // a stand-in for @types/vscode on machines without it
    ],
  },
  js.configs.recommended,
  tseslint.configs.strictTypeChecked,
  tseslint.configs.stylisticTypeChecked,
  unicorn.configs.recommended,
  {
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    linterOptions: { reportUnusedDisableDirectives: "error" },
    rules: {
      // Size and shape: long or deeply nested code is hard to review.
      complexity: ["error", 15],
      "max-depth": ["error", 4],
      "max-params": ["error", 4],
      "max-lines-per-function": ["error", { max: 80, skipBlankLines: true, skipComments: true }],
      "max-lines": ["error", { max: 600, skipBlankLines: true, skipComments: true }],

      // Clarity.
      curly: ["error", "multi-line"],
      eqeqeq: ["error", "always"],
      "no-console": "error",
      "no-else-return": "error",
      "no-param-reassign": "error",
      "object-shorthand": "error",
      "no-restricted-syntax": [
        "error",
        { selector: "ExportDefaultDeclaration", message: "Use named exports." },
        { selector: "TSEnumDeclaration[const=true]", message: "const enum breaks per-file compilation; use a union." },
      ],

      // TypeScript.
      "@typescript-eslint/consistent-type-definitions": ["error", "interface"],
      "@typescript-eslint/consistent-type-imports": "error",
      "@typescript-eslint/explicit-module-boundary-types": "error",
      "@typescript-eslint/switch-exhaustiveness-check": "error",
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
      // Numbers read naturally in messages ("3 of 5"); everything else must be a string already.
      "@typescript-eslint/restrict-template-expressions": ["error", { allowNumber: true }],
      // A do-nothing callback is clearest as `() => {}`.
      "@typescript-eslint/no-empty-function": ["error", { allow: ["arrowFunctions"] }],
      "@typescript-eslint/naming-convention": [
        "error",
        { selector: "default", format: ["camelCase"], leadingUnderscore: "allow" },
        { selector: "variable", format: ["camelCase", "UPPER_CASE"] },
        { selector: "typeLike", format: ["PascalCase"] },
        // Protocol fields, message types ("query.changed"), CSS and data-testid names keep their own spelling.
        { selector: ["objectLiteralProperty", "typeProperty", "objectLiteralMethod"], format: null },
        { selector: "import", format: null },
      ],

      // Where the project's conventions differ from unicorn's defaults.
      "unicorn/filename-case": ["error", { case: "camelCase", ignore: [/\.config\.m?js$/] }],
      "unicorn/no-null": "off", // the protocol uses null (an empty query's root, absent globals)
      "unicorn/prefer-top-level-await": "off", // the extension is bundled to CommonJS
      "unicorn/prefer-event-target": "off", // Node's EventEmitter is the idiom in the extension host
      // Named imports say what a file uses: import { join } from "node:path".
      "unicorn/import-style": ["error", { styles: { "node:path": { named: true } } }],
      "unicorn/prevent-abbreviations": [
        "error",
        {
          checkFilenames: false,
          // Words of this domain: a result's opaque ref, JSON-RPC params, process env.
          allowList: { ref: true, refs: true, params: true, Params: true, env: true, args: true },
        },
      ],
    },
  },
  {
    files: ["**/*.ts"],
    extends: [jsdoc.configs["flat/recommended-typescript-error"]],
    rules: {
      "jsdoc/require-jsdoc": [
        "error",
        {
          publicOnly: true,
          require: { FunctionDeclaration: true, ClassDeclaration: true, MethodDefinition: true },
          contexts: [
            "TSInterfaceDeclaration",
            "TSTypeAliasDeclaration",
            "ExportNamedDeclaration > VariableDeclaration",
          ],
        },
      ],
      // TypeScript types already document parameters and results; prose says what types can't.
      "jsdoc/require-param": "off",
      "jsdoc/require-returns": "off",
    },
  },
  {
    // Tests: one test per behaviour reads better than many tiny helpers.
    files: ["**/test/**", "**/*.test.ts", "**/*.test.mjs"],
    rules: {
      "max-lines-per-function": "off",
      "max-lines": "off",
      // Assertions read best on the awaited value: assert.equal((await reply()).text, "x").
      "unicorn/no-await-expression-member": "off",
      // node:test runs the promise test() returns.
      "@typescript-eslint/no-floating-promises": [
        "error",
        { allowForKnownSafeCalls: [{ from: "package", package: "node:test", name: ["test", "describe", "it"] }] },
      ],
    },
  },
  {
    // Build scripts and tests in plain JavaScript run in Node and print progress.
    files: ["**/*.mjs", "**/*.js"],
    extends: [tseslint.configs.disableTypeChecked],
    languageOptions: { globals: { ...globals.node } },
    rules: {
      "no-console": "off",
      // Plain JavaScript can't declare types.
      "@typescript-eslint/explicit-module-boundary-types": "off",
    },
  },
  {
    // Tool configs must default-export their settings.
    files: ["*.config.mjs"],
    rules: { "no-restricted-syntax": "off" },
  },
  {
    files: ["webview/test/**/*.mjs"],
    languageOptions: { globals: { ...globals.node, ...globals.browser } },
    // Playwright's locator.innerText() is not the DOM property this rule is about.
    rules: { "unicorn/prefer-dom-node-text-content": "off" },
  },
  prettier,
);
