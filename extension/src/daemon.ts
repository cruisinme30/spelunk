// Spawns and supervises the search daemon: the initialize handshake, the
// protocol version check, restarts after a crash and a graceful stop.
// Plain Node with no vscode import, so tests run it against the real binary.
import { spawn as nodeSpawn, type ChildProcess, type ChildProcessByStdio } from "node:child_process";
import { EventEmitter } from "node:events";
import type { Readable, Writable } from "node:stream";
import { Connection, RpcError, type CancelSource } from "./jsonRpc";
import {
  PROTOCOL_VERSION,
  type FileChange,
  type IndexStatusResult,
  type Root,
  type RpcRequests,
  type SearchBatchParams,
  type Settings,
} from "./protocol.gen";

/**
 * Where the daemon is in its life: "ok" accepts requests; "stopped" waits for
 * a manual restart; "protocolMismatch" means the binary and the extension
 * disagree and only reinstalling helps.
 */
export type DaemonState = "starting" | "ok" | "restarting" | "stopped" | "protocolMismatch";

/** Crashes are counted over a rolling window of this length. */
const CRASH_WINDOW_MS = 60_000;
/** A crashed daemon is restarted at most this many times a minute; after that it stays stopped. */
const DEFAULT_MAX_RESTARTS_PER_MINUTE = 3;
/** The pause before restarting a crashed daemon. */
const DEFAULT_RESTART_DELAY_MS = 200;
/** How long stop() waits for the shutdown reply, and then for the process to exit, before killing it. */
const SHUTDOWN_GRACE_MS = 2000;
/** How long the initialize handshake may take; a daemon that doesn't answer by then counts as crashed. */
const DEFAULT_INITIALIZE_TIMEOUT_MS = 20_000;
/** How long a request waits for a starting or restarting daemon before failing. */
const CONNECTION_WAIT_MS = 5000;
/** How often a waiting request checks whether the daemon is ready. */
const CONNECTION_POLL_MS = 25;

/** The daemon process: a child with piped stdin, stdout and stderr. */
type DaemonProcess = ChildProcessByStdio<Writable, Readable, Readable>;

/** How to run the daemon, and what to tell it. */
export interface DaemonOptions {
  binary: string;
  args?: string[];
  env?: NodeJS.ProcessEnv;
  /** The workspace roots, read again on every (re)start. */
  roots(): Root[];
  /** The settings, read again on every (re)start. */
  settings(): Settings;
  log?(line: string): void;
  maxRestartsPerMinute?: number;
  restartDelayMs?: number;
  initializeTimeoutMs?: number;
  /** For tests: replaces child_process.spawn. */
  spawn?: typeof nodeSpawn;
  /** For tests: replaces Date.now. */
  now?(): number;
}

/** The events a Daemon emits, and the arguments each listener receives. */
export interface DaemonEvents {
  state: [DaemonState, string | undefined];
  batch: [SearchBatchParams];
  progress: [IndexStatusResult];
}

/** One supervised daemon process. Emits "state", "batch" and "progress". */
export class Daemon extends EventEmitter {
  state: DaemonState = "stopped";
  /** The running daemon's version, from its initialize reply. */
  daemonVersion?: string;
  private child: ChildProcess | undefined;
  private connection: Connection | undefined;
  private stopping = false;
  private crashTimes: number[] = [];
  /** The restart scheduled after a crash, until it runs. */
  private restartTimer: NodeJS.Timeout | undefined;

  /** Creates a supervisor; nothing runs until start(). */
  constructor(private readonly options: DaemonOptions) {
    super();
  }

  /** Listens for one of the DaemonEvents, with typed arguments. */
  override on<E extends keyof DaemonEvents>(event: E, listener: (...args: DaemonEvents[E]) => void): this {
    return super.on(event, listener as (...args: unknown[]) => void);
  }

  /** Starts the daemon; resolves once initialize succeeded and the protocol matches. */
  start(): Promise<void> {
    this.stopping = false;
    return this.spawnAndInitialize();
  }

