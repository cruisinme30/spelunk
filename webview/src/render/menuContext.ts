// remotes.state: which kind of Git host each repo's links go to, which names
// the link items of VS Code's right-click menu on a result (menuContext in
// state.ts says what a row tells the menu).
import type { RemotesStateMessage, ResultItem } from "../protocol.gen";
import { menuContext, type ViewState } from "../state";

/** The rows a result list draws, by result. */
interface ResultRows {
  readonly items: readonly ResultItem[];
  rowFor(ref: string): HTMLElement | undefined;
}

/** remotes.state: keeps each repo's host, and names it on the rows already drawn. */
export function applyRemoteHosts(state: ViewState, rows: ResultRows | undefined, { repos }: RemotesStateMessage): void {
  state.remoteHosts = new Map(repos.map((repo) => [repo.repoId, repo.host]));
  for (const item of rows?.items ?? []) {
    rows?.rowFor(item.ref)?.setAttribute("data-vscode-context", menuContext(state, item));
  }
}
