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

type Fields = Record<string, unknown>;

const isObject = (value: unknown): value is Fields => typeof value === "object" && value !== null;
const isNumber = (value: unknown): value is number => typeof value === "number" && Number.isFinite(value);
const isString = (value: unknown): value is string => typeof value === "string";
const isObjectArray = (value: unknown): value is Fields[] =>
  Array.isArray(value) && value.every((item) => isObject(item));

/**
 * Per message type, whether a payload has the fields the webviews keep or
 * iterate. A payload that fails is dropped whole, so a malformed message can
 * neither throw halfway through rendering nor leave broken state behind
 * (index.status without repos would break every later render).
 */
const HAS_REQUIRED_FIELDS: Record<keyof HostToWebview, (payload: Fields) => boolean> = {
  "parse.result": ({ seq, query, completions }) =>
    isNumber(seq) &&
    isObject(query) &&
    isString(query["raw"]) &&
    isObjectArray(query["diagnostics"]) &&
    isObject(query["globals"]) &&
    isObjectArray(completions),
  "search.batch": ({ seq, searchId, items }) => isNumber(seq) && isString(searchId) && isObjectArray(items),
  "search.done": ({ seq, searchId, hidden }) => isNumber(seq) && isString(searchId) && isObjectArray(hidden),
  "preview.result": ({ ref, preview }) => isString(ref) && (preview === null || isObject(preview)),
  "index.status": ({ repos }) => isObjectArray(repos) && repos.every((repo) => isString(repo["name"])),
  "state.restore": ({ text, recent, settings }) =>
    isString(text) &&
    Array.isArray(recent) &&
    recent.every((query) => isString(query)) &&
    (settings === undefined || isObject(settings)),
  banner: ({ state }) => isString(state),
  focus: () => true,
  "welcome.state": ({ preset, historyDepth }) => isString(preset) && isString(historyDepth),
};

/** Whether `data` is a host message this webview speaks: the right version, a known type and a well-formed payload. */
function isHostMessage(data: unknown): data is HostMessage {
  if (!isObject(data) || data["v"] !== MESSAGE_VERSION || !isObject(data["payload"])) return false;
  const type = data["type"];
  if (!isString(type) || !Object.hasOwn(HAS_REQUIRED_FIELDS, type)) return false; // a newer host's message
  return HAS_REQUIRED_FIELDS[type as keyof HostToWebview](data["payload"]);
}

/** Sends a message to the extension host. */
export function send<T extends keyof WebviewToHost>(type: T, payload: WebviewToHost[T]): void {
  vscode.postMessage({ v: MESSAGE_VERSION, type, payload });
}

/** Calls `handler` for every well-formed message from the host with the version this webview speaks. */
export function onHostMessage(handler: (message: HostMessage) => void): void {
  window.addEventListener("message", (event: MessageEvent<unknown>) => {
    if (isHostMessage(event.data)) handler(event.data);
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