  /**
   * Restarts now, for the Restart button and command: forgets earlier crashes
   * (so the restart budget is full again), kills the current process and
   * starts a new one.
   */
  restart(): Promise<void> {
    this.crashTimes = [];
    this.cancelScheduledRestart();
    this.killChild();
    return this.start();
  }

  /** Graceful stop: shutdown, exit, then SIGKILL if the process lingers. */
  async stop(): Promise<void> {
    this.stopping = true;
    this.cancelScheduledRestart();
    const child = this.child;
    const connection = this.connection;
    if (!child || !connection) {
      this.setState("stopped");
      return;
    }
    const exited = new Promise<void>((resolve) =>
      child.once("exit", () => {
        resolve();
      }),
    );
    try {
      await Promise.race([connection.request("shutdown", {}), delay(SHUTDOWN_GRACE_MS)]);
      connection.notify("exit", {});
    } catch {
      // Already gone: nothing to shut down.
    }
    await Promise.race([exited, delay(SHUTDOWN_GRACE_MS).then(() => child.kill("SIGKILL"))]);
  }

  /** Sends a request, waiting first for a starting or restarting daemon to be ready. */
  async request<M extends keyof RpcRequests>(
    method: M,
    params: RpcRequests[M][0],
    cancel?: CancelSource,
  ): Promise<RpcRequests[M][1]> {
    const connection = await this.readyConnection();
    return connection.request(method, params, cancel?.token);
  }

  /** Tells the running daemon about changed workspace folders. */
  setRoots(roots: Root[]): void {
    this.connection?.notify("workspace/setRoots", { roots });
  }

  /** Tells the running daemon about changed settings. */
  updateSettings(settings: Settings): void {
    this.connection?.notify("settings/update", { settings });
  }

  /** Forwards file changes VS Code saw, so the index catches up before the daemon's own watcher would. */
  didChangeFiles(changes: FileChange[]): void {
    if (changes.length > 0) this.connection?.notify("workspace/didChangeFiles", { changes });
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
    let child: DaemonProcess;
    let params: RpcRequests["initialize"][0];
    try {
      params = { protocol: PROTOCOL_VERSION, roots: this.options.roots(), settings: this.options.settings() };
      child = this.spawnChild();
    } catch (error) {
      // Nothing is running, and trying again would fail the same way.
      this.stopping = true;
      this.setState("stopped", `The search daemon could not start: ${errorMessage(error)}`);
      throw error;
    }
    const connection = this.connect(child);
    const exited = this.watchExit(child);
    let initializeTimer: NodeJS.Timeout | undefined;
    const timedOut = new Promise<never>((_resolve, reject) => {
      const timeoutMs = this.options.initializeTimeoutMs ?? DEFAULT_INITIALIZE_TIMEOUT_MS;
      initializeTimer = setTimeout(() => {
        reject(new Error(`daemon did not answer initialize within ${timeoutMs} ms`));
      }, timeoutMs);
    });
    try {
      const result = await Promise.race([
        connection.request("initialize", params),
        exited.then(() => {
          throw new Error("daemon exited during initialize");
        }),
        timedOut,
      ]);
      // stop() or restart() while initialize was in flight: this process is no longer wanted.
      if (this.stopping || this.child !== child) throw new Error("daemon start was superseded");
      if (result.protocol !== PROTOCOL_VERSION) this.refuseProtocol(result.protocol);
      this.daemonVersion = result.daemonVersion;
      this.setState("ok");
    } catch (error) {
      if (!this.stopping && this.child === child) this.abandonStart(child, error);
      throw error;
    } finally {
      clearTimeout(initializeTimer);
    }
  }

  /** The connection to a new process; its notifications become this daemon's events. */
  private connect(child: DaemonProcess): Connection {
    const connection = new Connection(child.stdout, child.stdin);
    this.connection = connection;
    connection.onNotification("search/batch", (batch) => this.emit("batch", batch));
    connection.onNotification("index/progress", (progress) => this.emit("progress", progress));
    connection.on("error", (error) => {
      this.log(`[rpc] ${String(error)}`);
    });
    return connection;
  }

