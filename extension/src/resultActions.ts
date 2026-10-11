// What the result menu's items do: copy a result, its path or its sha,
// copy or open its permalink, and copy, export or link every result. It
// has no vscode import: extension.ts supplies the clipboard, browser,
// pop-ups and save dialog, and tests drive it in plain Node.
import type { Backend } from "./controller";
import type { HostToWebview, Remote, ResultItem, Root } from "./protocol.gen";
import { type HostSettings, permalink, problemMessage } from "./permalinks";
import {
  absolutePath,
  copyText,
  type ExportFormat,
  exportFileName,
  exportResults,
  relativePath,
  shaAndSubject,
} from "./resultText";

/** The most results an export saves; the daemon links at most this many at once too. */
const EXPORT_LIMIT = 10_000;

/** What the actions need from VS Code. */
export interface ActionsUi {
  /** Puts text on the clipboard, then says `done` briefly in the status bar. */
  copy(text: string, done: string): Promise<void>;
  /** Opens an address in the browser. */
  openExternal(url: string): Promise<void>;
  /** Says why something can't be done; `settings` adds a button that opens spelunk.permalinks.hosts. */
  inform(message: string, settings?: boolean): void;
  /** Asks which remote to link to; undefined when the user cancels. */
  pickRemote(remotes: Remote[]): Promise<string | undefined>;
  /** Asks where to save an export named `name`, then writes `text()` there; false when the user cancels. */
  save(name: string, format: ExportFormat, text: () => Promise<string>): Promise<boolean>;
}

/** What the actions read: the results, their repos, the search, and the daemon. */
export interface ActionsSource {
  backend: Pick<Backend, "request">;
  /** A result the panel shows, by ref. */
  item(ref: string): ResultItem | undefined;
  /** The result the panel last selected, for the keyboard shortcuts. */
  selectedRef(): string | undefined;
  /** The workspace folder a result's repoId names. */
  root(repoId: string): Root | undefined;
  hosts(): HostSettings;
  /** The query whose results the panel shows, once its search finished. */
  query(): string | undefined;
  /** The results loaded so far, and how many the search found. */
  shown(): { items: readonly ResultItem[]; total: number };
  /** Every result of the query, up to `limit`, loading the pages not shown yet. */
  all(limit: number): Promise<ResultItem[]>;
  /** The address that opens Spelunk on `query`. */
  searchLink(query: string): string;
  /** The path separator Copy Path writes. */
  separator: string;
}

/**
 * What the result menu needs to know of the panel, read off the messages
 * the host sends it: the result last selected (whose preview was sent, for
 * the keyboard shortcuts), how many results the search found, and when the
 * panel (re)opened, which is when it learns each repo's host.
 */
export class ShownResults {
  /** The result the panel last selected. */
  selectedRef: string | undefined;
  /** How many results the last finished search found, loaded or not. */
  total = 0;
  /** Called each time the panel gets its state, as it opens or settings change. */
  onRestore: () => void = () => {};

  /** Notes a message to the panel: a selected result's preview, a search's total, or the panel's state. */
  posted<T extends keyof HostToWebview>(type: T, payload: HostToWebview[T]): void {
    if (type === "preview.result") this.selectedRef = (payload as HostToWebview["preview.result"]).ref;
    if (type === "search.done") this.total = (payload as HostToWebview["search.done"]).total;
    if (type === "state.restore") this.onRestore();
  }
}

/** A result in a file: a file name, a code line or a definition. */
type FileResult = Exclude<ResultItem, { kind: "commit" }>;

/** How a sentence names a result: "line 46", "lines 46–48", "client.py", "4421bd6". */
function nameOf(item: ResultItem): string {
  if (item.kind === "commit") return item.sha.slice(0, 7);
  if (item.kind === "file") return item.path.split("/").pop() ?? item.path;
  const end = item.kind === "line" ? item.endLine : undefined;
  return end !== undefined && end > item.line ? `lines ${item.line}–${end}` : `line ${item.line}`;
}

/** How a problem sentence names where the result is: "line 46", "this file", "this commit". */
function whereOf(item: ResultItem): string {
  if (item.kind === "commit") return "this commit";
  return item.kind === "file" ? "this file" : nameOf(item);
}

/** The result menu's items, as actions on a result's ref (or the selected result's). */
export class ResultActions {
  /** Acts on `source`'s results through `ui`. */
  constructor(
    private readonly source: ActionsSource,
    private readonly ui: ActionsUi,
  ) {}

  /** The result `ref` names, else the selected one; undefined when neither is shown any more. */
  private resolve(ref: string | undefined): ResultItem | undefined {
    const wanted = ref ?? this.source.selectedRef();
    return wanted === undefined ? undefined : this.source.item(wanted);
  }

  /** The file result `ref` names, or undefined for a commit or a result no longer shown. */
  private fileResult(ref: string | undefined): FileResult | undefined {
    const item = this.resolve(ref);
    return item?.kind === "commit" ? undefined : item;
  }

  /** Copy: the result's path and line, then its code. */
  async copy(ref?: string): Promise<void> {
    const item = this.fileResult(ref);
    if (item) await this.ui.copy(copyText(item), `Copied ${relativePath(item)} and its code`);
  }

  /** Copy Path: the file's full path, then ":line". */
  async copyPath(ref?: string): Promise<void> {
    const item = this.fileResult(ref);
    const root = item && this.source.root(item.repoId);
    if (!item || !root) return;
    const path = absolutePath(item, root.path, this.source.separator);
    await this.ui.copy(path, `Copied ${path}`);
  }

