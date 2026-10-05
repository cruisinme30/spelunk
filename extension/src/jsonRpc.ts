// JSON-RPC 2.0 client for the daemon, with LSP-style Content-Length framing.
// Pure Node, no vscode import, so it is tested against the real daemon.
import { EventEmitter } from "node:events";
import type { Readable, Writable } from "node:stream";
import type { RpcNotifications, RpcRequests } from "./protocol.gen";

/** An error response from the peer. `code` is one of protocol.gen's ErrorCodes or a JSON-RPC code. */
export class RpcError extends Error {
  /** `data` is the error's optional extra detail, as the peer sent it. */
  constructor(
    readonly code: number,
    message: string,
    readonly data?: unknown,
  ) {
    super(message);
  }
}

const HEADER_END = "\r\n\r\n";

/** Splits a byte stream into Content-Length framed message bodies. */
export class FrameDecoder {
  private buffered = Buffer.alloc(0);

  /** `onBadFrame` hears about each header without a Content-Length; that header is skipped. */
  constructor(private readonly onBadFrame: (header: string) => void = () => {}) {}

  /** Adds a chunk and returns every complete body it finished. */
  push(chunk: Buffer): string[] {
    this.buffered = Buffer.concat([this.buffered, chunk]);
    const bodies: string[] = [];
    for (;;) {
      const headerEnd = this.buffered.indexOf(HEADER_END);
      if (headerEnd === -1) break;
      const header = this.buffered.subarray(0, headerEnd).toString("ascii");
      const bodyStart = headerEnd + HEADER_END.length;
      const lengthMatch = /content-length:\s*(\d+)/i.exec(header);
      if (!lengthMatch) {
        this.buffered = this.buffered.subarray(bodyStart); // skip the bad header so the stream can recover
        this.onBadFrame(header);
        continue;
      }
      const bodyLength = Number(lengthMatch[1]);
      if (this.buffered.length < bodyStart + bodyLength) break;
      bodies.push(this.buffered.subarray(bodyStart, bodyStart + bodyLength).toString("utf8"));
      this.buffered = this.buffered.subarray(bodyStart + bodyLength);
    }
    return bodies;
  }
}

/** Frames one message body for the wire. */
export function encodeFrame(body: string): Buffer {
  const bytes = Buffer.from(body, "utf8");
  return Buffer.concat([Buffer.from(`Content-Length: ${bytes.length}${HEADER_END}`, "ascii"), bytes]);
}

/** Lets a caller cancel a request it started. */
export interface CancelToken {
  onCancel(listener: () => void): void;
}

/** A minimal cancellation source, usable without vscode. */
export class CancelSource {
  private listeners: (() => void)[] = [];
  private isCancelled = false;

  readonly token: CancelToken = {
    onCancel: (listener) => {
      if (this.isCancelled) listener();
      else this.listeners.push(listener);
    },
  };

  /** Whether cancel() has been called. */
  get cancelled(): boolean {
    return this.isCancelled;
  }

  /** Cancels once: tells every listener, then ignores further calls. */
  cancel(): void {
    if (this.isCancelled) return;
    this.isCancelled = true;
    for (const listener of this.listeners.splice(0)) listener();
  }
}

interface PendingRequest {
  resolve(result: unknown): void;
  reject(error: unknown): void;
}

/** JSON-RPC's code for an internal error, used when a peer's error has no usable code. */
const INTERNAL_ERROR = -32_603;

/** The RpcError for a response's `error` member, whatever shape the peer gave it. */
function errorFromWire(error: unknown): RpcError {
  if (typeof error !== "object" || error === null) return new RpcError(INTERNAL_ERROR, String(error));
  const { code, message, data } = error as Record<string, unknown>;
  return new RpcError(
    typeof code === "number" && Number.isInteger(code) ? code : INTERNAL_ERROR,
    typeof message === "string" ? message : "request failed",
    data,
  );
}

