// Everything the panel knows. Only SearchPanel (panel.ts) mutates it;
// render functions read it.
import type {
  BannerMsg,
  Completion,
  ParsedQuery,
  Preview,
  RepoStatus,
  SearchDoneMsg,
  UiSettings,
} from "./protocol.gen";

export interface PreviewState {
  ref: string;
  preview: Preview | null;
  stale?: boolean;
}

export interface ViewState {
  /** Sequence number of the newest query.changed; older responses are dropped. */
  seq: number;
  parsed?: ParsedQuery;
  completions: Completion[];
  completionIndex: number;
  completionsOpen: boolean;
  /** The search whose results are on screen ("" when none). */
  searchId: string;
  done?: SearchDoneMsg;
  /** Raw text of the last query that parsed without errors (mock 13). */
  lastGoodText: string;
  selectedRef: string;
  preview?: PreviewState;
  showHiddenFiles: boolean;
  recent: string[];
  recentIndex: number;
  ui: UiSettings;
  repos: RepoStatus[];
  banner?: BannerMsg;
  sheetOpen: boolean;
}

export function createViewState(): ViewState {
  return {
    seq: 0,
    completions: [],
    completionIndex: 0,
    completionsOpen: false,
    searchId: "",
    lastGoodText: "",
    selectedRef: "",
    showHiddenFiles: false,
    recent: [],
    recentIndex: 0,
    ui: { typingDelayMs: 120, openTrigger: "doubleClick", preview: true, showParsedQuery: true, caseSensitive: false },
    repos: [],
    sheetOpen: false,
  };
}

export function hasErrors(state: ViewState): boolean {
  return state.parsed?.diagnostics.some((d) => d.severity === "error") ?? false;
}

export function repoName(state: ViewState, repoId: string): string {
  return state.repos.find((r) => r.repoId === repoId)?.name ?? repoId;
}
