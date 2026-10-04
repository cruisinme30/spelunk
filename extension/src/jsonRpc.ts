// JSON-RPC 2.0 with LSP-style Content-Length framing (Contract 3).
// Pure Node: no vscode import, so it is testable against the real daemon.
import { EventEmitter } from "node:events";
import type { Readable, Writable } from "node:stream";
import type { RpcNotifications, RpcRequests } from "./protocol.gen";

export class RpcError extends Error {
  constructor(
    public readonly code: number,
    message: string,
    public readonly data?: unknown,
  ) {
    super(message);
  }
}

export const ErrorCodes = {
  RequestCancelled: -32800,
  QueryInvalid: 1001,
  IndexNotReady: 1002,
  RefStale: 1003,
  IndexCorrupt: 1004,
  Overloaded: 1005,
} as const;

/** Splits a byte stream into Content-Length framed message bodies. */
export class FrameDecoder {
  private buf = Buffer.alloc(0);
  push(chunk: Buffer): string[] {
    this.buf = Buffer.concat([this.buf, chunk]);
    const out: string[] = [];
    for (;;) {
      const headerEnd = this.buf.indexOf("\r\n\r\n");
      if (headerEnd < 0) break;
      const header = this.buf.subarray(0, headerEnd).toString("ascii");
      const m = /content-length:\s*(\d+)/i.exec(header);
      if (!m) throw new Error("jsonrpc: missing Content-Length");
      const len = Number(m[1]);
      const start = headerEnd + 4;
      if (this.buf.length < start + len) break;
      out.push(this.buf.subarray(start, start + len).toString("utf8"));
      this.buf = this.buf.subarray(start + len);
    }
    return out;
  }
}

export function encodeFrame(body: string): Buffer {
  const b = Buffer.from(body, "utf8");
  return Buffer.concat([Buffer.from(`Content-Length: ${b.length}\r\n\r\n`, "ascii"), b]);
}

interface Pending {
  resolve(v: unknown): void;
  reject(e: unknown): void;
}

/** A client connection. Requests are typed by the generated RpcRequests map. */
export class Connection extends EventEmitter {
  private nextId = 1;
  private pending = new Map<number, Pending>();
  private decoder = new FrameDecoder();
  private closed = false;

  constructor(
    input: Readable,
    private output: Writable,
    private trace?: (dir: "send" | "recv", body: string) => void,
  ) {
    super();
    input.on("data", (chunk: Buffer) => {
      let bodies: string[];
      try {
        bodies = this.decoder.push(chunk);
      } catch (e) {
        this.emit("error", e);
        return;
      }
      for (const body of bodies) this.receive(body);
    });
    input.on("close", () => this.dispose(new Error("connection closed")));
  }

  request<M extends keyof RpcRequests>(
    method: M,
    params: RpcRequests[M][0],
    token?: { onCancel(cb: () => void): void },
  ): Promise<RpcRequests[M][1]> {
    if (this.closed) return Promise.reject(new Error("connection closed"));
    const id = this.nextId++;
    const p = new Promise<RpcRequests[M][1]>((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
    });
    this.write({ jsonrpc: "2.0", id, method, params });
    token?.onCancel(() => {
      if (this.pending.has(id)) this.write({ jsonrpc: "2.0", method: "$/cancelRequest", params: { id } });
    });
    return p;
  }

  notify<M extends keyof RpcNotifications>(method: M, params: RpcNotifications[M]): void {
    if (!this.closed) this.write({ jsonrpc: "2.0", method, params });
  }

  onNotification<M extends keyof RpcNotifications>(method: M, cb: (params: RpcNotifications[M]) => void): void {
    this.on(`notify:${method}`, cb);
  }

  dispose(reason = new Error("connection disposed")): void {
    if (this.closed) return;
    this.closed = true;
    for (const p of this.pending.values()) p.reject(reason);
    this.pending.clear();
    this.emit("close");
  }

  private write(msg: object): void {
    const body = JSON.stringify(msg);
    this.trace?.("send", body);
    this.output.write(encodeFrame(body));
  }

  private receive(body: string): void {
    this.trace?.("recv", body);
    let msg: {
      id?: number;
      method?: string;
      params?: unknown;
      result?: unknown;
      error?: { code: number; message: string; data?: unknown };
    };
    try {
      msg = JSON.parse(body);
    } catch (e) {
      this.emit("error", e);
      return;
    }
    if (msg.method) {
      this.emit(`notify:${msg.method}`, msg.params);
      return;
    }
    if (typeof msg.id !== "number") return;
    const p = this.pending.get(msg.id);
    if (!p) return;
    this.pending.delete(msg.id);
    if (msg.error) p.reject(new RpcError(msg.error.code, msg.error.message, msg.error.data));
    else p.resolve(msg.result);
  }
}

/** A minimal cancellation source usable without vscode. */
export class CancelSource {
  private cbs: (() => void)[] = [];
  cancelled = false;
  readonly token = { onCancel: (cb: () => void) => (this.cancelled ? cb() : this.cbs.push(cb)) };
  cancel(): void {
    if (this.cancelled) return;
    this.cancelled = true;
    for (const cb of this.cbs.splice(0)) cb();
  }
}
