// The HTML shell of the webviews: its content security policy, and that
// nothing interpolated into it can break out of its element or attribute.
import assert from "node:assert/strict";
import { test } from "node:test";
import type * as vscode from "vscode";
import { webviewPage } from "../webviewPage";

/** Just enough of vscode.Uri for webviewPage: a path, with(), and toString(). */
class FakeUri {
  constructor(readonly path: string) {}
  with(change: { path?: string }): FakeUri {
    return new FakeUri(change.path ?? this.path);
  }
  toString(): string {
    return `https://webview.test${this.path}`;
  }
}

const CSP_SOURCE = "https://*.vscode-cdn.test";
const webview = {
  cspSource: CSP_SOURCE,
  asWebviewUri: (uri: vscode.Uri) => uri,
};

const page = (root: string, title: string) =>
  webviewPage(webview, new FakeUri(root) as unknown as vscode.Uri, "main.js", title);

test("the page allows only its own nonce-tagged script and the webview's styles and fonts", () => {
  const html = page("/ext/dist/webview", "Spelunk");
  const csp = /http-equiv="Content-Security-Policy" content="([^"]*)"/.exec(html)?.[1] ?? "";
  const nonce = /script-src 'nonce-([^']+)'/.exec(csp)?.[1] ?? "";
  assert.ok(Buffer.from(nonce, "base64").length >= 16, "a nonce of at least 128 random bits");
  assert.match(csp, /^default-src 'none'; /);
  assert.ok(csp.includes(`style-src ${CSP_SOURCE};`), csp);
  assert.doesNotMatch(csp, /unsafe-inline|unsafe-eval/);
  const scripts = [...html.matchAll(/<script([^>]*)>/g)].map((match) => match[1]);
  assert.deepEqual(scripts, [` nonce="${nonce}" src="https://webview.test/ext/dist/webview/main.js"`]);
  assert.match(html, /<link rel="stylesheet" href="https:\/\/webview.test\/ext\/dist\/webview\/main.css">/);
  assert.notEqual(nonce, /'nonce-([^']+)'/.exec(page("/ext/dist/webview", "x"))?.[1], "a new nonce per page");
});

test("the title and resource URIs are escaped, so they cannot inject markup", () => {
  const html = page('/odd "path" <dir>&co', "</title><script>alert(1)</script>");
  assert.equal([...html.matchAll(/<script/g)].length, 1, "only the page's own script tag");
  assert.match(html, /<title>&#60;\/title&#62;&#60;script&#62;alert\(1\)&#60;\/script&#62;<\/title>/);
  assert.match(html, /src="https:\/\/webview.test\/odd &#34;path&#34; &#60;dir&#62;&#38;co\/main.js"/);
});
