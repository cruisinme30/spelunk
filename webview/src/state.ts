// Everything the panel knows. Only SearchPanel (panel.ts) changes it;
// render functions read it.
import type {
  BannerMessage,
  Completion,
  Facet,
  ParsedQuery,
  PinnedQuery,
  Preview,
  RepoStatus,
  SearchDoneMessage,
  UiSettings,
} from "./protocol.gen";

/** The preview pane's content: the host's answer for the selected result. */
interface PreviewState {
  ref: string;
  preview: Preview | null;
  stale?: boolean;
}

/** The panel's state; see createViewState for the starting values. */
export interface ViewState {
  /** Sequence number of the newest query.changed; older responses are dropped. */
  seq: number;
  parsed: ParsedQuery | undefined;
  completions: Completion[];
  /** The text the search runs on while a word is being completed; undefined when it runs on the whole box. */
  searchText: string | undefined;
  /** The highlighted suggestion, which Tab accepts. */
  completionIndex: number;
  /** False after Esc or Enter dismissed the suggestions, until the next keystroke. */
  completionsOpen: boolean;
  /** The search whose results are on screen ("" when none). */
  searchId: string;
  /**
   * Whether a newer query without errors has replaced the search on screen.
   * The host cancels that search then, so a message of it still in flight is
   * dropped; until then its pages keep arriving (a query with errors keeps the
   * last good results, Load more included).
   */
  searchReplaced: boolean;
  done: SearchDoneMessage | undefined;
  /** Raw text of the last query that parsed without errors; its results stay while the box has errors. */
  lastGoodText: string;
  /** The selected result's ref ("" when none). */
  selectedRef: string;
  preview: PreviewState | undefined;
  /** Whether a commit preview also lists the files an f: filter hid. */
  showHiddenFiles: boolean;
  /** Recent queries, newest first. */
  recent: string[];
  /** Pinned queries, in the order they were pinned (spelunk.ui.pinnedQueries). */
  pinned: PinnedQuery[];
  /** The empty box row ↑↓ has selected, counting the pinned queries first, then the recent ones. */
  recentIndex: number;
  /** The pinned query whose name is being typed, if any. */
  naming: string | undefined;
  ui: UiSettings;
  repos: RepoStatus[];
  /** Daemon health; undefined while it is healthy. */
  banner: BannerMessage | undefined;
  /** The width in pixels the divider gave the recent queries; undefined for the default split. */
  recentWidth: number | undefined;
  /** Each facet bucket seen, by filter, so a bucket the query leaves out (and so finds nothing) still shows. */
  facetLabels: Map<string, { field: Facet["field"]; label: string }>;
  /** The facets whose "+N" showed every bucket. */
  expandedFacets: Set<Facet["field"]>;
}

/**
 * The settings used until the host's first state.restore arrives. They match
 * the defaults in extension/package.json, which the host sends in its place.
 */
const DEFAULT_UI_SETTINGS: UiSettings = {
  typingDelayMs: 120,
  openTrigger: "doubleClick",
  preview: true,
  showParsedQuery: true,
  caseSensitive: "off",
  wholeWord: false,
  order: "best",
};

/** The state of a panel that has just opened: an empty box and no results. */
export function createViewState(): ViewState {
  return {
    seq: 0,
    parsed: undefined,
    completions: [],
    searchText: undefined,
    completionIndex: 0,
    completionsOpen: false,
    searchId: "",
    searchReplaced: false,
    done: undefined,
    lastGoodText: "",
    selectedRef: "",
    preview: undefined,
    showHiddenFiles: false,
    recent: [],
    pinned: [],
    recentIndex: 0,
    naming: undefined,
    ui: { ...DEFAULT_UI_SETTINGS },
    repos: [],
    banner: undefined,
    recentWidth: undefined,
    facetLabels: new Map(),
    expandedFacets: new Set(),
  };
}

/** The empty box's rows, top to bottom: the pinned queries, then the recent ones not pinned. */
export function emptyBoxQueries(state: ViewState): string[] {
  const pinned = state.pinned.map((entry) => entry.query);
  const kept = new Set(pinned);
  return [...pinned, ...state.recent.filter((query) => !kept.has(query))];
}

/** Whether the parsed query has errors; its results are then the last good query's. */
export function hasErrors(state: ViewState): boolean {
  return state.parsed?.diagnostics.some((diagnostic) => diagnostic.severity === "error") ?? false;
}

/** A repo's display name, or its id before index.status has named it. */
export function repoName(state: ViewState, repoId: string): string {
  return state.repos.find((repo) => repo.repoId === repoId)?.name ?? repoId;
}
