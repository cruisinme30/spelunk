// Messaging with the extension host. The only place that
// touches acquireVsCodeApi, so everything else is plain DOM code.
import type { HostToWebview, WebviewToHost } from "./protocol.gen";

interface VsCodeApi {
  postMessage(message: unknown): void;
  getState(): unknown;
  setState(state: unknown): void;
}
declare function acquireVsCodeApi(): VsCodeApi;

const vscode = acquireVsCodeApi();

/** A message from the host, discriminated by `type`. */
export type HostMessage = {
  [K in keyof HostToWebview]: { v: 1; type: K; payload: HostToWebview[K] };
}[keyof HostToWebview];

/** Sends a message to the extension host. */
export function send<T extends keyof WebviewToHost>(type: T, payload: WebviewToHost[T]): void {
  vscode.postMessage({ v: 1, type, payload });
}

/** Calls `handler` for every well-formed message from the host. */
export function onHostMessage(handler: (message: HostMessage) => void): void {
  window.addEventListener("message", (event: MessageEvent<HostMessage>) => {
    if (event.data?.v === 1) handler(event.data);
  });
}

/** Keeps the unsent query text across webview reloads (VS Code's webview state). */
export function saveDraft(text: string): void {
  vscode.setState({ text });
}

export function loadDraft(): string | undefined {
  const saved = vscode.getState() as { text?: string } | undefined;
  return saved?.text;
}
