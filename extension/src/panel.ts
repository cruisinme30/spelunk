// The search panel: a webview panel that hosts webview/ (Contract 2) under
// a strict content security policy.
import * as vscode from "vscode";
import type { WebviewMessage } from "./controller";
import type { HostToWebview } from "./protocol.gen";
import { webviewPage } from "./webviewPage";

interface QueuedMessage {
  type: keyof HostToWebview;
  payload: unknown;
}

export class SearchPanel implements vscode.Disposable {
  private panel?: vscode.WebviewPanel;
  private panelDisposables: vscode.Disposable[] = [];
  /** Messages posted before the webview said "ready". */
  private pendingMessages: QueuedMessage[] = [];
  private ready = false;

  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly onMessage: (message: WebviewMessage) => void,
    private readonly onOpenChanged: (open: boolean) => void,
  ) {}

  get isOpen(): boolean {
    return this.panel !== undefined;
  }

  /** Opens the panel, or reveals and focuses the open one. */
  show(): void {
    if (this.panel) {
      this.panel.reveal(vscode.ViewColumn.Active, false);
      this.post("focus", {});
      return;
    }
    const webviewRoot = vscode.Uri.joinPath(this.extensionUri, "dist", "webview");
    const panel = vscode.window.createWebviewPanel("unifiedSearch", "Unified Search", vscode.ViewColumn.Active, {
      enableScripts: true,
      retainContextWhenHidden: true,
      localResourceRoots: [webviewRoot],
    });
    this.panel = panel;
    this.ready = false;
    panel.webview.html = webviewPage(panel.webview, webviewRoot, "main.js", "Unified Search");
    panel.webview.onDidReceiveMessage(
      (message: WebviewMessage) => this.receive(message),
      undefined,
      this.panelDisposables,
    );
    panel.onDidDispose(() => this.onPanelDisposed(), undefined, this.panelDisposables);
    this.onOpenChanged(true);
  }

  /** Closes the panel; the controller keeps the query and results. */
  close(): void {
    this.panel?.dispose();
  }

  post<T extends keyof HostToWebview>(type: T, payload: HostToWebview[T]): void {
    if (!this.panel) return;
    if (this.ready) {
      void this.panel.webview.postMessage({ v: 1, type, payload });
      return;
    }
    // Only the newest state.restore matters; everything else waits in order.
    if (type === "state.restore") this.pendingMessages = this.pendingMessages.filter((m) => m.type !== "state.restore");
    this.pendingMessages.push({ type, payload });
  }

  dispose(): void {
    this.panel?.dispose();
  }

  private receive(message: WebviewMessage): void {
    if (message?.type === "ready" && this.panel) {
      this.ready = true;
      for (const queued of this.pendingMessages.splice(0)) void this.panel.webview.postMessage({ v: 1, ...queued });
    }
    this.onMessage(message);
  }

  private onPanelDisposed(): void {
    this.panel = undefined;
    this.ready = false;
    this.pendingMessages = [];
    for (const disposable of this.panelDisposables.splice(0)) disposable.dispose();
    this.onOpenChanged(false);
  }
}