  /** Copy Relative Path: the path from the workspace folder, then ":line". */
  async copyRelativePath(ref?: string): Promise<void> {
    const item = this.fileResult(ref);
    if (item) await this.ui.copy(relativePath(item), `Copied ${relativePath(item)}`);
  }

  /** Copy SHA: a commit's full sha. */
  async copySha(ref?: string): Promise<void> {
    const item = this.resolve(ref);
    if (item?.kind === "commit") await this.ui.copy(item.sha, `Copied ${item.sha}`);
  }

  /** Copy SHA and Subject: "4421bd6 Retry charges with the shared retry_policy". */
  async copyShaAndSubject(ref?: string): Promise<void> {
    const item = this.resolve(ref);
    if (item?.kind === "commit") await this.ui.copy(shaAndSubject(item), `Copied ${shaAndSubject(item)}`);
  }

  /** Copy Permalink: the result's address on its Git host, or why it has none. */
  async copyPermalink(ref?: string): Promise<void> {
    const item = this.resolve(ref);
    const found = item && (await this.permalinkOf(item));
    if (item && found) await this.ui.copy(found.url, `Copied a ${found.host} permalink to ${nameOf(item)}`);
  }

  /** Open on GitHub (or the repo's host): the permalink, in the browser. */
  async openOnRemote(ref?: string): Promise<void> {
    const item = this.resolve(ref);
    const found = item && (await this.permalinkOf(item));
    if (found) await this.ui.openExternal(found.url);
  }

  /**
   * The result's permalink. When the repo has several remotes and tracks
   * none, the user picks one; when there's no link, a pop-up says why and
   * this returns undefined.
   */
  private async permalinkOf(item: ResultItem): Promise<{ url: string; host: string } | undefined> {
    const repo = this.source.root(item.repoId)?.name ?? "This repo";
    const resolve = async (remote?: string) => {
      const answer = await this.source.backend.request("link/resolve", {
        refs: [item.ref],
        ...(remote ? { remote } : {}),
      });
      return answer.links[0];
    };
    let link = await resolve();
    if (link?.problem === "pickRemote" && link.remotes) {
      const remote = await this.ui.pickRemote(link.remotes);
      if (remote === undefined) return undefined;
      link = await resolve(remote);
    }
    if (!link) return undefined;
    const made = permalink(link, this.source.hosts());
    if ("url" in made) return made;
    this.ui.inform(problemMessage(made, repo, whereOf(item)), made.problem === "unknownHost");
    return undefined;
  }

  /** Copy All Results: the results loaded so far, as text; says when the search found more. */
  async copyAll(): Promise<void> {
    const query = this.source.query();
    const { items, total } = this.source.shown();
    if (query === undefined || items.length === 0) return;
    const text = exportResults("txt", this.exportInput(query, items, new Map()));
    const which = total > items.length ? `the ${items.length} shown of ${total}` : `all ${items.length}`;
    await this.ui.copy(text, `Copied ${which} results as text`);
  }

  /** Export Results: every result (up to EXPORT_LIMIT) to a file in `format`, with each one's permalink. */
  async export(format: ExportFormat): Promise<void> {
    const query = this.source.query();
    if (query === undefined || this.source.shown().items.length === 0) return;
    await this.ui.save(exportFileName(query, format), format, async () => {
      const items = await this.source.all(EXPORT_LIMIT);
      // Text has no links, so it needs none worked out.
      const links = format === "txt" ? new Map<string, string>() : await this.linksOf(items);
      return exportResults(format, this.exportInput(query, items, links));
    });
  }

  /** Copy Link to This Search: an address that opens Spelunk on the query. */
  async copySearchLink(): Promise<void> {
    const query = this.source.query();
    if (query === undefined) return;
    await this.ui.copy(this.source.searchLink(query), "Copied a link that opens this search in Spelunk");
  }

  /** Each result's permalink, by ref; results without one are left out. */
  private async linksOf(items: readonly ResultItem[]): Promise<Map<string, string>> {
    const links = new Map<string, string>();
    if (items.length === 0) return links;
    const refs = items.map((item) => item.ref);
    const answer = await this.source.backend.request("link/resolve", { refs });
    const hosts = this.source.hosts();
    for (const [index, link] of answer.links.entries()) {
      const made = permalink(link, hosts);
      const ref = refs[index];
      if ("url" in made && ref !== undefined) links.set(ref, made.url);
    }
    return links;
  }

  /** What every export format reads, for these results and links. */
  private exportInput(query: string, items: readonly ResultItem[], links: Map<string, string>) {
    return {
      items,
      query,
      repoName: (repoId: string) => this.source.root(repoId)?.name ?? repoId,
      searchLink: this.source.searchLink(query),
      link: (item: ResultItem) => links.get(item.ref),
    };
  }
}

/**
 * A search link's query: the q of vscode://<extension>/search?q=…, given the
 * link's path and its query as VS Code decodes them; undefined for any
 * other link. q is the link's only parameter, so everything after "q=" is
 * the query, & and = included.
 */
export function queryOfSearchLink(path: string, decodedQuery: string): string | undefined {
  if (path !== "/search" || !decodedQuery.startsWith("q=")) return undefined;
  const text = decodedQuery.slice(2);
  return text.trim() === "" ? undefined : text;
}

/** The link that opens Spelunk on `query`, for an editor whose URI scheme is `scheme` (vscode, cursor…). */
export function searchLink(scheme: string, extensionId: string, query: string): string {
  return `${scheme}://${extensionId}/search?q=${encodeURIComponent(query)}`;
}
