// Stylelint for the webview's CSS. Run: npm run lint:css
/** @type {import("stylelint").Config} */
export default {
  extends: ["stylelint-config-standard"],
  rules: {
    "selector-class-pattern": ["^[a-z][a-z0-9]*(-[a-z0-9]+)*$", { message: "Use kebab-case class names." }],
    "selector-id-pattern": ["^[a-z][a-z0-9]*(-[a-z0-9]+)*$", { message: "Use kebab-case ids." }],
    // Our own custom properties are kebab-case; VS Code's theme variables keep VS Code's spelling.
    "custom-property-pattern": [
      "^(vscode-[A-Za-z0-9.-]+|[a-z][a-z0-9]*(-[a-z0-9]+)*)$",
      { message: "Use kebab-case custom properties." },
    ],
    // This rule compares selectors without knowing the DOM, so it flags rules for different elements that share a
    // class name (a commit title in a result row and in the preview header). Order the cascade by section instead.
    "no-descending-specificity": null,
  },
};
