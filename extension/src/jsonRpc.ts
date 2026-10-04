// JSON-RPC 2.0 client for the daemon, with LSP-style Content-Length framing.
// Pure Node, no vscode import, so it is tested against the real daemon.
import { EventEmitter } from "node:events";
import type { Readable, Writable } from "node:stream";
import type { RpcNotifications, RpcRequests } from "./protocol.gen";

/** An error response from the peer. `code` is one of protocol.gen's ErrorCodes or a JSON-RPC code. */
export class RpcError extends Error {
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

  /** Adds a chunk and returns every complete body it finished. Throws on a frame without Content-Length, after dropping it. */
  push(chunk: Buffer): string[] {
    this.buffered = Buffer.concat([this.buffered, chunk]);
    const bodies: string[] = [];
    for (;;) {
      const headerEnd = this.buffered.indexOf(HEADER_END);
      if (headerEnd < 0) break;
      const header = this.buffered.subarray(0, headerEnd).toString("ascii");
      const bodyStart = headerEnd + HEADER_END.length;
      const lengthMatch = /content-length:\s*(\d+)/i.exec(header);
      if (!lengthMatch) {
        this.buffered = this.buffered.subarray(bodyStart); // drop the bad header so the stream can recover
        throw new Error("jsonrpc: frame without Content-Length");
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

  get cancelled(): boolean {
    return this.isCancelled;
  }

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

interface WireMessage {
  id?: number;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: { code: number; message: string; data?: unknown };
}

/** A client connection. Requests and notifications are typed by the generated protocol tables. */
export class Connection extends EventEmitter {
  private nextId = 1;
  private readonly pending = new Map<number, PendingRequest>();
  private readonly decoder = new FrameDecoder();
  private closed = false;

  constructor(
    input: Readable,
    private readonly output: Writable,
    private readonly trace?: (direction: "send" | "recv", body: string) => void,
  ) {
    super();
    input.on("data", (chunk: Buffer) => this.onData(chunk));
    input.on("close", () => this.dispose(new Error("connection closed")));
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
      this.pending.set(id, { resolve: resolve as (result: unknown) => void, reject });
    });
    this.write({ jsonrpc: "2.0", id, method, params });
    token?.onCancel(() => {
      if (this.pending.has(id)) this.write({ jsonrpc: "2.0", method: "$/cancelRequest", params: { id } });
    });
    return response;
  }

  notify<M extends keyof RpcNotifications>(method: M, params: RpcNotifications[M]): void {
    if (!this.closed) this.write({ jsonrpc: "2.0", method, params });
  }

  onNotification<M extends keyof RpcNotifications>(method: M, listener: (params: RpcNotifications[M]) => void): void {
    this.on(`notify:${method}`, listener);
  }

  /** Fails every pending request with `reason` and stops sending. */
  dispose(reason = new Error("connection disposed")): void {
    if (this.closed) return;
    this.closed = true;
    for (const request of this.pending.values()) request.reject(reason);
    this.pending.clear();
    this.emit("close");
  }

  private onData(chunk: Buffer): void {
    let bodies: string[];
    try {
      bodies = this.decoder.push(chunk);
    } catch (error) {
      this.emit("error", error);
      return;
    }
    for (const body of bodies) this.receive(body);
  }

  private write(message: object): void {
    const body = JSON.stringify(message);
    this.trace?.("send", body);
    this.output.write(encodeFrame(body));
  }

  private receive(body: string): void {
    this.trace?.("recv", body);
    let message: WireMessage;
    try {
      message = JSON.parse(body) as WireMessage;
    } catch (error) {
      this.emit("error", error);
      return;
    }
    if (message.method) {
      this.emit(`notify:${message.method}`, message.params);
      return;
    }
    if (typeof message.id !== "number") return;
    const request = this.pending.get(message.id);
    if (!request) return;
    this.pending.delete(message.id);
    if (message.error) request.reject(new RpcError(message.error.code, message.error.message, message.error.data));
    else request.resolve(message.result);
  }
}
