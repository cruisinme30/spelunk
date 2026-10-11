// Ask, from the host's side: the panel's ask and ask.setup messages, answered
// through an Asker over the daemon's parser and guide.
import { Asker, type AskSetup } from "./ask";
import type { AskMessage, AskResultMessage, AskSetupMessage, IndexStatusResult, RpcRequests } from "./protocol.gen";

/** How Ask is set up: the spelunk.ask.* settings and the API key, read each time a question is asked. */
export interface AskOptions {
  setup(): Promise<AskSetup>;
  /** spelunk.ask.shareNames: whether the repo and team names go with the question. */
  shareNames(): boolean;
  /** Asks the user for the API key (Spelunk: Set or Change Anthropic API Key). */
  setKey(): Promise<void>;
}

/** What the route needs from the host: the daemon, the panel, the settings and the open repos. */
export interface AskRouteDeps {
  request<M extends keyof RpcRequests>(method: M, params: RpcRequests[M][0]): Promise<RpcRequests[M][1]>;
  post(type: "ask.result", payload: AskResultMessage): void;
  openSettings(setting: string): void;
  /** The index's status, for the names of the repos open in the window; undefined before it is known. */
  status(): IndexStatusResult | undefined;
  options: AskOptions | undefined;
}

/** The panel's messages the route handles. */
export type AskRouteMessage = { type: "ask"; payload: AskMessage } | { type: "ask.setup"; payload: AskSetupMessage };

/** Answers the panel's ask and ask.setup messages. */
export class AskRoute {
  private readonly asker: Asker;
  /** Stops the provider call of a question a newer one replaced. */
  private asking: AbortController | undefined;

  /** A route over the host's daemon and panel. */
  static fromHost(
    backend: { request: AskRouteDeps["request"] },
    ui: { post: AskRouteDeps["post"]; openSettings: AskRouteDeps["openSettings"] },
    options: AskOptions | undefined,
    status: () => IndexStatusResult | undefined,
  ): AskRoute {
    return new AskRoute({
      request: (method, params) => backend.request(method, params),
      post: (type, payload) => {
        ui.post(type, payload);
      },
      openSettings: (setting) => {
        ui.openSettings(setting);
      },
      status,
      options,
    });
  }

  /** A route over `deps`: the daemon's requests, the panel's post, the settings and the repos. */
  constructor(private readonly deps: AskRouteDeps) {
    this.asker = new Asker({
      parse: async (text) => {
        const { query } = await deps.request("query/parse", { text, cursor: text.length });
        return query;
      },
      guide: async () => {
        const { text } = await deps.request("query/guide", {});
        return text;
      },
      setup: () => deps.options?.setup() ?? Promise.resolve({ kind: "off" }),
      names: () => this.namesToShare(),
    });
  }

  /** Handles one of the panel's Ask messages. */
  async handle(message: AskRouteMessage): Promise<void> {
    if (message.type === "ask") {
      await this.onAsk(message.payload);
    } else if (message.payload.action === "key") {
      await this.deps.options?.setKey();
    } else {
      this.deps.openSettings("spelunk.ask.provider");
    }
  }

  /** Turns the question into a query and posts it for the panel to run; a newer question stops the one before. */
  private async onAsk({ question, seq, broaden }: AskMessage): Promise<void> {
    this.asking?.abort();
    const control = new AbortController();
    this.asking = control;
    const outcome = await this.asker.ask(question, broaden, control.signal);
    if (control.signal.aborted) return;
    this.asking = undefined;
    this.deps.post("ask.result", { seq, question, ...outcome });
  }

  /** The repo and CODEOWNERS team names a question may use, when spelunk.ask.shareNames lets them go. */
  private async namesToShare(): Promise<{ repos: string[]; teams: string[] }> {
    if (!(this.deps.options?.shareNames() ?? false)) return { repos: [], teams: [] };
    let teams: string[] = [];
    try {
      const prefix = "owner:";
      const { completions } = await this.deps.request("query/parse", { text: prefix, cursor: prefix.length });
      teams = completions.map((completion) => completion.label).filter((label) => label.startsWith("@"));
    } catch {
      // Without the daemon the question still goes, without team names.
    }
    return { repos: (this.deps.status()?.repos ?? []).map((repo) => repo.name), teams };
  }
}
