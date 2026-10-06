// The help page: the search guide in its own editor tab. Its Try
// buttons run an example in the search panel.
import type * as vscode from "vscode";
import type { WebviewMessage } from "./webviewMessages";
import { SingletonPanel } from "./webviewPanel";

/** The help page's editor tab; at most one is open. */
export class HelpPanel extends SingletonPanel {
  /** `handle` hears the page's Try and settings buttons. */
  constructor(
    extensionUri: vscode.Uri,
    private readonly handle: (message: WebviewMessage) => void,
  ) {
    super(extensionUri, { viewType: "spelunk.help", title: "Spelunk · Help", script: "help.js" });
  }

  protected onMessage(message: WebviewMessage): void {
    this.handle(message);
  }
}
