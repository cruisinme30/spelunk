// The help page: the search guide in its own editor tab. Its Try
// buttons run an example in the search panel.
import type * as vscode from "vscode";
import type { WebviewMessage } from "./webviewMessages";
import { openWebviewPanel } from "./webviewPanel";

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
    const panel = openWebviewPanel(
      this.extensionUri,
      { viewType: "spelunk.help", title: "Spelunk · Help", script: "help.js" },
      this.onMessage,
    );
    this.panel = panel;
    panel.onDidDispose(() => {
      this.panel = undefined;
    });
  }

  /** Closes the guide when the extension deactivates. */
  dispose(): void {
    this.panel?.dispose();
  }
}
