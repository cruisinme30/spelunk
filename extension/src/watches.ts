// What query watches found that you haven't looked at yet: per watched
// query, the new commits announced since the query last ran. No vscode
// import: the controller keeps one, and extension.ts saves it in the
// workspace's state, so the counts survive a reload.
import type { UnseenCommits, WatchCommit, WatchMatchedParams } from "./protocol.gen";

/** The most shas kept per query; the count goes on past it. */
export const MAX_UNSEEN_SHAS = 500;

/** The new commits of one announcement, and how many it counted in all. */
export interface NewMatches {
  /** The announced commits not seen before, newest first. */
  commits: WatchCommit[];
  /** How many of the announced commits are new, the unlisted ones included. */
  count: number;
}

/** The unseen commits of every watched query. */
export class UnseenWatches {
  private readonly unseen = new Map<string, UnseenCommits>();

  /** Starts from the saved state, dropping malformed entries; `save` is called after every change. */
  constructor(
    saved: unknown,
    private readonly save: (unseen: UnseenCommits[]) => void = () => {},
  ) {
    if (!Array.isArray(saved)) return;
    for (const entry of saved as unknown[]) {
      if (typeof entry !== "object" || entry === null) continue;
      const { query, count, shas } = entry as Record<string, unknown>;
      if (typeof query !== "string" || !Number.isSafeInteger(count) || (count as number) < 1 || !Array.isArray(shas)) {
        continue;
      }
      const kept = shas.filter((sha): sha is string => typeof sha === "string");
      this.unseen.set(query, { query, count: Math.max(count as number, kept.length), shas: kept });
    }
  }

  /**
   * Adds an announcement's commits to its query's unseen ones. A commit
   * already unseen (another repo's update can't repeat it, but a restart
   * reading the same update again can) is not counted twice.
   */
  record(match: WatchMatchedParams): NewMatches {
    const entry = this.unseen.get(match.query) ?? { query: match.query, count: 0, shas: [] };
    const known = new Set(entry.shas);
    const commits = match.commits.filter((commit) => !known.has(commit.sha));
    const unlisted = Math.max(0, match.total - match.commits.length);
    const count = commits.length + unlisted;
    if (count === 0) return { commits, count };
    this.unseen.set(match.query, {
      query: match.query,
      count: entry.count + count,
      shas: [...commits.map((commit) => commit.sha), ...entry.shas].slice(0, MAX_UNSEEN_SHAS),
    });
    this.save(this.list());
    return { commits, count };
  }

  /** Takes a query's unseen shas, now that it ran; empty when it has none. */
  take(query: string): string[] {
    const entry = this.unseen.get(query);
    if (!entry) return [];
    this.unseen.delete(query);
    this.save(this.list());
    return entry.shas;
  }

  /** Forgets every query not in `watched`: unpinned or no longer watched. */
  keepOnly(watched: readonly string[]): void {
    const keep = new Set(watched);
    let changed = false;
    for (const query of this.unseen.keys()) {
      if (!keep.has(query)) changed = this.unseen.delete(query) || changed;
    }
    if (changed) this.save(this.list());
  }

  /** Every query with unseen commits. */
  list(): UnseenCommits[] {
    return [...this.unseen.values()];
  }
}

/** Shows nothing, and keeps "](" from forming. */
const WORD_JOINER = "\u2060";

/**
 * Text from a commit, a pin or a repo as plain text in a notification. VS
 * Code makes "[label](command:…)" in a notification a link, and has no
 * escape for it (a backslash would show), so a word joiner, which shows
 * nothing, goes between "]" and "(".
 */
function notificationText(text: string): string {
  return text.replaceAll("](", `]${WORD_JOINER}(`);
}

/** The pop-up for an announcement: "2 new commits match “Payments reverts” in payments-api: “Revert …” and 1 more." */
export function watchMessage(label: string, repoName: string, found: NewMatches): string {
  const [first] = found.commits;
  const matches = found.count === 1 ? "commit matches" : "commits match";
  const head = `${found.count} new ${matches} “${notificationText(label)}” in ${notificationText(repoName)}`;
  if (!first) return `${head}.`;
  const more = found.count - 1;
  return `${head}: “${notificationText(first.subject)}”${more > 0 ? ` and ${more} more` : ""}.`;
}
