// The HTML shell of every webview: one bundled script and the shared
// stylesheet, under a strict content security policy.
import { randomBytes } from "node:crypto";
import * as vscode from "vscode";

/** A page that loads `script` from `webviewRoot` (dist/webview) with main.css. */
export function webviewPage(webview: vscode.Webview, webviewRoot: vscode.Uri, script: string, title: string): string {
  const nonce = randomBytes(16).toString("base64");
  const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, script));
  const styles = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, "main.css"));
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
<link rel="stylesheet" href="${styles}">
<title>${title}</title>
</head>
<body>
<div id="app"></div>
<script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
}
