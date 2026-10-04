// Spawns and supervises the search daemon (Contract 3 lifecycle).
// Pure Node: the vscode glue lives in extension.ts.
import { spawn as nodeSpawn, type ChildProcess } from "node:child_process";
import { EventEmitter } from "node:events";
import { Connection, CancelSource } from "./jsonRpc";
import {
  PROTOCOL_VERSION,
  type FileChange,
  type IndexStatusResult,
  type Root,
  type RpcRequests,
  type SearchBatchParams,
  type Settings,
} from "./protocol.gen";

export type DaemonState = "starting" | "ok" | "restarting" | "stopped" | "protocolMismatch";

export interface DaemonOptions {
  binary: string;
  args?: string[];
  env?: NodeJS.ProcessEnv;
  roots(): Root[];
  settings(): Settings;
  log?(line: string): void;
  /** Crashes tolerated per rolling minute before giving up (failure table: 3). */
  maxRestartsPerMinute?: number;
  restartDelayMs?: number;
  spawn?: typeof nodeSpawn;
  now?(): number;
}

export interface DaemonEvents {
  state: [DaemonState, string | undefined];
  batch: [SearchBatchParams];
  progress: [IndexStatusResult];
}

export class Daemon extends EventEmitter {
  state: DaemonState = "stopped";
  daemonVersion?: string;
  private child?: ChildProcess;
  private conn?: Connection;
  private stopping = false;
  private crashes: number[] = [];
  private ready?: Promise<void>;

  constructor(private readonly opts: DaemonOptions) {
    super();
  }

  override on<E extends keyof DaemonEvents>(event: E, cb: (...args: DaemonEvents[E]) => void): this {
    return super.on(event, cb as (...a: unknown[]) => void);
  }

  private setState(s: DaemonState, message?: string): void {
    this.state = s;
    this.emit("state", s, message);
  }

  /** Starts the daemon; resolves once initialize succeeded. */
  start(): Promise<void> {
    this.stopping = false;
    this.ready = this.spawnOnce();
    return this.ready;
  }

  /** Manual restart from the "Search stopped — Restart" banner: clears the crash budget. */
  restart(): Promise<void> {
    this.crashes = [];
    this.killChild();
    return this.start();
  }

  private async spawnOnce(): Promise<void> {
    if (this.state !== "restarting") this.setState("starting");
    const spawn = this.opts.spawn ?? nodeSpawn;
    const child = spawn(this.opts.binary, this.opts.args ?? [], {
      env: { ...process.env, ...this.opts.env },
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child = child;
    child.stderr?.on("data", (d: Buffer) => this.opts.log?.(`[daemon] ${d.toString().trimEnd()}`));
    const conn = new Connection(child.stdout!, child.stdin!);
    this.conn = conn;
    conn.onNotification("search/batch", (p) => this.emit("batch", p));
    conn.onNotification("index/progress", (p) => this.emit("progress", p));
    conn.on("error", (e) => this.opts.log?.(`[rpc] ${String(e)}`));

    const exited = new Promise<number | null>((resolve) => {
      child.on("exit", (code) => resolve(code));
      child.on("error", (err) => {
        this.opts.log?.(`[daemon] failed to start: ${err.message}`);
        resolve(-1);
      });
    });
    exited.then((code) => this.onExit(child, code));

    try {
      const res = await Promise.race([
        conn.request("initialize", {
          protocol: PROTOCOL_VERSION,
          roots: this.opts.roots(),
          settings: this.opts.settings(),
        }),
        exited.then(() => {
          throw new Error("daemon exited during initialize");
        }),
      ]);
      if (res.protocol !== PROTOCOL_VERSION) {
        this.setState(
          "protocolMismatch",
          `Search daemon speaks protocol ${res.protocol}, extension needs ${PROTOCOL_VERSION}. Reinstall the extension.`,
        );
        this.stopping = true;
        this.killChild();
        throw new Error("protocol mismatch");
      }
      this.daemonVersion = res.daemonVersion;
      this.setState("ok");
    } catch (e) {
      if (this.state !== "protocolMismatch" && this.state !== "stopped") this.opts.log?.(`[daemon] ${String(e)}`);
      throw e;
    }
  }

  private onExit(child: ChildProcess, code: number | null): void {
    if (child !== this.child) return;
    this.conn?.dispose(new Error("daemon exited"));
    this.conn = undefined;
    this.child = undefined;
    if (this.stopping) {
      if (this.state !== "protocolMismatch") this.setState("stopped");
      return;
    }
    const now = (this.opts.now ?? Date.now)();
    this.crashes = this.crashes.filter((t) => now - t < 60_000);
    this.crashes.push(now);
    const budget = this.opts.maxRestartsPerMinute ?? 3;
    this.opts.log?.(`[daemon] exited with ${code}; crash ${this.crashes.length} in the last minute`);
    if (this.crashes.length > budget) {
      this.setState("stopped", "Search stopped");
      return;
    }
    this.setState("restarting", "Search restarting…");
    setTimeout(() => {
      if (!this.stopping) this.start().catch(() => undefined);
    }, this.opts.restartDelayMs ?? 200);
  }

  private killChild(): void {
    const c = this.child;
    this.child = undefined;
    this.conn?.dispose();
    this.conn = undefined;
    if (c && c.exitCode === null) c.kill("SIGKILL");
  }

  /** Graceful stop: shutdown, exit, then kill if it lingers past 2 seconds. */
  async stop(): Promise<void> {
    this.stopping = true;
    const child = this.child,
      conn = this.conn;
    if (!child || !conn) {
      this.setState("stopped");
      return;
    }
    const exited = new Promise<void>((r) => child.once("exit", () => r()));
    try {
      await Promise.race([conn.request("shutdown", {}), delay(2000)]);
      conn.notify("exit", {});
    } catch {
      /* already gone */
    }
    await Promise.race([exited, delay(2000).then(() => child.kill("SIGKILL"))]);
  }

  /** Waits for a usable connection (during restarts) up to timeoutMs. */
  private async connection(timeoutMs = 5000): Promise<Connection> {
    const deadline = Date.now() + timeoutMs;
    for (;;) {
      if (this.conn && this.state === "ok") return this.conn;
      if (this.state === "stopped" || this.state === "protocolMismatch")
        throw new Error(`search daemon is ${this.state}`);
      if (Date.now() > deadline) throw new Error("search daemon not ready");
      await delay(25);
    }
  }

  async request<M extends keyof RpcRequests>(
    method: M,
    params: RpcRequests[M][0],
    cancel?: CancelSource,
  ): Promise<RpcRequests[M][1]> {
    const conn = await this.connection();
    return conn.request(method, params, cancel?.token);
  }

  setRoots(roots: Root[]): void {
    this.conn?.notify("workspace/setRoots", { roots });
  }
  updateSettings(settings: Settings): void {
    this.conn?.notify("settings/update", { settings });
  }
  didChangeFiles(changes: FileChange[]): void {
    if (changes.length) this.conn?.notify("workspace/didChangeFiles", { changes });
  }
  /** Test hook: the daemon process id. */
  get pid(): number | undefined {
    return this.child?.pid;
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}
