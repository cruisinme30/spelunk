// The welcome page: shown once after installing, and by Spelunk:
// Show Welcome. It sets the shortcut preset and what to index, and shows
// each repo's indexing as it runs.
import type * as vscode from "vscode";
import { MESSAGE_VERSION, type WebviewMessage } from "./webviewMessages";
import type { HostToWebview, IndexStatusResult, WelcomeStateMessage as WelcomeState } from "./protocol.gen";
import { SingletonPanel } from "./webviewPanel";

/** The globalState key set once the welcome page has been shown. */
export const WELCOMED_KEY = "spelunk.welcomed";

/** What the page shows, read when it opens and whenever settings change. */
export interface WelcomeSources {
  settings(): WelcomeState;
  indexStatus(): Promise<IndexStatusResult>;
}

/** The welcome page's editor tab; at most one is open. */
export class WelcomePanel extends SingletonPanel {
  /** `handle` hears the page's choices and buttons. */
  constructor(
    extensionUri: vscode.Uri,
    private readonly sources: WelcomeSources,
    private readonly handle: (message: WebviewMessage) => void,
  ) {
    super(extensionUri, { viewType: "spelunk.welcome", title: "Welcome · Spelunk", script: "welcome.js" });
  }

  protected onMessage(message: WebviewMessage): void {
    if (message.type === "ready") void this.sendState();
    else this.handle(message);
  }

  /** Sends the settings again, after they changed. */
  refresh(): void {
    this.post("welcome.state", this.sources.settings());
  }

  /** Forwards indexing progress while the page is open. */
  indexStatus(status: IndexStatusResult): void {
    this.post("index.status", status);
  }

  private async sendState(): Promise<void> {
    this.refresh();
    try {
      this.indexStatus(await this.sources.indexStatus());
    } catch {
      // The daemon isn't ready (or is stopped); its index/progress brings the status once it is.
    }
  }

  private post<K extends keyof HostToWebview>(type: K, payload: HostToWebview[K]): void {
    void this.panel?.webview.postMessage({ v: MESSAGE_VERSION, type, payload });
  }
}
