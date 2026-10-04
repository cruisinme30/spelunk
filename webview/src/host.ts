// Messaging with the extension host. The only place that
// touches acquireVsCodeApi, so everything else is plain DOM code.
import type { Envelope, HostToWebview, WebviewToHost } from "./protocol.gen";

/** The `v` of every message between the webview and the extension host. */
const MESSAGE_VERSION: Envelope["v"] = 1;

interface VsCodeApi {
  postMessage(message: unknown): void;
  getState(): unknown;
  setState(state: unknown): void;
}
declare function acquireVsCodeApi(): VsCodeApi;

const vscode = acquireVsCodeApi();

/** A message from the host, discriminated by `type`. */
export type HostMessage = {
  [K in keyof HostToWebview]: { v: typeof MESSAGE_VERSION; type: K; payload: HostToWebview[K] };
}[keyof HostToWebview];

/** Sends a message to the extension host. */
export function send<T extends keyof WebviewToHost>(type: T, payload: WebviewToHost[T]): void {
  vscode.postMessage({ v: MESSAGE_VERSION, type, payload });
}

/** Calls `handler` for every message from the host with the version this webview speaks. */
export function onHostMessage(handler: (message: HostMessage) => void): void {
  window.addEventListener("message", (event: MessageEvent<HostMessage | undefined>) => {
    if (event.data?.v === MESSAGE_VERSION) handler(event.data);
  });
}

/** Keeps the unsent query text across webview reloads (VS Code's webview state). */
export function saveDraft(text: string): void {
  vscode.setState({ text });
}

/** The query text saveDraft kept, if any. */
export function loadDraft(): string | undefined {
  const saved = vscode.getState() as { text?: string } | undefined;
  return saved?.text;
}
