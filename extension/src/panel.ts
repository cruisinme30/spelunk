// The search panel: a webview panel hosting webview/ (Contract 2).
import * as vscode from "vscode";
import type { HostToWebview } from "./protocol.gen";

export class SearchPanel {
  private panel?: vscode.WebviewPanel;
  private queue: { type: string; payload: unknown }[] = [];
  private ready = false;
  private readonly subs: vscode.Disposable[] = [];

  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly onMessage: (msg: any) => void,
    private readonly onVisibility: (open: boolean) => void,
  ) {}

  get isOpen(): boolean {
    return this.panel !== undefined;
  }

  show(): void {
    if (this.panel) {
      this.panel.reveal(vscode.ViewColumn.Active, false);
      this.post("focus", {});
      return;
    }
    const root = vscode.Uri.joinPath(this.extensionUri, "dist", "webview");
    const panel = vscode.window.createWebviewPanel("unifiedSearch", "Unified Search", vscode.ViewColumn.Active, {
      enableScripts: true,
      retainContextWhenHidden: true,
      localResourceRoots: [root],
    });
    this.panel = panel;
    this.ready = false;
    panel.webview.html = this.html(panel.webview, root);
    panel.webview.onDidReceiveMessage(
      (msg) => {
        if (msg?.type === "ready") {
          this.ready = true;
          for (const m of this.queue.splice(0)) void panel.webview.postMessage({ v: 1, ...m });
        }
        this.onMessage(msg);
      },
      undefined,
      this.subs,
    );
    panel.onDidDispose(
      () => {
        this.panel = undefined;
        this.ready = false;
        this.queue = [];
        this.onVisibility(false);
      },
      undefined,
      this.subs,
    );
    this.onVisibility(true);
  }

  hide(): void {
    this.panel?.dispose();
  }

  post<T extends keyof HostToWebview>(type: T, payload: HostToWebview[T]): void {
    if (!this.panel) return;
    if (!this.ready) {
      // Only the newest state.restore matters; everything else queues.
      if (type === "state.restore") this.queue = this.queue.filter((m) => m.type !== "state.restore");
      this.queue.push({ type, payload });
      return;
    }
    void this.panel.webview.postMessage({ v: 1, type, payload });
  }

  private html(webview: vscode.Webview, root: vscode.Uri): string {
    const nonce = Array.from({ length: 32 }, () => Math.floor(Math.random() * 36).toString(36)).join("");
    const js = webview.asWebviewUri(vscode.Uri.joinPath(root, "main.js"));
    const css = webview.asWebviewUri(vscode.Uri.joinPath(root, "main.css"));
    return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${webview.cspSource}; script-src 'nonce-${nonce}'; font-src ${webview.cspSource};">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="${css}">
<title>Unified Search</title>
</head>
<body>
<div id="app"></div>
<script nonce="${nonce}" src="${js}"></script>
</body>
</html>`;
  }

  dispose(): void {
    this.panel?.dispose();
    for (const s of this.subs) s.dispose();
  }
}