  /** Calls onExit when `child` exits or fails to start; the promise resolves then. */
  private watchExit(child: DaemonProcess): Promise<void> {
    return new Promise<void>((resolve) => {
      child.on("exit", (code, signal) => {
        this.onExit(child, `exited with ${String(code ?? signal)}`);
        resolve();
      });
      child.on("error", (error) => {
        // Only a process that never started gets no "exit": a missing or non-executable binary.
        if (child.pid !== undefined) return;
        this.log(`[daemon] failed to start ${this.options.binary}: ${error.message}`);
        this.onExit(child, undefined, error);
        resolve();
      });
    });
  }

  /**
   * A start that failed while the process may still run. A refused
   * initialize (say, settings it can't read) stops: a restart would be
   * refused too. Anything else (no answer, a garbled one) kills the process,
   * which counts as a crash and restarts within the budget.
   */
  private abandonStart(child: DaemonProcess, error: unknown): void {
    this.log(`[daemon] ${String(error)}`);
    if (error instanceof RpcError) {
      this.stopping = true;
      this.killChild();
      this.setState("stopped", `The search daemon refused to start: ${error.message}`);
      return;
    }
    child.kill("SIGKILL");
  }

  /** Stops a daemon that speaks another protocol version; restarting would not help. */
  private refuseProtocol(daemonProtocol: number): never {
    this.setState(
      "protocolMismatch",
      `The search daemon speaks protocol ${daemonProtocol} but the extension needs ${PROTOCOL_VERSION}. Reinstall the extension.`,
    );
    this.stopping = true;
    this.killChild();
    throw new Error("protocol mismatch");
  }

  private spawnChild(): DaemonProcess {
    const spawn = this.options.spawn ?? nodeSpawn;
    const child = spawn(this.options.binary, this.options.args ?? [], {
      env: { ...process.env, ...this.options.env },
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child = child;
    child.stderr.on("data", (data: Buffer) => {
      this.log(`[daemon] ${data.toString().trimEnd()}`);
    });
    return child;
  }

  /**
   * Restarts after a crash, unless that would exceed the per-minute budget.
   * A binary that could not be run at all (`spawnError`) is not retried.
   */
  private onExit(child: ChildProcess, how: string | undefined, spawnError?: Error): void {
    if (child !== this.child) return; // an old process we already replaced
    this.connection?.dispose(new Error("daemon exited"));
    this.connection = undefined;
    this.child = undefined;
    if (this.stopping) {
      if (this.state !== "protocolMismatch") this.setState("stopped");
      return;
    }
    if (spawnError) {
      this.setState("stopped", `The search daemon could not start: ${spawnError.message}`);
      return;
    }
    const now = this.now();
    this.crashTimes = this.crashTimes.filter((time) => now - time < CRASH_WINDOW_MS);
    this.crashTimes.push(now);
    const crashes = this.crashTimes.length;
    this.log(`[daemon] ${how ?? "exited"}; crash ${crashes} in the last minute`);
    if (crashes > (this.options.maxRestartsPerMinute ?? DEFAULT_MAX_RESTARTS_PER_MINUTE)) {
      // The panel's banner and the status bar name the state; the message says why.
      this.setState("stopped", `The search daemon crashed ${crashes} times in a minute.`);
      return;
    }
    this.setState("restarting");
    this.restartTimer = setTimeout(() => {
      this.restartTimer = undefined;
      if (!this.stopping) this.start().catch(() => {});
    }, this.options.restartDelayMs ?? DEFAULT_RESTART_DELAY_MS);
  }

  private cancelScheduledRestart(): void {
    clearTimeout(this.restartTimer);
    this.restartTimer = undefined;
  }

  private killChild(): void {
    const child = this.child;
    this.child = undefined;
    this.connection?.dispose();
    this.connection = undefined;
    if (child?.exitCode === null) child.kill("SIGKILL");
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

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
