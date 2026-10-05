// The empty box's queries in the panel's state: what starring, naming,
// unpinning and removing change, and the message that tells the host.
import type { PinnedQuery } from "./protocol.gen";
import type { ViewState } from "./state";

/** Pins `query` at the end of the list and selects it; the pinned.save to send, or undefined when it is pinned. */
export function addPin(state: ViewState, query: string): PinnedQuery | undefined {
  if (state.pinned.some((entry) => entry.query === query)) return undefined;
  state.pinned = [...state.pinned, { query }];
  state.recentIndex = state.pinned.length - 1;
  return { query };
}

/**
 * Unpins `query`, or takes it off the recent list when it isn't pinned.
 * Returns the message that tells the host, or undefined when it is in neither.
 */
export function forgetQuery(state: ViewState, query: string): "pinned.remove" | "recent.remove" | undefined {
  if (state.pinned.some((entry) => entry.query === query)) {
    state.pinned = state.pinned.filter((entry) => entry.query !== query);
    if (state.naming === query) state.naming = undefined;
    return "pinned.remove";
  }
  if (!state.recent.includes(query)) return undefined;
  state.recent = state.recent.filter((kept) => kept !== query);
  return "recent.remove";
}

/**
 * Closes the name field on `query` with the typed name (none when blank),
 * or keeps the old name when `name` is undefined (Esc). Returns the
 * pinned.save to send, or undefined when nothing changed.
 */
export function namePin(state: ViewState, query: string, name: string | undefined): PinnedQuery | undefined {
  state.naming = undefined;
  const entry = state.pinned.find((kept) => kept.query === query);
  const trimmed = name?.trim();
  if (!entry || trimmed === undefined || trimmed === (entry.name ?? "")) return undefined;
  if (trimmed) entry.name = trimmed;
  else delete entry.name;
  return { ...entry };
}
