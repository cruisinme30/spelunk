// The HTML shell of every webview: one bundled script and the shared
// stylesheet, under a strict content security policy. It imports only
// vscode's types, so tests can build a page in plain Node.
import { randomBytes } from "node:crypto";
import { posix } from "node:path";
import type * as vscode from "vscode";

/** Escapes text for HTML element content and quoted attribute values. */
function escapeHtml(text: string): string {
  return text.replaceAll(/[&<>"']/g, (character) => `&#${character.codePointAt(0) ?? 0};`);
}

/** A page that loads `script` from `webviewRoot` (dist/webview) with main.css. */
export function webviewPage(
  webview: Pick<vscode.Webview, "asWebviewUri" | "cspSource">,
  webviewRoot: vscode.Uri,
  script: string,
  title: string,
): string {
  const nonce = randomBytes(16).toString("base64");
  const resource = (name: string) =>
    webview.asWebviewUri(webviewRoot.with({ path: posix.join(webviewRoot.path, name) })).toString();
  const csp = [
    "default-src 'none'",
    `style-src ${webview.cspSource}`,
    `script-src 'nonce-${nonce}'`,
    `font-src ${webview.cspSource}`,
  ].join("; ");
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${csp};">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="${escapeHtml(resource("main.css"))}">
<title>${escapeHtml(title)}</title>
</head>
<body>
<div id="app"></div>
<script nonce="${nonce}" src="${escapeHtml(resource(script))}"></script>
</body>
</html>`;
}
