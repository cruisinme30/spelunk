// Everything the panel knows. Only SearchPanel (panel.ts) changes it;
// render functions read it.
import type {
  BannerMsg as BannerMessage,
  Completion,
  ParsedQuery,
  Preview,
  RepoStatus,
  SearchDoneMsg as SearchDoneMessage,
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
  /** Recent queries, newest first, and the one ↑↓ has selected. */
  recent: string[];
  recentIndex: number;
  ui: UiSettings;
  repos: RepoStatus[];
  /** Daemon health; undefined while it is healthy. */
  banner: BannerMessage | undefined;
  /** The width in pixels the divider gave the recent queries; undefined for the default split. */
  recentWidth: number | undefined;
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
  caseSensitive: false,
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
    recentIndex: 0,
    ui: { ...DEFAULT_UI_SETTINGS },
    repos: [],
    banner: undefined,
    recentWidth: undefined,
  };
}

/** Whether the parsed query has errors; its results are then the last good query's. */
export function hasErrors(state: ViewState): boolean {
  return state.parsed?.diagnostics.some((diagnostic) => diagnostic.severity === "error") ?? false;
}

/** A repo's display name, or its id before index.status has named it. */
export function repoName(state: ViewState, repoId: string): string {
  return state.repos.find((repo) => repo.repoId === repoId)?.name ?? repoId;
}
