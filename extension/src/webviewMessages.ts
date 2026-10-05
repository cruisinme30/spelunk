// Checks what a webview posts before the host acts on it. postMessage carries
// any JSON, so a message whose shape doesn't match the protocol is dropped
// here instead of failing (or doing something odd) deep inside a handler.
import { MESSAGE_VERSION, type WebviewMessage } from "./controller";
import type { WebviewToHost } from "./protocol.gen";

type Fields = Record<string, unknown>;
/** Returns the checked payload of one message type, or undefined when its shape is wrong. */
type PayloadCheck<K extends keyof WebviewToHost> = (payload: Fields) => WebviewToHost[K] | undefined;

const isString = (value: unknown): value is string => typeof value === "string";
const isCount = (value: unknown): value is number => Number.isSafeInteger(value) && (value as number) >= 0;
const noPayload = () => ({});

/** `value` when it is one of `allowed`. */
function member<T extends string>(value: unknown, allowed: readonly T[]): T | undefined {
  return allowed.find((candidate) => candidate === value);
}

const PRESETS = ["quickOpen", "findInFiles", "none"] as const;
const HISTORY_DEPTHS = ["6m", "2y", "all"] as const;

const PAYLOAD_CHECKS: { [K in keyof WebviewToHost]: PayloadCheck<K> } = {
  "query.changed": ({ text, cursor, seq, asTyped }) => {
    if (!isString(text) || !isCount(seq) || typeof cursor !== "number" || Number.isNaN(cursor)) return;
    // The cursor is a UTF-16 offset into text; one outside it would confuse the parser's completions.
    const inText = Math.min(text.length, Math.max(0, Math.trunc(cursor)));
    return { text, cursor: inText, seq, ...(asTyped === true ? { asTyped } : {}) };
  },
  "result.select": ({ ref }) => (isString(ref) ? { ref } : undefined),
  "result.open": ({ ref, where }) => {
    const side = member(where, ["current", "side"] as const);
    return isString(ref) && side ? { ref, where: side } : undefined;
  },
  "results.more": ({ searchId, cursor }) => (isString(searchId) && isString(cursor) ? { searchId, cursor } : undefined),
  "help.try": ({ query }) => (isString(query) ? { query } : undefined),
  "welcome.choose": ({ preset, historyDepth, symbols }) => {
    const choice: WebviewToHost["welcome.choose"] = {};
    const checkedPreset = member(preset, PRESETS);
    const checkedDepth = member(historyDepth, HISTORY_DEPTHS);
    if (checkedPreset) choice.preset = checkedPreset;
    if (checkedDepth) choice.historyDepth = checkedDepth;
    if (typeof symbols === "boolean") choice.symbols = symbols;
    return choice;
  },
  "panel.close": noPayload,
  "help.open": noPayload,
  "settings.open": noPayload,
  "welcome.shortcut": noPayload,
  "welcome.start": noPayload,
  ready: noPayload,
  "daemon.restart": noPayload,
};

/** The message, with a checked payload, or undefined when it isn't a well-formed message of a known type. */
export function parseWebviewMessage(raw: unknown): WebviewMessage | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const { v, type, payload } = raw as Fields;
  if (v !== MESSAGE_VERSION || !isString(type) || !Object.hasOwn(PAYLOAD_CHECKS, type)) return undefined;
  if (typeof payload !== "object" || payload === null || Array.isArray(payload)) return undefined;
  const check = PAYLOAD_CHECKS[type as keyof WebviewToHost] as PayloadCheck<keyof WebviewToHost>;
  const checked = check(payload as Fields);
  return checked === undefined ? undefined : ({ v: MESSAGE_VERSION, type, payload: checked } as WebviewMessage);
}
