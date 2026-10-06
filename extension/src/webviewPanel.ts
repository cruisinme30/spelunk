// Opens an editor tab that hosts one of the webview pages: the search
// panel, the help page or the welcome page.
import * as vscode from "vscode";
import type { WebviewMessage } from "./webviewMessages";
import { parseWebviewMessage } from "./webviewMessages";
import { webviewPage } from "./webviewPage";

/** Which page a tab shows. */
export interface WebviewPanelPage {
  viewType: string;
  title: string;
  /** The page's bundle in dist/webview. */
  script: string;
  retainContextWhenHidden?: boolean;
}

/** Opens `page` in the active column; `onMessage` hears each valid message it posts. */
export function openWebviewPanel(
  extensionUri: vscode.Uri,
  page: WebviewPanelPage,
  onMessage: (message: WebviewMessage) => void,
  disposables?: vscode.Disposable[],
): vscode.WebviewPanel {
  const webviewRoot = vscode.Uri.joinPath(extensionUri, "dist", "webview");
  const panel = vscode.window.createWebviewPanel(page.viewType, page.title, vscode.ViewColumn.Active, {
    enableScripts: true,
    retainContextWhenHidden: page.retainContextWhenHidden ?? false,
    localResourceRoots: [webviewRoot],
  });
  panel.webview.html = webviewPage(panel.webview, webviewRoot, page.script, page.title);
  panel.webview.onDidReceiveMessage(
    (raw: unknown) => {
      const message = parseWebviewMessage(raw);
      if (message) onMessage(message);
    },
    undefined,
    disposables,
  );
  return panel;
}

/** A page's editor tab; at most one is open. Subclasses say what to do with the page's messages. */
export abstract class SingletonPanel implements vscode.Disposable {
  protected panel: vscode.WebviewPanel | undefined;

  /** `page` says which page the tab shows. */
  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly page: WebviewPanelPage,
  ) {}

  /** Opens the page, or reveals the open one. */
  show(): void {
    if (this.panel) {
      this.panel.reveal();
      return;
    }
    const panel = openWebviewPanel(this.extensionUri, this.page, (message) => {
      this.onMessage(message);
    });
    this.panel = panel;
    panel.onDidDispose(() => {
      this.panel = undefined;
    });
  }

  /** Closes the tab when the extension deactivates. */
  dispose(): void {
    this.panel?.dispose();
  }

  /** Hears each valid message the page posts. */
  protected abstract onMessage(message: WebviewMessage): void;
}
