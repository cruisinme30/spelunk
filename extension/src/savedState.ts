// What a window keeps across reloads (globalState): the box's text, and the
// recent list, newest first, one row per query, each with the question Ask
// wrote it for, if it came from one.
import type { RecentQuery } from "./protocol.gen";

/**
 * Longer queries (a pasted file, say) are not kept in globalState: not in
 * the recent list, and not as the box's text for the next session. The
 * daemon refuses a query this long anyway (query_too_long).
 */
const MAX_REMEMBERED_QUERY_LENGTH = 1000;

/** Survives window reloads: the last query and recent queries. */
export interface PersistedState {
  text: string;
  recent: RecentQuery[];
}

/** PersistedState as globalState may hold it: 3.0 kept each recent query as a plain string. */
export interface SavedState {
  text: string;
  recent: (string | RecentQuery)[];
}

/**
 * `recent` with `query` on top, at most `limit` long, or undefined when the query can't be kept (blank, too long, or
 * a limit of none). The question Ask wrote it for labels it; a query kept again without one keeps its old label.
 */
export function withRecent(
  recent: RecentQuery[],
  query: string,
  limit: number,
  question?: string,
): RecentQuery[] | undefined {
  const text = query.trim();
  if (!text || text.length > MAX_REMEMBERED_QUERY_LENGTH || !(limit > 0)) return undefined;
  const asked = question?.trim() ? question.trim() : recent.find((entry) => entry.query === text)?.question;
  const entry: RecentQuery = { query: text, ...(asked ? { question: asked } : {}) };
  return [entry, ...recent.filter((kept) => kept.query !== text)].slice(0, limit);
}

/**
 * The state a window opens with: the saved one, with the query left in the box when the window closed on top of the
 * recent list, as closing the panel would have put it there.
 */
export function openingState(saved: SavedState | undefined, limit: number): PersistedState {
  const text = saved?.text ?? "";
  const recent = (saved?.recent ?? []).map((entry) => (typeof entry === "string" ? { query: entry } : entry));
  return { text, recent: withRecent(recent, text, limit) ?? recent };
}

/** What to save: the box's text, unless it is too long to keep, and the recent queries. */
export function persisted({ text, recent }: PersistedState): PersistedState {
  return { text: text.length > MAX_REMEMBERED_QUERY_LENGTH ? "" : text, recent: [...recent] };
}
