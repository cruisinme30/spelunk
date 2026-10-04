// Spawns and supervises the search daemon: handshake, protocol check, crash
// restarts and graceful stop (Contract 3 lifecycle, plan failure table).
// Pure Node; the vscode glue lives in extension.ts.
import { spawn as nodeSpawn, type ChildProcess } from "node:child_process";
import { EventEmitter } from "node:events";
import { Connection, type CancelSource } from "./jsonRpc";
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

/** Crashes are counted over a rolling window of this length. */
const CRASH_WINDOW_MS = 60_000;
/** Failure table: "restarts it, up to 3 times per minute". */
const DEFAULT_MAX_RESTARTS_PER_MINUTE = 3;
const DEFAULT_RESTART_DELAY_MS = 200;
/** Contract 3: after shutdown the daemon must flush within 2 seconds. */
const SHUTDOWN_GRACE_MS = 2000;
/** How long a request waits for a restarting daemon before failing. */
const CONNECTION_WAIT_MS = 5000;
const CONNECTION_POLL_MS = 25;

export interface DaemonOptions {
  binary: string;
  args?: string[];
  env?: NodeJS.ProcessEnv;
  roots(): Root[];
  settings(): Settings;
  log?(line: string): void;
  maxRestartsPerMinute?: number;
  restartDelayMs?: number;
  /** For tests: replaces child_process.spawn. */
  spawn?: typeof nodeSpawn;
  /** For tests: replaces Date.now. */
  now?(): number;
}

export interface DaemonEvents {
  state: [DaemonState, string | undefined];
  batch: [SearchBatchParams];
  progress: [IndexStatusResult];
}

/** One supervised daemon process. Emits "state", "batch" and "progress". */
export class Daemon extends EventEmitter {
  state: DaemonState = "stopped";
  daemonVersion?: string;
  private child?: ChildProcess;
  private connection?: Connection;
  private stopping = false;
  private crashTimes: number[] = [];

  constructor(private readonly options: DaemonOptions) {
    super();
  }

  override on<E extends keyof DaemonEvents>(event: E, listener: (...args: DaemonEvents[E]) => void): this {
    return super.on(event, listener as (...args: unknown[]) => void);
  }

  /** Starts the daemon; resolves once initialize succeeded and the protocol matches. */
  start(): Promise<void> {
    this.stopping = false;
    return this.spawnAndInitialize();
  }

  /** The "Search stopped — Restart" button: forgets earlier crashes and starts again. */
  restart(): Promise<void> {
    this.crashTimes = [];
    this.killChild();
    return this.start();
  }

  /** Graceful stop: shutdown, exit, then SIGKILL if the process lingers. */
  async stop(): Promise<void> {
    this.stopping = true;
    const child = this.child;
    const connection = this.connection;
    if (!child || !connection) {
      this.setState("stopped");
      return;
    }
    const exited = new Promise<void>((resolve) => child.once("exit", () => resolve()));
    try {
      await Promise.race([connection.request("shutdown", {}), delay(SHUTDOWN_GRACE_MS)]);
      connection.notify("exit", {});
    } catch {
      // Already gone: nothing to shut down.
    }
    await Promise.race([exited, delay(SHUTDOWN_GRACE_MS).then(() => child.kill("SIGKILL"))]);
  }

  async request<M extends keyof RpcRequests>(
    method: M,
    params: RpcRequests[M][0],
    cancel?: CancelSource,
  ): Promise<RpcRequests[M][1]> {
    const connection = await this.readyConnection();
    return connection.request(method, params, cancel?.token);
  }

  setRoots(roots: Root[]): void {
    this.connection?.notify("workspace/setRoots", { roots });
  }

  updateSettings(settings: Settings): void {
    this.connection?.notify("settings/update", { settings });
  }

  didChangeFiles(changes: FileChange[]): void {
    if (changes.length) this.connection?.notify("workspace/didChangeFiles", { changes });
  }

  /** The daemon's process id, for tests. */
  get pid(): number | undefined {
    return this.child?.pid;
  }

  private setState(state: DaemonState, message?: string): void {
    this.state = state;
    this.emit("state", state, message);
  }