/** A client connection. Requests and notifications are typed by the generated protocol tables. */
export class Connection extends EventEmitter {
  private nextId = 1;
  private readonly pending = new Map<number, PendingRequest>();
  private readonly decoder: FrameDecoder;
  private closed = false;

  /**
   * Emits "error" for each problem with what the peer sent (it is skipped)
   * and "close" once. Problems with no "error" listener are dropped rather
   * than thrown, since they surface inside a stream event.
   */
  constructor(
    input: Readable,
    private readonly output: Writable,
  ) {
    super();
    this.decoder = new FrameDecoder((header) => {
      this.report(new Error(`jsonrpc: skipped a frame without a usable Content-Length: ${JSON.stringify(header)}`));
    });
    input.on("data", (chunk: Buffer) => {
      this.onData(chunk);
    });
    input.on("close", () => {
      this.dispose(new Error("connection closed"));
    });
    input.on("error", (error) => {
      this.dispose(error);
      this.report(error);
    });
    // Writing to a process that just exited fails asynchronously (EPIPE).
    output.on("error", (error) => {
      this.dispose(error);
      this.report(error);
    });
  }

  /** Sends a request. Cancelling `token` sends $/cancelRequest; the promise then rejects with RequestCancelled. */
  request<M extends keyof RpcRequests>(
    method: M,
    params: RpcRequests[M][0],
    token?: CancelToken,
  ): Promise<RpcRequests[M][1]> {
    if (this.closed) return Promise.reject(new Error("connection closed"));
    const id = this.nextId++;
    const response = new Promise<RpcRequests[M][1]>((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
    });
    this.write({ jsonrpc: "2.0", id, method, params });
    token?.onCancel(() => {
      if (this.pending.has(id)) this.write({ jsonrpc: "2.0", method: "$/cancelRequest", params: { id } });
    });
    return response;
  }

  /** Sends a notification; dropped once the connection is closed. */
  notify<M extends keyof RpcNotifications>(method: M, params: RpcNotifications[M]): void {
    if (!this.closed) this.write({ jsonrpc: "2.0", method, params });
  }

  /** Calls `listener` with the params of every `method` notification from the peer. */
  onNotification<M extends keyof RpcNotifications>(method: M, listener: (params: RpcNotifications[M]) => void): void {
    this.on(`notify:${method}`, listener);
  }

  /** Fails every pending request with `reason` and stops sending. */
  dispose(reason = new Error("connection disposed")): void {
    if (this.closed) return;
    this.closed = true;
    const requests = [...this.pending.values()];
    this.pending.clear();
    for (const request of requests) request.reject(reason);
    this.emit("close");
  }

  /** Tells "error" listeners about a problem, if there are any. */
  private report(error: unknown): void {
    if (this.listenerCount("error") > 0) this.emit("error", error);
  }

  private onData(chunk: Buffer): void {
    if (this.closed) return; // a replaced daemon's last words: its searches are no longer wanted
    for (const body of this.decoder.push(chunk)) {
      // One bad message (or a listener that throws on it) must not lose the ones after it.
      try {
        this.receive(body);
      } catch (error) {
        this.report(error);
      }
    }
  }

  private write(message: object): void {
    // A write to a stream that is already destroyed fails only through this callback.
    this.output.write(encodeFrame(JSON.stringify(message)), (error) => {
      if (error) this.dispose(error);
    });
  }

  private receive(body: string): void {
    const message: unknown = JSON.parse(body);
    if (typeof message !== "object" || message === null || Array.isArray(message)) {
      throw new Error(`jsonrpc: not a message: ${body.slice(0, 80)}`);
    }
    const { id, method, params, result, error } = message as Record<string, unknown>;
    if (typeof method === "string") {
      this.emit(`notify:${method}`, params);
      return;
    }
    const request = typeof id === "number" ? this.pending.get(id) : undefined;
    if (!request) return; // an answer to a request this side no longer waits for
    this.pending.delete(id as number);
    if (error !== undefined && error !== null) request.reject(errorFromWire(error));
    else request.resolve(result);
  }
}
