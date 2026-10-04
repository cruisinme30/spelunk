// The welcome page: shown once after installing, and by Unified Search:
// Show Welcome. It sets the shortcut preset and what to index, and shows
// each repo's indexing as it runs.
import * as vscode from "vscode";
import { MESSAGE_VERSION, type WebviewMessage } from "./controller";
import type { HostToWebview, IndexStatusResult, WelcomeStateMsg as WelcomeState } from "./protocol.gen";
import { webviewPage } from "./webviewPage";

/** The globalState key set once the welcome page has been shown. */
export const WELCOMED_KEY = "unifiedSearch.welcomed";

/** What the page shows, read when it opens and whenever settings change. */
export interface WelcomeSources {
  settings(): WelcomeState;
  indexStatus(): Promise<IndexStatusResult>;
}

/** The welcome page's editor tab; at most one is open. */
export class WelcomePanel implements vscode.Disposable {
  private panel: vscode.WebviewPanel | undefined;

  /** `onMessage` hears the page's choices and buttons. */
  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly sources: WelcomeSources,
    private readonly onMessage: (message: WebviewMessage) => void,
  ) {}

  /** Opens the page, or reveals the open one. */
  show(): void {
    if (this.panel) {
      this.panel.reveal();
      return;
    }
    const webviewRoot = vscode.Uri.joinPath(this.extensionUri, "dist", "webview");
    const panel = vscode.window.createWebviewPanel(
      "unifiedSearch.welcome",
      "Welcome · Unified Search",
      vscode.ViewColumn.Active,
      { enableScripts: true, localResourceRoots: [webviewRoot] },
    );
    this.panel = panel;
    panel.webview.html = webviewPage(panel.webview, webviewRoot, "welcome.js", "Welcome · Unified Search");
    panel.webview.onDidReceiveMessage((message: WebviewMessage) => {
      if (message.type === "ready") void this.sendState();
      else this.onMessage(message);
    });
    panel.onDidDispose(() => {
      this.panel = undefined;
    });
  }

  /** Sends the settings again, after they changed. */
  refresh(): void {
    this.post("welcome.state", this.sources.settings());
  }

  /** Forwards indexing progress while the page is open. */
  indexStatus(status: IndexStatusResult): void {
    this.post("index.status", status);
  }

  /** Closes the page. */
  dispose(): void {
    this.panel?.dispose();
  }

  private async sendState(): Promise<void> {
    this.refresh();
    this.indexStatus(await this.sources.indexStatus());
  }

  private post<K extends keyof HostToWebview>(type: K, payload: HostToWebview[K]): void {
    void this.panel?.webview.postMessage({ v: MESSAGE_VERSION, type, payload });
  }
}
