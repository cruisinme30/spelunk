// The help page: the search guide in its own editor tab. Its Try
// buttons run an example in the search panel.
import * as vscode from "vscode";
import type { WebviewMessage } from "./webviewMessages";
import { parseWebviewMessage } from "./webviewMessages";
import { webviewPage } from "./webviewPage";

/** The help page's editor tab; at most one is open. */
export class HelpPanel implements vscode.Disposable {
  private panel: vscode.WebviewPanel | undefined;

  /** `onMessage` hears the page's Try and settings buttons. */
  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly onMessage: (message: WebviewMessage) => void,
  ) {}

  /** Opens the guide, or reveals the open one. */
  show(): void {
    if (this.panel) {
      this.panel.reveal();
      return;
    }
    const webviewRoot = vscode.Uri.joinPath(this.extensionUri, "dist", "webview");
    const panel = vscode.window.createWebviewPanel(
      "unifiedSearch.help",
      "Unified Search · Help",
      vscode.ViewColumn.Active,
      {
        enableScripts: true,
        localResourceRoots: [webviewRoot],
      },
    );
    this.panel = panel;
    panel.webview.html = webviewPage(panel.webview, webviewRoot, "help.js", "Unified Search · Help");
    panel.webview.onDidReceiveMessage((raw: unknown) => {
      const message = parseWebviewMessage(raw);
      if (message) this.onMessage(message);
    });
    panel.onDidDispose(() => {
      this.panel = undefined;
    });
  }

  /** Closes the guide when the extension deactivates. */
  dispose(): void {
    this.panel?.dispose();
  }
}
