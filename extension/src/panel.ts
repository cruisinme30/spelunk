// The search panel: a webview panel that hosts webview/ under
// a strict content security policy.
import * as vscode from "vscode";
import { MESSAGE_VERSION, type WebviewMessage } from "./webviewMessages";
import type { HostToWebview } from "./protocol.gen";
import { openWebviewPanel } from "./webviewPanel";

interface QueuedMessage {
  type: keyof HostToWebview;
  payload: unknown;
}

/** The search panel's editor tab. Messages to it wait until its webview says "ready". */
export class SearchPanel implements vscode.Disposable {
  private panel: vscode.WebviewPanel | undefined;
  private panelDisposables: vscode.Disposable[] = [];
  /** Messages posted before the webview said "ready". */
  private pendingMessages: QueuedMessage[] = [];
  private ready = false;

  /** `onOpenChanged` hears when the panel opens or closes, for the panelOpen context key. */
  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly onMessage: (message: WebviewMessage) => void,
    private readonly onOpenChanged: (open: boolean) => void,
  ) {}

  /** Whether the panel is open (visible or in a background tab). */
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
    const panel = openWebviewPanel(
      this.extensionUri,
      { viewType: "spelunk", title: "Spelunk", script: "main.js", retainContextWhenHidden: true },
      (message) => {
        this.receive(message);
      },
      this.panelDisposables,
    );
    this.panel = panel;
    this.ready = false;
    panel.onDidDispose(
      () => {
        this.onPanelDisposed();
      },
      undefined,
      this.panelDisposables,
    );
    this.onOpenChanged(true);
  }

  /** Closes the panel; the controller keeps the query and results. */
  close(): void {
    this.panel?.dispose();
  }

  /** Sends a message to the webview, or queues it until the webview is ready. Dropped while closed. */
  post<T extends keyof HostToWebview>(type: T, payload: HostToWebview[T]): void {
    if (!this.panel) return;
    if (this.ready) {
      void this.panel.webview.postMessage({ v: MESSAGE_VERSION, type, payload });
      return;
    }
    // Only the newest state.restore matters; everything else waits in order.
    if (type === "state.restore") {
      this.pendingMessages = this.pendingMessages.filter((queued) => queued.type !== "state.restore");
    }
    this.pendingMessages.push({ type, payload });
  }

  /** Closes the panel when the extension deactivates. */
  dispose(): void {
    this.panel?.dispose();
  }

  private receive(message: WebviewMessage): void {
    if (message.type === "ready" && this.panel) {
      this.ready = true;
      for (const queued of this.pendingMessages.splice(0)) {
        void this.panel.webview.postMessage({ v: MESSAGE_VERSION, ...queued });
      }
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
