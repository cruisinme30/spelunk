# Progress and milestone sign-off

Each milestone closes only when its exit gate from the [implementation plan](implementation-plan.md#milestones)
is demonstrated by tests that ran green. Run everything with `npm test` (which runs `scripts/test-all.sh`).

**Environment note.** This build happens offline: there's no npm, Go module proxy or GitHub access. So:

- VS Code itself can't be launched (`@vscode/test-electron` isn't installed). L4 end-to-end tests inside a real
  VS Code window are listed as **not yet run** wherever they apply. Everything below VS Code (daemon, extension
  host logic, webview) is tested for real.
- The extension type-checks against `extension/types/vscode-shim.d.ts`, a shim of the VS Code API it uses
  (`tsconfig.offline.json`). On a machine with `npm install` done, the real `@types/vscode` is used instead; that
  was run on the maintainer's Mac and passed at M0.

## M0 · Skeleton — done

Exit gate: *one hard-coded search round-trips.* Scope: daemon spawned over stdio, JSON-RPC handshake, webview
shell, `protocol/` types generated, opening a file at a line works.

| Scope item | Evidence |
| --- | --- |
| Daemon spawned over stdio, JSON-RPC handshake | `extension/src/test/daemon.test.ts`: spawns the real binary, `initialize` returns the version and protocol 1 |
| Crash recovery (failure table) | same test: SIGKILL 3× → restarts each time; a 4th kill in the minute → `stopped`; manual Restart → `ok`; `stop()` → shutdown + exit |
| Content-Length framing, cancellation | `daemon/internal/rpc/conn_test.go` (framing, call/notify, `$/cancelRequest` → -32800, panic → -32603); `extension/src/test/jsonRpc.test.ts` (frames split across chunks) |
| `protocol/` types generated | `node protocol/gen.mjs --check` in test-all; `daemon/internal/protocol/protocol_test.go` (unions emit only their variant's fields, required arrays are `[]`) |
| Webview shell | `webview/test/webview.test.mjs`: Playwright tests against the real bundle: ready/restore, typing → `query.changed`, results + preview, stale `seq` dropped, fix-it via ⌘., Aa edits text, Restart banner |
| **Hard-coded search round-trips** | `daemon/internal/server/search_test.go` (RPC level) and `extension/src/test/controller.test.ts` (host controller → real daemon): `query.changed` → `parse.result` → `search.batch` → `search.done`, then `result.select` → `preview.result` |
| Opening a file at a line | Same controller test: `result.open` resolves to `{path, line 3, column 14, length 12}` and calls `openTarget(…, "side")`; the panel hides (closeOnOpen); F4 reopens the next result. The final `showTextDocument` call is VS Code glue in `extension.ts` (**not yet run**: needs VS Code) |
| Visual check | [`proof/search-panel-results-and-preview.png`](proof/search-panel-results-and-preview.png): real daemon results rendered by the real webview bundle |

The first search was a deliberate placeholder (one case-insensitive literal, found by scanning files). M1 replaced it
with the working-tree engine.

## Conventions pass (between M0 and M1): done

Requested before M1: research open-source practice and make the code easy to read and review.

| Area | What changed | Evidence |
| --- | --- | --- |
| Commits | Conventional Commits with directory scopes; the 12 earlier messages rewritten (contents unchanged: identical tree hash) | `git log --oneline`; every commit since passes `scripts/test-all.sh` on its own (checked in a clean worktree) |
| Go layout | Module `github.com/cruisinme30/spelunk/daemon`, packages under `internal/`, a `doc.go` per package, `.golangci.yml` | `go vet` and tests pass |
| TypeScript | camelCase whole-word files, tests in `src/test/`, `verbatimModuleSyntax`, webview `main.ts` split into state, host, layout and `render/` modules | 11 extension and 10 webview tests pass; tsc strict |
| Tooling | npm workspaces at the root, ESLint (typescript-eslint, type-aware) and Prettier configs, `.editorconfig`, `.gitattributes` | `prettier --check` in test-all; ESLint needs `npm install` (not available offline) |
| Docs | README, ARCHITECTURE, CONTRIBUTING, CHANGELOG, SECURITY, CODE_OF_CONDUCT, MIT LICENSE, `.github/` templates; docs in Diátaxis folders; ADRs 0001-0003 | `docs/README.md` |
| Review | An independent readability review found 10 bugs and about 30 readability issues; all fixed. Each bug has a regression test that fails on the old code | See below |

Bugs found by the review, each now covered by a test:

1. A reopened panel dropped every query: the host never reset its last `seq`.
2. "1 file hidden" rendered as "1 fil hidden".
3. Match columns were wrong (or panicked) where lowercasing changes byte length (U+212A KELVIN SIGN).
4. `SPELUNK_NOW` made search durations come out as years.
5. Previews of CRLF files kept the `\r`.
6. A malformed ref became line 0 instead of `RefStale`.
7. `rpc.Call` waited forever if the peer ignored `$/cancelRequest`.
8. A frame without `Content-Length` wedged the extension's JSON-RPC decoder.
9. `index.location` `~alice/idx` was expanded as if it were `~/`.
10. The coverage step in test-all could fail without showing why.

Spec coverage after this pass: 24/94 (`node scripts/specCoverage.mjs`). M1 adds the operators, syntax and diagnostics.

## M1 · Query language + tree search — done

Exit gate: *mocks 1, 2, 8 and 13; parse under 15 ms.* Scope: parser, diagnostics and fix-its for every rule; an index
for the working tree with text, `f:`, `case:` and regex; results, preview, double-click to open.

The plan named Zoekt for the index. It can't be fetched offline, so the daemon has its own trigram index
([ADR 0003](../adr/0003-in-house-trigram-index.md)); it covers every root, not just one.

| Scope item | Evidence |
| --- | --- |
| Parser, every operator and syntax rule | `daemon/internal/query/parser_test.go`, `golden_test.go` (11 mock queries, reviewed golden trees): spec coverage `op` 10/10, `syntax` 6/6 |
| Diagnostics and fix-its for every rule | `diagnostics_test.go`: all 16 diagnostic codes, each fix applied and re-parsed (`diag` 16/16) |
| Completions | `complete_test.go`: operators by prefix, values, authors; nothing once a value is typed in full |
| Planner (Contract 4) | `plan_test.go`: lowering, `since:` windows, filters with undo fixes, `type:` and `case:yes` notes, history scan warning |
| File listing and excludes | `trigram/walk_test.go`, `glob_test.go`: Git ignore rules, `index.includeIgnored`, `index.exclude`, size limit, binaries |
| Index build, save, load | `trigram/shard_test.go`: candidates, atomic save, format version check, corrupt file |
| Engine semantics | `trigram/engine_test.go`: AND per file, OR as union, NOT, `f:`, `case:`, regex with `^` per line, `lang:`, `repo:`, `type:file`, paging, UTF-16 hits, long lines clipped, budget and cancel |
| No match is ever lost | `trigram/ripgrep_test.go`: every line found by ripgrep on a generated corpus (11 queries, Unicode folds included) is found by the engine, and nothing else. It caught a real bug: `k`/`s` trigrams dropped KELVIN SIGN and LONG S matches |
| Indexing per root | `indexer/indexer_test.go`: progress, saved shards serve the next session at once, roots dropped, settings rebuild, missing folder → error status |
| Results stream, preview, open | `server/search_test.go` (RPC), `extension/src/test/controller.test.ts` (host controller → real daemon): batches, Load more cursor, preview with every term highlighted, `open/resolve` to line and column, stale refs |
| Double-click to open | `webview/test/webview.test.mjs`: one click selects, a double-click sends `result.open`; Enter and ⌘Enter too. The `showTextDocument` call itself needs VS Code (**not yet run**) |
| A search typed while indexing | `controller.test.ts`: the search reruns when a repo becomes ready |

**Mocks.** `testdata/workspace` holds the three repos from the mocks. `TestMockQueriesOverTheFixtureWorkspace` checks
every result and hidden note for mocks 1, 2 and 8 through the real indexer and engine; webview tests check mock 13's
behaviour. The screenshots are the real daemon, host controller and webview bundle together
(`node scripts/screenshotPanel.mjs`), to compare with the [mocks](https://claude.ai/artifact/GaBrpZJxgp9ZkJPKZqdq7F):

| Mock | Query | Result | Screenshot |
| --- | --- | --- | --- |
| 1 | `retry_policy` | 3 file names; 5 code matches in 3 files; line 42 previewed with every match marked | [plain-text-results.png](proof/plain-text-results.png) |
| 2 | `f:.*test\.py$ timeout` | 10 matches in 4 files, only `…test.py`; `TimeoutError` matches (case off); "Code in matching paths" | [path-scoped-results.png](proof/path-scoped-results.png) |
| 8 | `case:yes /Retry(Policy\|Config)/ lang:python` | 7 matches in 4 files; Aa and .* pressed; "1 match hidden by case:yes · Ignore case" | [case-regex-language-results.png](proof/case-regex-language-results.png) |
| 13 | `( timeout OR retry -f:vendor/ sinse:6m` | Both errors with spans and fixes; the last good results stay, labelled with their query | [query-errors-keep-last-results.png](proof/query-errors-keep-last-results.png) |

Mock 13's results in the canvas are commits; history search arrives in M3, so the proof uses a working-tree query.

**Parse under 15 ms.** `TestParseIsFastEnough` holds the 95th percentile of parse + plan + complete under 15 ms for the
mocks' queries and a 1,000-character query. `BenchmarkParse`: 8–20 µs per query, 0.2 ms at the length limit.

**Search speed** (not an M1 gate; the budget is first results within 100 ms). `BenchmarkLargeRepo` on Go's standard
library (about 10,000 files): first result in under 12 ms; complete searches, which count every match for the
totals and hidden notes, take 20–170 ms. A part-filled batch is sent after 20 ms, so first results don't wait for
the count.

Totals at sign-off: 96 Go tests, 12 extension tests, 15 webview tests; spec coverage 70/110 (the rest belong to
later milestones).

## M2 · Workspace scope + discovery — done

Exit gate: *mocks 4, 5, 12 and 17.* Scope: many repos; `repo:`, `lang:`, `type:`, `count:`; operator autocomplete,
cheat sheet, help page; hidden-result notes with undo.

| Scope item | Evidence |
| --- | --- |
| Many repos, `repo:`, `lang:`, `type:`, `count:` | Built in M1 and covered there (`trigram/engine_test.go`, `server/mocks_test.go`); M2 adds the repo menu (below) |
| Repo menu ("All repos · 3") | `webview/test/discovery.test.mjs`: picking a repo writes `repo:web-checkout` into the query, the button names it, "All repos" removes it |
| Operator autocomplete (mock 5) | Daemon: `complete_test.go` offers `since:` then `sym:` for `timeout s`. Webview: descriptions, example values, "Search for timeout and s as plain text", the results glimpse; Tab inserts at the cursor; Enter or Esc searches as typed. Host: `controller.test.ts` searches `timeout` while `s` is being completed, and `timeout s` once dismissed |
| Cheat sheet (mock 4) | `webview.test.mjs`: recent queries in order and all 16 operators |
| Help page (mock 17) | `discovery.test.mjs`: 16 operator rows and 5 examples; Try sends `help.try`, which `extension.ts` runs in the search panel. `extension/src/test/helpExamples.test.ts`: all 21 examples parse with no diagnostics against the real daemon, and every working-tree example finds results in the fixture workspace |
| Hidden-result notes with undo (mock 12) | `server/mocks_test.go`: `type:file lang:python retry` returns 5 file names and a note whose count equals what `type:code lang:python retry` returns (17, test plan E12). Webview: "17 code matches for retry hidden by type:file · Show code too", and the undo removes `type:file` |

Screenshots, rendered end to end by `scripts/screenshotPanel.mjs` over `testdata/workspace`:

| Mock | Screenshot |
| --- | --- |
| 4 | [empty-box-recent-and-operators.png](proof/empty-box-recent-and-operators.png) |
| 5 | [operator-suggestions.png](proof/operator-suggestions.png) |
| 12 | [file-names-only.png](proof/file-names-only.png) |
| 17 | [help-page.png](proof/help-page.png) |
| Repo menu | [repo-menu.png](proof/repo-menu.png) |

**Not yet:** five help examples need commit history (M3) or the symbol index (M4) before they find anything:
`author:jane timeout`, `msg:"fix flaky"`, `sym:RetryPolicy`, and two worked examples. `since:2w timeout` uses file
times until M4 brings Git's change dates, so it may find nothing on an old checkout. The mock 12 file rows show each
file's last commit, which comes with history (M3). Opening the help page and running Try need a VS Code window (**not
yet run**); everything under them is tested.

**Protocol (Contract 2, additive, no version bump):** `query.changed` may carry `asTyped`, `parse.result` may carry
`searchText`, and the help page sends `help.try`.

Totals at sign-off: 98 Go tests, 15 extension tests, 22 webview tests; spec coverage 77/111.

## M3 · History — done

Exit gate: *mocks 3, 6, 7 and 11; history under 250 ms.* Scope: commit ingest newest first; `author:`, `msg:`,
`since:` on commits; OR colors; author autocomplete with `.mailmap`.

The plan named SQLite FTS5 for the commit index. The Go standard library has no SQLite driver and modules can't be
fetched, so the history index reuses the trigram package ([ADR 0004](../adr/0004-in-house-history-index.md)).

| Scope item | Evidence |
| --- | --- |
| Ingest newest first, published as it fills | `history/history_test.go`: commits newest first with their changed lines and `.mailmap` applied; `TestUpdateReadsNewCommitsOrRereadsRewrittenHistory`; saved and loaded; a folder outside Git |
| `author:`, `msg:`, `since:`, `f:`, `lang:` on commits | `TestHistoryQueries` and `TestFiltersReportTheCommitsTheyHid` on a fixture repo with fixed authors and dates. A differential test against `git log -G` is not written yet |
| Files `index.exclude` leaves out | `TestSkippedFilesAreListedWithoutTheirLines`: listed and counted, lines not kept |
| Search, preview and open (mock 3) | `server/history_test.go` `TestHistorySearchPreviewAndOpen`: `author:jane timeout` → the one commit, its diff of `src/retry.py`, and an open target |
| Author and message suggestions (mocks 6, 19) | `TestAuthorAndMessageSuggestionsComeFromHistory`: `author:ja` → Jane (2 commits, last 3 days ago) then Jason; `msg:fl` → flaky |
| AND / OR / NOT / `since:` with hidden notes (mock 7) | `TestBooleanHistoryQueriesCountWhatTheyHide`: `type:commit (timeout OR attempts) -f:tests/ since:2w` → one commit, "1 hidden by -f:tests/" and "1 hidden by since:2w" |
| `msg:"phrase"`, `repo:`, `count:` (mock 11) | `TestMessagePhraseRepoAndCount`: three commits across two repos newest first; with `repo:` and `count:1` the newest of two and a next page |
| OR colors | Each commit result carries the terms it matched (`matchedTerms`); the webview colors them, as for files |
| A folder outside Git | `TestAFolderOutsideGitSaysHistoryIsOff` (`failure:no-git`) |

**History under 250 ms.** `BenchmarkLargeHistory` on Prometheus (8,532 first-parent commits, all of its history,
default `index.exclude`): the newest 200 commits are searchable 0.6 s after reading starts, and all of them after
18 s; the index takes 188 MB. First results arrive in under 1 ms, because results stream newest first as they are
found; complete searches, which count every match and hidden commit, take 5–65 ms. Before streaming and the shared
line matcher, the same queries took up to 2 s and the index 785 MB.

Screenshots, rendered end to end by `scripts/screenshotPanel.mjs` over a fixture repo:

| Mock | Screenshot |
| --- | --- |
| 3 | [author-history.png](proof/author-history.png) |
| 19 | [author-values.png](proof/author-values.png) |

**Also done from M4:** `since:` on current files uses Git's change dates and uncommitted edits
(`TestSinceOnFilesUsesCommitDatesAndUncommittedEdits`), and saved files go into an overlay that is searchable at once
and folded into the next build (`TestSavedFilesAreSearchableWithoutARebuild`,
`TestSavedFilesGoIntoTheOverlayUntilTheNextBuild`). Failure modes now covered: `index-corrupt` (rebuilt, with a
message) and `disk-full` (keeps serving from memory, with a warning).

Totals at sign-off: spec coverage 89/114.

## M4 · Freshness + symbols — done

Exit gate: *mocks 9, 10 and 14; saves findable in 1 s.* Scope: dirty-file overlay, tombstones, compaction;
`file_touch` and `since:` on current files; `sym:` through universal-ctags.

universal-ctags can't be installed offline, so `daemon/internal/symbols` finds definitions with a few regexes per
language, read line by line as ctags does for most languages. It covers Python, Go, TypeScript, JavaScript, Java, C#,
Kotlin, Scala, Swift, Rust, Ruby, PHP, C and C++.

| Scope item | Evidence |
| --- | --- |
| Overlay of saved files, masking their old copies | `indexer/freshness_test.go` `TestSavedFilesGoIntoTheOverlayUntilTheNextBuild`: a saved file, a new one and a deleted folder; the next build folds them in |
| Compaction | The overlay is folded into a quiet rebuild after 500 files (`maxOverlayFiles`), and at every build |
| Saves findable in 1 s | `server/history_test.go` `TestSavedFilesAreSearchableWithoutARebuild`: `workspace/didChangeFiles`, then the new text is found at once |
| `since:` on current files (mock 10) | `TestSinceOnFilesUsesCommitDatesAndUncommittedEdits`: commit dates decide, and an uncommitted edit counts as now |
| Definitions per language | `symbols/symbols_test.go`: classes, interfaces, types, functions and methods in 8 languages; minified lines skipped |
| `sym:` (mock 9) | `server/symbols_test.go` `TestSymbolSearchFindsDefinitionsAndOffersTheTextSearch`: `sym:RetryPolicy` → 4 definitions in 3 repos with their kinds; the preview lists the class's members; "Search RetryPolicy as text" counts what running it finds |
| No results while indexing (mock 14) | `TestEverySuggestionForNoResultsCountsWhatItFinds`: for `case:yes sym:retrypolicy`, every note's count equals what its undo finds. `webview/test/discovery.test.mjs`: the indexing banner, "No symbol is named retrypolicy", why, and the notes |
| `sym:` suggestions | `query/values_test.go` `TestSymbolSuggestionsNameTheDefinitions` |

Screenshots, rendered end to end by `scripts/screenshotPanel.mjs`:

| Mock | Screenshot |
| --- | --- |
| 9 | [symbol-definitions.png](proof/symbol-definitions.png) |
| 14 | [no-results.png](proof/no-results.png) (the indexing banner is in the webview test; a screenshot can't hold indexing at 64%) |

**Changed from the plan:** `sym:RetryPolicy` matches names that contain RetryPolicy, as the mock shows
(`RetryPolicyError`, `RetryPolicyConfig`), rather than only the exact name.

## M5 · Setup + release — done, except what needs a real VS Code or the release secrets

Exit gate: *mocks 15, 16 and 18; every budget green.* Scope: first-run page and shortcut presets; settings, error
banners, daemon restarts; per-platform `.vsix` on the Marketplace and Open VSX.

| Scope item | Evidence |
| --- | --- |
| First-run page (mock 15) | `webview/test/welcome.test.mjs`: the three shortcut choices with the platform's keys, each repo's indexing, and every choice sent to the host; `extension/src/test/settings.test.ts` maps the saved settings back to the page. [first-run.png](proof/first-run.png) |
| Shortcut presets | `extension/src/test/manifest.test.ts`: the keys each preset binds, and none for `none` |
| Opened file (mock 16) | `controller.test.ts` "opening a result says which of the results it is": the editor target, "2 of 3", F4 and ⇧F4 in turn, and ⌘P restoring the query |
| Settings take effect without a reload (mock 18) | `server/settings_test.go`: case, page size and a new exclude pattern over `settings/update`; `indexer/settings_test.go`: `index.symbols`, `index.location`, `index.historyDepth`; extension and webview tests for the UI settings. Every setting has a test |
| Banners and restarts | Built in M0 and M1 (`daemon.test.ts`, `webview.test.mjs`) |
| Every command | `extension/src/e2e/commands.test.ts`, run in a real VS Code by `scripts/e2e.mjs` and by CI's e2e job. **Not yet run:** VS Code can't be downloaded in this environment |
| Per-platform packages and publishing | `.github/workflows/release.yml`, on a `v*` tag only: `scripts/packageExtension.mjs` builds all six platforms, attached to a GitHub release, and published to the Marketplace and Open VSX when `VSCE_PAT` and `OVSX_PAT` are set. Ran for v0.1.0 through v1.0.0, attaching the packages to each GitHub release. **Not yet published:** `VSCE_PAT` and `OVSX_PAT` aren't set, so the Marketplace and Open VSX steps are skipped |
| Budgets | `scripts/checkBudgets.mjs` on Prometheus: every search's first result well under its budget (under 3 ms for current files, under 1 ms for history), the working tree indexed in 1.6 s, history read in 18–31 s with the newest commits searchable within 11 s. The release workflow repeats the check before it publishes |

Spec coverage at sign-off: 119/119 (`node scripts/specCoverage.mjs --strict` passes).

## Screenshots for the remaining mocks

Rendered by `scripts/screenshotPanel.mjs` over a copy of `testdata/workspace` with a few weeks of commits by Jane Doe,
Jason Kim and Marta Ruiz, and one uncommitted edit:

| Mock | Query | Screenshot |
| --- | --- | --- |
| 6 | `timeout author:ja` | [value-suggestions.png](proof/value-suggestions.png) |
| 7 | `author:jane (timeout OR retry) -f:vendor/ since:6m` | [boolean-history.png](proof/boolean-history.png) |
| 10 | `since:2w timeout` | [since-on-files.png](proof/since-on-files.png) |
| 11 | `msg:"fix flaky" repo:web count:3` | [message-repo-count.png](proof/message-repo-count.png) |
| 20 | `timeout since:` | [since-values.png](proof/since-values.png) |
| 21 | `timeout f:` | [path-values.png](proof/path-values.png) |
| 22 | `timeout lang:` | [other-values.png](proof/other-values.png) |
| 23 | `author:jane retry` | [words-in-messages.png](proof/words-in-messages.png) |
| 24 | `retry` | [ranked-results.png](proof/ranked-results.png) |
| 25 | `retry order:path` | [path-order.png](proof/path-order.png) |
| 26 | `retry is:test` | [test-files.png](proof/test-files.png) |
| 27 | `author:jane is:test retry` | [test-history.png](proof/test-history.png) |

Mocks 16 (the opened file) and 18 (Settings) are VS Code's own editor and Settings UI, which the tool can't draw
without VS Code, so they have no screenshot.
