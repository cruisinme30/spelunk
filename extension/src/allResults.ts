// Every result of a query, for an export: the query runs again as searches
// of its own, page after page, so the panel's results and pages stay as
// they are. No vscode import, so it's tested in plain Node.
import type { Backend } from "./controller";
import type { ResultItem, SearchBatchParams } from "./protocol.gen";

/** Collects the batches of the searches it runs, as the daemon streams them. */
export class AllResults {
  private readonly running = new Map<string, ResultItem[]>();
  private started = 0;

  /** Listens for the backend's batches; those of its own searches are kept, the rest ignored. */
  constructor(private readonly backend: Pick<Backend, "request" | "on">) {
    backend.on("batch", (batch: SearchBatchParams) => {
      this.running.get(batch.searchId)?.push(...batch.items);
    });
  }

  /** Every result of `text`, up to `limit`; `openFiles` are the editor's open files, for is:open. */
  async of(text: string, openFiles: string[], limit: number): Promise<ResultItem[]> {
    this.started++;
    const searchId = `all${this.started}`;
    const items: ResultItem[] = [];
    this.running.set(searchId, items);
    try {
      let cursor: string | undefined;
      do {
        const page = cursor === undefined ? {} : { cursor };
        const result = await this.backend.request("search/start", { searchId, text, openFiles, ...page });
        cursor = result.nextCursor;
      } while (cursor !== undefined && items.length < limit);
    } finally {
      this.running.delete(searchId);
    }
    return items.slice(0, limit);
  }
}