  private log(line: string): void {
    this.options.log?.(line);
  }

  private now(): number {
    return (this.options.now ?? Date.now)();
  }

  private async spawnAndInitialize(): Promise<void> {
    if (this.state !== "restarting") this.setState("starting");
    const child = this.spawnChild();
    const connection = new Connection(child.stdout!, child.stdin!);
    this.connection = connection;
    connection.onNotification("search/batch", (batch) => this.emit("batch", batch));
    connection.onNotification("index/progress", (progress) => this.emit("progress", progress));
    connection.on("error", (error) => this.log(`[rpc] ${String(error)}`));

    const exited = new Promise<number | null>((resolve) => {
      child.on("exit", (code) => resolve(code));
      child.on("error", (error) => {
        this.log(`[daemon] failed to start: ${error.message}`);
        resolve(-1);
      });
    });
    void exited.then((code) => this.onExit(child, code));

    const initialized = connection.request("initialize", {
      protocol: PROTOCOL_VERSION,
      roots: this.options.roots(),
      settings: this.options.settings(),
    });
    const diedFirst = exited.then(() => {
      throw new Error("daemon exited during initialize");
    });
    try {
      const result = await Promise.race([initialized, diedFirst]);
      if (result.protocol !== PROTOCOL_VERSION) {
        this.setState(
          "protocolMismatch",
          `The search daemon speaks protocol ${result.protocol} but the extension needs ${PROTOCOL_VERSION}. Reinstall the extension.`,
        );
        this.stopping = true;
        this.killChild();
        throw new Error("protocol mismatch");
      }
      this.daemonVersion = result.daemonVersion;
      this.setState("ok");
    } catch (error) {
      if (this.state !== "protocolMismatch" && this.state !== "stopped") this.log(`[daemon] ${String(error)}`);
      throw error;
    }
  }

  private spawnChild(): ChildProcess {
    const spawn = this.options.spawn ?? nodeSpawn;
    const child = spawn(this.options.binary, this.options.args ?? [], {
      env: { ...process.env, ...this.options.env },
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child = child;
    child.stderr?.on("data", (data: Buffer) => this.log(`[daemon] ${data.toString().trimEnd()}`));
    return child;
  }

  /** Restarts after a crash, unless that would exceed the per-minute budget. */
  private onExit(child: ChildProcess, code: number | null): void {
    if (child !== this.child) return; // an old process we already replaced
    this.connection?.dispose(new Error("daemon exited"));
    this.connection = undefined;
    this.child = undefined;
    if (this.stopping) {
      if (this.state !== "protocolMismatch") this.setState("stopped");
      return;
    }
    const now = this.now();
    this.crashTimes = this.crashTimes.filter((time) => now - time < CRASH_WINDOW_MS);
    this.crashTimes.push(now);
    this.log(`[daemon] exited with ${code}; crash ${this.crashTimes.length} in the last minute`);
    if (this.crashTimes.length > (this.options.maxRestartsPerMinute ?? DEFAULT_MAX_RESTARTS_PER_MINUTE)) {
      this.setState("stopped", "Search stopped");
      return;
    }
    this.setState("restarting", "Search restarting…");
    setTimeout(() => {
      if (!this.stopping) this.start().catch(() => undefined);
    }, this.options.restartDelayMs ?? DEFAULT_RESTART_DELAY_MS);
  }

  private killChild(): void {
    const child = this.child;
    this.child = undefined;
    this.connection?.dispose();
    this.connection = undefined;
    if (child && child.exitCode === null) child.kill("SIGKILL");
  }

  /** The connection once the daemon is ready, waiting through a restart. */
  private async readyConnection(): Promise<Connection> {
    const deadline = this.now() + CONNECTION_WAIT_MS;
    for (;;) {
      if (this.connection && this.state === "ok") return this.connection;
      if (this.state === "stopped" || this.state === "protocolMismatch")
        throw new Error(`search daemon is ${this.state}`);
      if (this.now() > deadline) throw new Error("search daemon not ready");
      await delay(CONNECTION_POLL_MS);
    }
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
