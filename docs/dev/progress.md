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
| Repo menu ("All repos · 3") | `webview/test/discovery.test.mjs`: picking a repo writes `repo:^web-checkout$` into the query, the button names it, "All repos" removes it |
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
| 25 | `retry order:file` | [path-order.png](proof/path-order.png) |
| 26 | `retry is:test` | [test-files.png](proof/test-files.png) |
| 27 | `author:jane is:test retry` | [test-history.png](proof/test-history.png) |
| 28 | `word:yes retry` | [whole-words.png](proof/whole-words.png) |
| 29 | `case:smart Retry` | [smart-case.png](proof/smart-case.png) |
| 30 | `retry`; `type:commit timeout author:"Jane Doe" -repo:^shared-libs$` after `type:commit timeout` | [result-facets.png](proof/result-facets.png), [result-facets-commits.png](proof/result-facets-commits.png) |
| 31 | An empty box with a named and an unnamed pin, the second being named | [pinned-queries.png](proof/pinned-queries.png) |
| 32 | `RetryPolicy`, replaced with `BackoffPolicy` (`--replace`) | [replace-in-files.png](proof/replace-in-files.png) |
| 33 | `match:fuzzy cfg`; `cfg` | [fuzzy-file-names.png](proof/fuzzy-file-names.png), [fuzzy-file-names-note.png](proof/fuzzy-file-names-note.png) |
| 34 | `type:added retry_policy`, over a history where one commit only removed `retry_policy` lines | [added-removed-lines.png](proof/added-removed-lines.png) |
| 35 | `sym:retry kind:class`, `ref:RetryPolicy` | [symbol-kinds.png](proof/symbol-kinds.png), [references.png](proof/references.png) |
| 36 | `type:code f:http/ retry` with `--setting results.contextLines=2` | [context-lines.png](proof/context-lines.png) |
| 37 | `RetryPolicy -is:generated -is:vendored`, replaced with `BackoffPolicy`, and `RetryPolicy -is:`, over the workspace plus a protoc `retry_pb2.py`, a `third_party/` copy and a `dist/` bundle | [generated-vendored.png](proof/generated-vendored.png), [generated-vendored-values.png](proof/generated-vendored-values.png) |
| 38 | An empty box with a watched pin, then `msg:revert f:payments/`, after two new reverts by others (`--new-commit`) | [query-watches.png](proof/query-watches.png), [query-watches-results.png](proof/query-watches-results.png) |
| 41 | `at:v1.0.0 retry_after` with `--select 1`, and `retry_after at:`, over a copy of the workspace whose payments-api is tagged v1.0.0 and later deleted `backoff.py` and changed `client.py`, whose web-checkout has no such tag, and whose shared-libs isn't a Git repository | [past-revision.png](proof/past-revision.png), [past-revision-values.png](proof/past-revision-values.png) |
| 42 | `retry owner:payments-core` with `--key Escape --select 2`, `retry owner:`, and `retry owner:payment-core`, over the workspace, whose payments-api has a `.github/CODEOWNERS` | [owner-filter.png](proof/owner-filter.png), [owner-values.png](proof/owner-values.png), [owner-warning.png](proof/owner-warning.png) |

Mocks 16 (the opened file) and 18 (Settings) are VS Code's own editor and Settings UI, which the tool can't draw
without VS Code, so they have no screenshot.

## Context lines (1.2)

Mock 36 (`context-lines`). Exit gate: with `spelunk.results.contextLines` above 0, every code result on the page
carries the lines around its match, the panel shows them merged into runs, and the Context buttons change the setting
end to end.

- Daemon: `daemon/internal/trigram/context_test.go` (line ends, file edges, clipping, pages, best-match order, a
  changed file's current text) and `TestTheContextLinesSettingAddsLinesAroundCodeResults` over `settings/update`.
- Panel: `webview/test/contextLines.test.mjs` (runs, overlaps, Load more, files, out-of-order matches, ↑ ↓, screen
  readers, the replace preview's indent, the Context buttons).
- Host: `controller.test.ts` (`context.set` saves only a change, bad payloads are dropped) and `settings.test.ts`.
- End to end in VS Code: "The panel's Context buttons save the setting, and the results gain context lines" in
  `extension/src/e2e/commands.test.ts`.

## Generated and vendored files (1.2)

Mock 37 (`generated-vendored`). Exit gate: `is:generated` and `is:vendored` keep or (negated) leave out the files a
tool wrote and the ones copied in, by the same checks best-match ranking uses, in current files and in commits, and
say what they left out.

- Detection: `daemon/internal/lang/generated_test.go` (names, `DO NOT EDIT` and `@generated` headers, protoc's
  Python output, a hand-written "do not edit" note, a marker past the first kilobyte).
- Current files: `TestIsGeneratedAndIsVendoredFilterFiles` and `TestIsGeneratedAndIsVendoredSayWhatTheyLeftOut` in
  `daemon/internal/trigram`.
- Commits: `TestIsGeneratedAndIsVendoredInHistory` in `daemon/internal/history/history_test.go` (by path, and a
  header-only generated file counting as written by people).
- Suggestions: `TestGeneratedAndVendoredSuggestionsCountTheirFiles` in `daemon/internal/query/values_test.go`, and
  `TestIsGeneratedReadsTheIndexedHeaders` over JSON-RPC in `daemon/internal/server/search_test.go`.

## Query watches (1.2)

Mock 38 (`query-watches`). Exit gate: the bell watches a pinned query that searches history; after a history update,
the daemon runs it over the commits the update brought in (not your own, not a first read, not a switch to an older
branch) and announces what matched; the pin counts them, a notification offers Show and Stop watching (unless
`spelunk.watch.notify` is off), and the query's next run tags them New.

- New commits: `daemon/internal/history/watch_test.go` (forward moves, rewrites and branch switches, a store of the new
  commits alone) and `TestAHistoryUpdateAnnouncesTheCommitsItBroughtIn` in `daemon/internal/indexer`.
- Watched queries: `daemon/internal/server/watches_test.go` (`initialize`'s watches and `watch/set`, others' commits
  only, the query's own filters and count, queries over current files ignored).
- Host: `extension/src/test/watches.test.ts` (counts, repeats, the saved state, the pop-up text), the watch tests in
  `controller.test.ts` (the pop-up's buttons, `pinned.watch` for history queries only, renames keeping the watch, New
  on every page, and a real daemon announcing a commit) and `settings.test.ts`.
- Panel: `webview/test/watches.test.mjs` (the three bells, the count, ⌥W, New tags on every page and only for that
  search) and malformed `watch.state` in `untrustedInput.test.mjs`.
- End to end in VS Code: "The panel's bell watches a pinned history query in the setting, and only a history query"
  in `extension/src/e2e/commands.test.ts`.

## Ownership filters (1.2)

Mock 42 (`owner-filter`). Exit gate: `owner:` keeps the current files a repo's CODEOWNERS file gives an owner, by
GitHub's rules, and `owner:none` the files it gives none; it says what it left out, suggests the owners with their
counts, warns when it can match nothing, and the results, the facet row and the preview name the owners.

- CODEOWNERS: `daemon/internal/codeowners/codeowners_test.go` (GitHub's documented example, anchoring, folders,
  `docs/*`, `**`, escaped spaces, case, the lines GitHub skips, a line with no owners, which of the three files counts,
  the 3 MB limit, and how a value names an owner).
- Query: `TestOwnerIsAFilterThatCountsWhatItHid` and the lowering row in `daemon/internal/query/plan_test.go`; the
  history-query and regex rows in `diagnostics_test.go`; `TestOwnerWarnsWhenItCanMatchNothing` and
  `TestOwnerWarnsWhenNoRepoHasCodeOwners` in `parser_test.go`; the two owner tests in `values_test.go`.
- Current files: `daemon/internal/trigram/owners_test.go` (who matches, a repo without CODEOWNERS, the hidden note,
  each result's owners, the Owner facet, and the preview's deciding line).
- Index: `daemon/internal/indexer/owners_test.go` (read with each build, again when the editor saves, deletes or adds
  a CODEOWNERS file, with a saved index, and never from other folders).
- Over JSON-RPC: `TestOwnerReadsTheReposCodeOwnersFile` in `daemon/internal/server/search_test.go`.
- Panel: `webview/test/owners.test.mjs` (badges, the Owner facet, the preview's line, the hidden note), and the help
  guide's `owner:payments-core retry` runs cleanly against the fixture in `extension/src/test/helpExamples.test.ts`.

## Multiline regex (1.2)

Mock 39 (`multiline-regex`). Exit gate: a regex with `\n` matches across lines of current files, as ripgrep's `-U`
does, with "\r\n" read as "\n"; each match is one code result showing the lines it spans (up to ten, then where it
ends), matches sharing a line join, context lines sit around the whole span, the preview marks every line of it,
and opening it selects all of it. Every other regex still matches one line at a time; a history query warns, and
Replace says why it can't change such a match.

- Query: `daemon/internal/query/multiline_test.go` (what spans lines, the m flag, relaxed plans, newline-free
  narrowing literals, and the `multiline_in_history` warning).
- Current files: `daemon/internal/trigram/span_test.go` (spans, one-line matches, anchors, CRLF, joining with other
  terms, long matches, UTF-16 columns, clipping, context, NOT, `word:`, `case:` and `f:`, history's line finder, the
  preview and the open target, and 200,000 joined matches in bounded time), and `TestMatchesAcrossLinesLikeRipgrep`
  in `ripgrep_test.go` (seven patterns against `rg -U`; it skips without `rg` on the PATH).
- Over JSON-RPC: `TestAMatchAcrossLinesRoundTrips` in `daemon/internal/server/search_test.go` (a CRLF file through
  search, preview and open), and the replace refusal in `replace_test.go`.
- Panel: `webview/test/multilineRegex.test.mjs` (rows, the "more lines" line, context, the preview's focus and header,
  the glimpse, Replace), and span lines in `untrustedInput.test.mjs`. The help guide's new example finds results over
  the fixture in `extension/src/test/helpExamples.test.ts`.
- End to end in VS Code: "Opening a match across lines selects all of it" in `extension/src/e2e/commands.test.ts`.
- Built: `docs/dev/proof/multiline-regex.png`, from
  `node scripts/screenshotPanel.mjs --query '/for attempt in .*:\n\s*try:/' --setting results.contextLines=1`.

## Structural search (1.2)

Mock 40 (`structural-search`). Exit gate: `struct:"…"` takes a template of code with `:[holes]` and finds code of that
shape in current files, read by each file's language, so a call is found however its arguments are laid out over
lines, and code in comments and strings that looks like it is not. Each match is one code result showing the lines it
spans, with the template's code and each hole's code marked apart and a chip for what each hole took; the preview
marks the same and opening it selects the whole match. Bad templates and history queries say what's wrong, Replace
says why it can't change a template, and no template can hold a search.

- Matcher: `daemon/internal/structural/` — `template_test.go` (hole names, what makes a template bad, unpaired
  brackets, the words narrowing uses), `lex_test.go` (every language's strings and comments, unclosed quotes, escaped
  line ends, lines and joins, Unicode words, bracket pairs), `match_test.go` (holes over lines, spaces, joined
  punctuation, strings and comments, laziness, empty holes, repeated names, the line rule, case, overlap, limits, code
  ranges, CRLF, the step budget and a 40,000-line file in bounded time, side-by-side use), and `FuzzLex` and
  `FuzzFind` (ran for a minute each: tokens in order, matches apart and inside the text, and every match holding the
  template's words, which keeps narrowing sound).
- Query: `daemon/internal/query/structural_test.go` (the text term, bad values in words, the history error, the
  lowered term and its narrowing literal, plain-text Find methods, `word:yes`).
- Current files: `daemon/internal/trigram/structure_test.go` (marks and hole values across lines, the one-line
  selection, squeezed and cut values, joined matches, NOT, `lang:`, `f:`, `case:`, file names, CRLF, context, the
  preview, a slow template), the fixture query in `designs_test.go`, `TestAStructMatchRoundTrips` in
  `daemon/internal/server/search_test.go` (search, preview and open over JSON-RPC), and the replace refusal in
  `replace_test.go`.
- Panel: `webview/test/structuralSearch.test.mjs` (rows, hole colors and chips, the chip label, the preview, Replace,
  the sheet's insert), the `.*` toggle in `queryEdit.test.mjs`, and the sheet's example over the fixture in
  `extension/src/test/helpExamples.test.ts`.
- End to end in VS Code: "Opening a struct: match selects all of it" in `extension/src/e2e/commands.test.ts`.
- Built: `docs/dev/proof/structural-search.png`, from
  `node scripts/screenshotPanel.mjs --query 'struct:"backoff(:[call], tries=:[n])"'`.

## Past revisions (1.2)

Mock 41 (`past-revision`). Exit gate: `at:` names a tag, branch, commit or time, and the whole query searches each
repo's files as they were then, read from Git without an index, with everything that searches current files (text,
regex, `f:`, `sym:`, `struct:`, ranking, facets, context lines) working there. Results carry the commit in their refs
and say whether their file was deleted or changed since; the mode chip names the commit read and its date; a repo
without the revision, or outside Git, is left out and says why. The preview reads the file from the commit, opening a
result shows it read-only, and Compare with now diffs it against the file now. History queries, operators about files
as they are now, and Replace refuse it in words.

- Git: `daemon/internal/history/revision_test.go` (tags, branches, shas, `HEAD~1`; names that aren't commits, option
  lookalikes and a blob; a folder outside Git; the newest commit by a time; regular files under a root, without
  symlinks and submodules; batched blob reads in order, a missing object, stopping on an error; one file at a commit;
  tags newest first).
- Query: `daemon/internal/query/revision_test.go` (the global and its mode, revision names and times as values, what
  isn't one, once at the top level, the history error, `op_with_at` for `is:open`, `is:changed`, `since:`, `until:`
  and `owner:`, the planned time, and tag-then-time suggestions).
- Files at a commit: `daemon/internal/trigram/revision_test.go` (refs at a commit, deleted and changed since for
  lines, file names and definitions, the overlay's current text, blobs read as Build reads files, the preview and open
  target), and full-sha refs in `ref_test.go`.
- Over JSON-RPC: `daemon/internal/server/revisions_test.go` (a deleted file found, the commit each repo read, times
  picking the newest commit by then, repos without the revision or outside Git, preview, open and `revision/text` of a
  deleted file, stale refs, the kept shards, `index.exclude`, the replace refusal, tag suggestions, and no owners at a
  revision).
- Host: the real-daemon test in `extension/src/test/controller.test.ts` (`search.done`'s revisions, `compare`, the
  open target and `revision/text`) and `revisionPath.test.ts` (document names and the diff's title).
- Panel: `webview/test/pastRevision.test.mjs` (the mode chip before and after the search, several commits, a typed
  sha, the badges, repos without the revision, the read-only preview and Compare with now, on and off).
- End to end in VS Code: "Opening a result at a revision shows the file as it was, read-only, or compared with the
  file now" in `extension/src/e2e/commands.test.ts`.
- Cost: nothing is indexed ahead of time. The first search of a revision reads each repo's files with one `git ls-tree`
  and one `git cat-file --batch`, all repos in parallel (`TestAtReadsTheReposRevisionsInParallel`), and indexes them in
  memory (this repo's 364 files at `HEAD~30`: under 0.1 s of Git);
  later searches of it reuse that shard, and the two newest are kept. It hasn't been timed on a large repo yet.
- Built: `docs/dev/proof/past-revision.png` and `docs/dev/proof/past-revision-values.png`, from
  `node scripts/screenshotPanel.mjs --workspace <the tagged copy> --query 'at:v1.0.0 retry_after' --select 1` and
  `--query 'retry_after at:'`.

## Syntax colors and whitespace (2.1)

Mock 43 (`syntax-colors`): code in results, context lines and the preview is colored by VS Code's own grammar engine
(vscode-textmate with vscode-oniguruma) running the grammars the installed extensions contribute, with the active
color theme's token colors and `editor.tokenColorCustomizations`. The host colors each batch and preview after posting
it (`syntax.lines`), so coloring never holds results up; the panel wraps the text already drawn, matches included. The
preview marks whitespace as `editor.renderWhitespace` says, and code follows `editor.tabSize`, `editor.fontSize` and
`editor.fontLigatures`.

- Host: `extension/src/test/syntaxColors.test.ts` (a contributed grammar and theme color a file by its extension or
  name; consecutive lines carry a block comment over; a theme's comments, includes, tokenColors file, renamed ids and
  customizations; batches and previews colored after they are posted, and from scratch on a theme change) and
  `settings.test.ts` (the editor settings followed, the panel's own settings winning, wrong types).
- Panel: `webview/test/syntax.test.mjs` (plain until the colors come, then in color with matches kept, a reset, code
  drawn again colored again, a replace preview left plain, each `renderWhitespace` mode, marks keeping the text and
  `editor.tabSize`).
- End to end in VS Code: "Code in results is colored with the theme's colors, by the grammar VS Code colors the editor
  with" in `extension/src/e2e/commands.test.ts` (the built-in theme and Python grammar, through the packaged
  `onig.wasm`).
- Cost: the grammar engine adds `dist/onig.wasm` (462 KB) and about 90 KB to `dist/extension.js`; the engine loads
  the first time code is colored, and each grammar the first time a file of its language shows.
- Built: `docs/dev/proof/syntax-colors.png` and `docs/dev/proof/syntax-colors-light.png`, from
  `node scripts/screenshotPanel.mjs --query 'retry_policy' --select 3 --theme "Dark Modern"` (and `"Light Modern"`).

## Outline in the preview (2.1)

Mock 44 (`file-outline`): a file preview's **Outline** button opens a column beside the code with every definition in
the file, nested as VS Code's Outline nests them, in place of the "In this file" line, which listed the first eight
names. The daemon builds the tree (`symbols.Outline`): a definition indented under a class, interface or module until
a line as far left ends it, a Go method under its receiver's type, a Rust method under the type its `impl` names; the
preview sends each definition's kind and depth. The panel marks the definition the match is in, opens one beside the
editor on a click, and keeps the Outline closed until opened, remembering the choice in the webview state.

- Daemon: `daemon/internal/symbols/outline_test.go` (Go receivers wherever the type is, Python indentation, Java
  nested classes, Rust `impl` blocks, a Ruby module, at most `MaxOutline` entries), the receiver checked in the
  extractor's fuzz test, and the outline of a `sym:` result's preview in `daemon/internal/server/symbols_test.go`.
- Panel: "the Outline lists the file's definitions, nested, closed until opened; a click opens one to the side" and
  "a file with no definitions has no Outline, and a reload keeps an open Outline open" in
  `webview/test/webview.test.mjs`.
- Built: `docs/dev/proof/file-outline.png` and `docs/dev/proof/file-outline-light.png`, from
  `node scripts/screenshotPanel.mjs --query 'retry_policy' --select 3 --theme "Dark Modern" --click-after
  '[data-testid="outline-toggle"]'` (and `"Light Modern"`).

## Share a result from its right-click menu (2.1)

Mock 53 (`share-result`): right-clicking a result shows VS Code's own menu, made for results through the webview's
`data-vscode-context`: Open, Open to the Side; Copy, Copy Path, Copy Relative Path; Copy SHA and Copy SHA and Subject
on a commit; Copy Permalink and Open on the repo's host, named after it (GitHub, GitLab, Bitbucket, Gitea, Azure
DevOps); Copy All Results; Export Results as text, Markdown, CSV or JSON; and Copy Link to This Search, a
`vscode://spelunk.spelunk/search?q=` link that opens Spelunk on the query. The daemon says what a permalink names
(`link/resolve`, in `internal/permalink`): the remote the branch tracks (else `origin`, else the only one), HEAD when
the remote has it or the newest commit of the remote's branch HEAD builds on, the path in the repository after a
rename, and the lines mapped there from the file on disk by git's own diff. The host turns that into each host's
address (`permalinks.ts`), and the panel learns each repo's host from `remotes.state`.

- Daemon: `daemon/internal/permalink/permalink_test.go` (a pushed line; a line moved by uncommitted edits; its own
  line changed; a match over lines; unpushed commits mapped to the remote's commit; a repo never pushed; a new file; a
  rename; a folder inside a bigger repo; an `at:` revision, pushed or not; a commit result; which remote: tracked,
  `origin`, the only one, or several to pick from; no Git, no remote; one `Linker` for many results) and
  `daemon/internal/server/links_test.go` (`repo/remotes`, `link/resolve` for a code line, an `at:` result and a commit,
  many refs in order, no remote, forged refs refused as stale).
- Host: `permalinks.test.ts` (every form of remote URL, credentials dropped; each host's line, range, file and commit
  address; `spelunk.permalinks.hosts` and names guessed; the menu's host per repo; why a result has no link),
  `resultText.test.ts` (Copy, both paths, SHA and subject; text, Markdown, CSV and JSON exports), `resultActions.test.ts`
  (each host's items, problems said in plain words, picking a remote, the clicked or selected result, Copy All with
  more found, exports with links, search links round-tripping any query, and one copy end to end with the real
  daemon), `allResults.test.ts` (an export's own pages) and the manifest's menu checks.
- Panel: `webview/test/resultMenu.test.mjs` (each row's `data-vscode-context`, the box keeping VS Code's own menu,
  `remotes.state` naming rows drawn before and after it).
- End to end in VS Code: "The result menu opens and copies the clicked result, and copies a link to the search" in
  `extension/src/e2e/commands.test.ts`. By hand in VS Code 1.141 on three Git repos (GitHub over SSH, GitLab over
  HTTPS, no remote): the copied GitHub link, a line moved by an unpushed edit linking to its old line, the notification
  for an edited line, an unpushed commit and an unknown server, and the greyed items without a remote.
- Built: `docs/dev/proof/share-result.png` and `docs/dev/proof/share-result-light.png`, VS Code's real menu (drawn
  with `window.menuStyle: custom`, so a screenshot shows it) over `retry_policy`, Copy GitHub Permalink chosen, under
  Dark Modern and Light Modern.
- Follow-up: on a server Spelunk can't place, Copy Permalink now asks which kind it is, saves the answer in
  `spelunk.permalinks.hosts` (user settings) and copies the link, instead of pointing at the setting.
  "on a server Spelunk can't place, the user says which kind it is, once, and the link is copied" in
  `resultActions.test.ts`; by hand in VS Code 1.141 on `git@git.acme.corp:payments/api.git`, GitLab picked, the
  GitLab-style link copied.

## Search from your code (2.1)

Mock 50 (`seed-from-editor`): the search key, pressed in the editor with text selected, searches that text; with nothing
selected it reopens the last query, as before. Its binding has an editor-only twin that passes `{ fromEditor: true }` to
`spelunk.open`, so a selection left in a file is ignored from the Explorer, the terminal, the panel or the Command
Palette. `seedQuery.ts` turns the selection into a query: a line is a quoted phrase, a single word too, so it matches
only that text and never file names by their letters (`"RetryPolicy"` doesn't find `retry_policy.py`), and
`content:"retry"`, `TransientError:`, `-v` and `OR` are text; an escaped `/regex/` when the last query's `.*` was
pressed, several lines one regex across them; `case:` and `word:` carry over; more than 10 lines or 1,000 characters
keep the last query, with a status bar note. The replaced query goes under Recent, and the panel types the new one in
(`state.restore`'s `seeded`), so ⌘Z brings the old one back. **Spelunk: Find Definitions** and **Find Uses**, on the
editor's right-click menu, search `sym:/^Name$/` and `ref:Name` for the name at the cursor.
`spelunk.search.seedFromSelection` turns the selection search off.

- Host: `extension/src/test/seedQuery.test.ts` (every line a phrase, escaped quotes and backslashes, operator-like text,
  `OR`/`NOT`, a leading slash or dash, regex escaping with `.*` on, toggles carried and filters not, lines across,
  blank and too-long selections, the find queries, and every one run against the real daemon: no diagnostics, and it
  finds the selection; `"RetryPolicy"` finds its 5 lines and no file names; `sym:/^RetryPolicy$/` finds the class
  alone), the controller's seeded restore (Recent keeps the
  replaced query; the same query, a Try query and a plain reopen aren't seeded), the manifest (each preset's editor
  twin, the right-click menu) and `spelunk.search.seedFromSelection`'s default.
- Panel: `webview/test/seedFromEditor.test.mjs` (a seeded query replaces the box's text selected, and ⌘Z brings the
  last query back; typing replaces it; a seeded query already in the box changes nothing).
- End to end in VS Code: "The search key in the editor searches its selection; with nothing selected the last query
  stays" and "Find Definitions and Find Uses search the name at the cursor" in `extension/src/e2e/commands.test.ts`.
- Built: `docs/dev/proof/seed-from-editor.png` (`RetryPolicy` selected in client.py, then ⌘P) and
  `docs/dev/proof/seed-from-editor-definitions.png` (Find Definitions on `RetryPolicy`), in VS Code 1.141 under Dark
  Modern.

## Search in Folder (2.1)

Mock 51 (`search-in-folder`): **Search in Folder with Spelunk**, on the Explorer's right-click menu for local folders
(group `4_search`, after Find in Folder…; hidden from the Command Palette), opens the panel scoped to the folders
right-clicked. `folderScope.ts` writes each one anchored at the root that holds it (the innermost, so a workspace folder
opened inside another searches itself): `f:^src/api/`, with `repo:^name$` in front when more than one root is open, a
root as `repo:^name$`, a file to its whole path, several ORed per repo and repos ORed in turn. RE2's metacharacters are
escaped, and a name with a space, a bracket or a quote takes the `/…/` form. A pick while the box still starts with the
last pick's scope joins it (a path already covered adds nothing; a folder drops the paths inside it); otherwise the
scope goes in front of what was typed. The panel types it in with the cursor after it (`state.restore`'s
`keepTyping`), so ⌘Z brings back the box as it was, which also stays under Recent. `folder/explain` says why a folder
the index holds no file of isn't searched (`ignored`, `excluded`, `link`, `empty`, from its first files on disk) and
which Git repo inside a root holds it; the host puts those as `folderNotes` on searches that start with the scope (the
Git repo's only in history), and the panel draws them with the setting to change.

- Daemon: `daemon/internal/server/folders_test.go` (an indexed folder, the root, a gitignored, an excluded and an empty
  folder, a symlink, a repo cloned inside, a trailing slash; paths outside the repo refused, a closed repo stale).
- Host: `extension/src/test/folderScope.test.ts` (anchoring, repo names, regex characters, spaces, brackets and
  quotes, several folders and repos, files, covered folders, nested roots, joining a second pick, and every scope run
  against the real daemon: no diagnostics, and exactly the folder's files, for `(shop)/orders/[id]`, `my components`,
  `a.b` beside `aXb`, `c++`, `star*` beside `starXY`, a file, two repos and a root), the controller's notes (only
  under searches that start with the scope, the Git repo's only in history) and `keepTyping` restore, the manifest's
  menu entry, and `settings.open` opening only Spelunk's settings.
- Panel: `webview/test/searchInFolder.test.mjs` (the scope typed in with the cursor after it, typing adds to it, ⌘Z
  brings the last query back; the skipped-folder notes and their setting; the Git repo note in history; No results
  pointing to them).
- End to end in VS Code: "Search in Folder scopes the box to the folders picked in the Explorer, and a second pick
  joins the first" in `extension/src/e2e/commands.test.ts`.
- Built: `docs/dev/proof/search-in-folder.png` (web-checkout's `src/api`, right-clicked with `retry` typed) and
  `docs/dev/proof/search-in-folder-notes.png` (a gitignored `dist/`), in VS Code 1.141 under Dark Modern.

## Go to Symbol and Go to Definition (2.1)

<!-- cspell:ignore retpol -->

Mock 55 (`editor-navigation`): VS Code's own navigation answers from the index. `symbols/find` scans only the names
the index keeps (`trigram.FindSymbols`), never file text: the letters typed in order, ignoring case, ranked exact, then
prefix, then substring, then the rest, generated and vendored files after the others, shorter names first, trimmed as
it goes to keep memory bounded, with each name's UTF-16 column. Through `sym:` on the Linux clone (96,080 files) ⌘T
took 3 s for `pci_enable_device` and timed out empty on `spin_lock`; a scan of its 942,916 definition names takes
18–25 ms (measured once on this Mac with `symbols.Extract` over the clone's C files), so ⌘T doesn't go through the
search. `exact` with `languageOf` serves Go to Definition, kept to the file's language when it has any. The host's
`editorNavigation.ts` registers a workspace symbol provider (on by default, `spelunk.editor.workspaceSymbols`) and
definition and reference providers, which `spelunk.editor.definitions` keeps `off` by default; `fallback` asks the
language servers first, with its own providers standing aside while it does, and answers only when they find nothing.
Find References runs `ref:"Name" case:yes` in the file's language, or every language when that finds none, and opens
each at the name with `open/resolve`. VS Code itself shows a definition both a language server and Spelunk list once.

- Daemon: `daemon/internal/server/definitions_test.go` (letters in order across three repos and languages, ranking
  and the limit, blank and unmatched text, generated and vendored marked and last, exact names kept to the caller's
  language or every language without one, VS Code's excludes, a deleted file, a saved new definition) and
  `daemon/internal/trigram/definitions_test.go` (each rank, Unicode folding, UTF-16 columns, a name its line no
  longer holds, the limit over 5,000 names, cancellation).
- Host: `extension/src/test/navigation.test.ts` against the real daemon (⌘T, the setting off, blank text, generated
  and vendored, Go to Definition in the file's language and exact, Find References in the language, with generated
  files, every language as a fallback, names `ref:` can't take, and when each mode answers),
  `extension/src/test/settings.test.ts` (defaults and wrong values) and the manifest's new section.
- End to end in VS Code: "Go to Symbol in Workspace lists Spelunk's definitions, and Go to Definition falls back to
  them when set to" in `extension/src/e2e/commands.test.ts`.
- Built: `docs/dev/proof/editor-navigation.png` (⌘T `retpol` over Python, TypeScript and Go),
  `docs/dev/proof/editor-navigation-definition.png` and `docs/dev/proof/editor-navigation-references.png` (Peek
  Definition and Peek References on `RetryPolicy` in a Go file with `fallback`), in VS Code 1.141 under Dark Modern.

## Hover labels (2.1)

Mock 61 (`hover-labels`): resting the mouse on any control with a name shows it in a label the panel draws
(`webview/src/hoverLabel.ts`), in the theme's `editorHoverWidget` colors, since VS Code's webviews show no native
title tooltips. It shows 300 ms after the mouse rests, at once when the mouse moves straight on to the next control,
and goes away on a click, a key or a scroll. It stays inside the panel, below the control or above it when there's no
room. While the mouse is on a control its `title` moves to `data-hover-title`, so no native tooltip doubles it. The
search box's magnifying glass and the Replace row's icon got names ("Search", "Replace with"), as the only icons
without one.

- Panel: `webview/test/hoverLabel.test.mjs` (the name shows within half a second but not at once, leaving hides it and
  gives the title back; moving to the next control shows it at once; the two new names; a click hides it and a title
  changed meanwhile survives; inside a narrow panel).
- Built: `docs/dev/proof/hover-labels.png` and `docs/dev/proof/hover-labels-light.png`, from
  `node scripts/screenshotPanel.mjs --query 'retry_policy' --theme "Dark Modern" --hover
  '[data-testid="toggle-replace"]'` (and `"Light Modern"`).

## Jupyter notebooks by cell (2.1)

Mock 60 (`notebook-cells`): a notebook is indexed as its cells' text (`daemon/internal/notebook`), each cell's source
in turn, never its JSON or outputs; results, symbols, previews and open targets carry each line's cell. The panel
heads a notebook's results with their cell and numbers lines within it, and the preview draws the cells with their
run numbers and a note of their outputs. VS Code opens a result in the notebook editor at its cell, Replace edits the
cells there (so a quote can't break the JSON), and `at:` opens a past notebook read-only in the notebook editor.
History diffs notebooks by their cells through a Git textconv filter, the daemon itself (ADR 0006).
`spelunk.index.notebookOutputs` also searches the text cells printed.

- Daemon: `daemon/internal/notebook/notebook_test.go` (cells' text and places, outputs after the source, kernel and
  per-cell languages, what isn't nbformat 4); `daemon/internal/trigram/notebooks_test.go` (search by cell, never the
  printed line, definitions and `ref:` by cell, `language:python`, the outputs setting, plots neither searched nor
  counted toward the size limit, a broken notebook searched as text, context and regexes kept inside a cell, the
  preview's cells and outline, open targets, Replace leaving outputs, at a revision);
  `daemon/internal/history/notebooks_test.go` (a re-run is no change, a one-line edit is +1 −1 with each diff line's
  cell, the filter never fails); `daemon/internal/server/notebooks_test.go` (at: by cell, revision/text the JSON).
- Host: `extension/src/test/notebookLines.test.ts` (lines to cells as the daemon counts them),
  `extension/src/test/syntaxColors.test.ts` (each cell in its own language, outputs plain), the new setting in
  `settings.test.ts`.
- Panel: `webview/test/notebooks.test.mjs` (cell headings, numbering within cells, context runs per cell, a
  definition's cell, the preview's cells, run numbers, output notes and outline, a commit diff by cell).
- End to end in VS Code: "A notebook result opens in the notebook editor at its cell, and Replace edits the cell,
  quotes and all" in `extension/src/e2e/commands.test.ts`.
- Built: `docs/dev/proof/notebook-cells.png` and `docs/dev/proof/notebook-cells-light.png`, from
  `node scripts/screenshotPanel.mjs --workspace "$PWD/testdata/notebooks" --query 'retry_policy' --select 3 --height 900
  --theme "Dark Modern"` (and `"Light Modern"`).

## VS Code's excludes (2.1)

Mock 52 (`editor-excludes`): searches leave out the files each workspace folder's `files.exclude` and
`search.exclude` hide, as VS Code's own search does. The extension sends every folder's enabled patterns in the
settings (`editorExcludes`; user, workspace and folder merged, `search.exclude` winning) and sends them again when
they change or a folder comes or goes, and the open panel searches again; nothing is reindexed, because the daemon
applies them when searching. `daemon/internal/excludes` reads VS Code's glob dialect (anchored at the folder,
`{a,b}`, `[abc]`, a folder hiding its contents, `when` clauses checked on disk), and the plan ends in a `Visible`
conjunct, so the engines count what it hid like any filter: the note names the patterns, and Show them adds
`excludes:no`, which shows those files last with an `excluded` badge and a note whose Hide them takes it out. History
hides commits that only change excluded files; Replace leaves them alone and counts their matches.
`spelunk.search.useExcludeSettings` turns it off.

- Daemon: `daemon/internal/excludes/excludes_test.go` (VS Code's anchoring, `**`, braces nested, classes, a folder's
  contents, `when` beside a sibling and not without one) and `daemon/internal/server/excludes_test.go` (hidden lines,
  totals, facets and the note with only the patterns that hid something; `excludes:no` badges and orders them last,
  counts them and undoes to the plain query; no settings changes nothing; file names; history hides a dist-only
  commit and drops an excluded file from a kept one; Replace's `excluded` count).
- Host: `extension/src/test/editorExcludes.test.ts` (each folder's patterns, `when` kept, `false` off, search.exclude
  turning off a files.exclude pattern, bad values ignored); the help example `excludes:no retry_policy` runs against
  the real daemon.
- Panel: `webview/test/discovery.test.mjs` (the note's text, the summary count, Show them and Hide them editing the
  box) and `webview/test/replace.test.mjs` (matches left alone in the Replace row).
- Built: `docs/dev/proof/editor-excludes.png`, `editor-excludes-shown.png`, `editor-excludes-history.png` and
  `editor-excludes-replace.png`, from `node scripts/screenshotPanel.mjs --workspace <three repos, each with a
  .vscode/settings.json> --query 'retry_policy' --height 900 --theme "Dark Modern"` (`excludes:no retry_policy`,
  `type:commit retry_policy`, and `--replace retry_rules`).

## Recently changed first (2.1)

Mock 54 (`recent-order`): the order name in each results heading is a menu of best match, path order and recently
changed (`webview/src/render/order.ts`), with an arrow beside it for the direction: newest or oldest first, A to Z or
Z to A, and greyed out for best match, which runs one way. A pick saves `spelunk.results.order`, which takes `best`,
`file`, `file:desc`, `recent` and `recent:asc` (and `file:asc`, `recent:desc`), or rewrites an `order:` typed in the
query, whose suggestions list each direction on its own. Recently changed sorts files by when they last changed (the
save time of a file with uncommitted edits or outside Git, otherwise its newest commit), with no extra Git work per
search: best match already looks the commit up. Ties go to best match, a file with no commit in the history window
goes last either way, and tests aren't moved down. Each code result then says when its file changed. At a past
revision the choice is greyed out, a typed `order:recent` is an error, and the setting's falls back to best match.

- Daemon: `daemon/internal/trigram/recent_test.go` (newest first with uncommitted edits by save time, oldest
  first with undated files still last, the setting, one timeline across a Git repo and a folder outside Git, what each
  result says about its change, ties by best match, pages, path from Z to A per repo with lines kept in order);
  `daemon/internal/query` (the order and direction from `order:` or the setting, the setting's recent at a revision,
  each direction suggested, `order:recent` with `at:` or in a commit search, a bad direction).
- Host: `controller.test.ts` (`order.set` saves only a change; bad payloads dropped), `settings.test.ts` (every
  value reaches the daemon and the panel).
- Panel: `webview/test/resultOrder.test.mjs` (the menu and arrow in each heading, what each pick sends, a typed order
  rewritten in the box, unavailable at a revision, ↑↓ and Esc, when each file changed).
- Built: `docs/dev/proof/recent-order.png`, `recent-order-menu.png` and `recent-order-light.png`, from
  `node scripts/screenshotPanel.mjs --query retry --setting results.order=recent --theme "Dark Modern"` (and
  `"Light Modern"`; the menu with `--click '[data-testid="order-button"]'` and the default order), over a copy of
  `testdata/workspace` whose payments-api has a few weeks of commits and an uncommitted edit, whose web-checkout was
  committed 2 hours ago, and whose shared-libs isn't a Git repository.

## Exact words, fuzzy file names on request (2.1)

Mock 33 (`fuzzy-file-names`) changed: a word matches its exact text, and `match:fuzzy` (the new `match:` operator,
`exact` or `fuzzy`) lets a bare word also find file names that have its letters in order. `spelunk.search.match` set
to `fuzzy` makes that the default, and `match:exact` turns it off. Without fuzzy matching, the plan's `MatchFilter`
counts what a fuzzy copy of the search would add (`Plan.MatchingFuzzy`), and the panel's note says "3 file names have
these letters in order" with Show them, which adds `match:fuzzy` or turns `match:exact` into it.

- Daemon: `daemon/internal/query/plan_test.go` (bare words fuzzy only under `match:fuzzy` or the setting, `match:exact`
  winning; the note's undo; no note for a phrase, a whole word or a NOT) and `daemon/internal/trigram/fuzzy_test.go`
  (the earlier fuzzy cases under `match:fuzzy`; exact by default with each note's count and undo, the setting, and
  `match:exact` over it).
- Host: the setting's default and fallback in `settings.test.ts`, and the help page's new example run against the real
  daemon.
- Panel: "exact matching's note counts the file names match:fuzzy would add, and Show them adds it" in
  `webview/test/webview.test.mjs`; the operator sheet and help list `match:`.
- Built: `docs/dev/proof/fuzzy-file-names.png` (`match:fuzzy cfg`) and `docs/dev/proof/fuzzy-file-names-note.png`
  (`cfg`, exact, with the note), from `node scripts/screenshotPanel.mjs`.

## Agents and the terminal (2.1)

Mock 56 (`agent-access`): Claude Code, Copilot and a shell search with Spelunk. The daemon binary is also the
`spelunk` command (`daemon/internal/cli`): `search` (ripgrep's lines, `--vimgrep` or JSON lines, exit 0/1/2),
`status`, `help query` (the guide `query.Guide` writes from the operator table) and `mcp`, an MCP server with four
read-only tools on the official MCP Go SDK ([ADR 0007](../adr/0007-agents-search-through-the-daemon.md)). It never
writes an index: it asks a window that shares the folder (`spelunk.agent.shareWindow`) over its owner-only socket in
`~/.spelunk/run`, where a guest gets only hello, status, parse, search and preview, or else a detached read-only
reader that loads the saved index, re-reads files changed since it was saved, and exits after ten idle minutes. The
per-repo lock the plan described is built: one daemon writes each repo, and another window on it follows its saves and
takes over when it closes. The extension adds the Agents and terminal settings, Install 'spelunk' Command in PATH,
Add to Claude Code with its `CLAUDE.md` note, Spelunk's MCP server for VS Code's agents (VS Code 1.101+), and
`~/.spelunk/cli.json` for the reader.

- Daemon: `daemon/internal/indexer/writelock_test.go` (a second indexer follows instead of writing and leaves the save
  alone; loads newer saves; re-reads editor saves into its own overlay; takes over when the writer closes; a dropped
  repo releases its lock; waits for a writer's first save; a read-only indexer writes nothing and leaves the lock to a
  window; the sweep re-reads edited, added and deleted files and leaves out a binary the build left out),
  `daemon/internal/server/share_test.go` (a guest searches the window's roots through its socket, in an owner-only
  folder, without the editor seeing its batches; every changing method is not found; sharing off removes the socket;
  the reader serves a saved index without writing and says when a folder has none; the agent time limit) and
  `daemon/internal/cli/cli_test.go` (the window answers live; the reader starts, answers from the saved index and
  re-reads an edit; exit codes; a folder with no index; `--json` and `--vimgrep`; status; help; options that leave
  query words alone; the MCP tools listed read-only, search, preview, a query error and syntax), and
  `daemon/internal/query/guide_test.go` (the guide's examples parse and plan; every operator is listed).
- Host: `extension/src/test/commandInstall.test.ts` (where the command installs on each system and whether that is on
  the PATH; install and the refresh after an update; Claude Code's own command; the `CLAUDE.md` note once; the
  settings file, owner-only and not rewritten unchanged), and the agent settings in `settings.test.ts`.
- Built: `docs/dev/proof/agent-access-terminal.png` (the command on the spelunk repo: code, definitions, commits, JSON,
  a mistake and a folder with no index) and `docs/dev/proof/agent-access-claude.png` (the same question in headless
  Claude Code with Sonnet: 63.3 s of `git log` without Spelunk, 8.7 s with it and the `CLAUDE.md` note, the same
  commits, and Spelunk right that neither commit removed a line). Without the note, in three earlier tries, Claude
  never called Spelunk.

## Uses inside definitions (2.1)

Mock 45 (`uses-in-definitions`): `kind:` or `symbol:` with `ref:`, `content:`, `struct:` or a regex across lines
matches inside the definitions it keeps. Before, `kind:function ref:kmalloc file:mm/slub.c` listed all 369 functions
of the file; now it lists each line that uses `kmalloc` inside a function, as a code result naming the innermost kept
definition that holds it (`ResultItem.within`, with its first and last line). The symbol extractor records where each
definition ends (`symbols.Symbol.End`): the last line indented under it, or a `}` or `end` lined up with it, with a `)`
or `{` there still part of its first line. The index format is 4, so saved indexes are rebuilt once. The plan sets
`InDefinitions` and asks for code lines; matches outside every kept definition are counted for `kind:`'s note, "2
matches outside a method", with Show every match. Left out, these terms leave out the definitions whose body has
them (`kind:function -ref:X`).

- Daemon: `daemon/internal/symbols/symbols_test.go` (where definitions end in Python, Go, C, Ruby and TypeScript,
  multi-line signatures and braces on their own line included), `daemon/internal/trigram/kind_test.go` (`ref:`,
  `content:` and `struct:` inside functions, methods and classes; the innermost kept definition named; a word still
  naming the definition; a left-out `ref:`; the note's count; each result carrying its definition's lines) and
  `daemon/internal/cli/cli_test.go` (`within` in `--json`, and `[in function build]` for agents).
- Panel: `webview/test/usesInDefinitions.test.mjs` (each row names its definition; the note and Show every match; the
  preview marks the definition's lines and names them).
- Built: `docs/dev/proof/uses-in-definitions.png` (`kind:method ref:RetryPolicy`) and
  `docs/dev/proof/uses-in-definitions-content.png` (`kind:method content:timeout`), from
  `node scripts/screenshotPanel.mjs`.

## Comments, quoted text or code (2.1)

Mock 57 (`comments-strings`): `in:comments`, `in:strings` and `in:code` keep the matches inside comments (Python
docstrings too), inside string literals, or in code outside both; `-in:` leaves one place out. `structural.Places`
reads a file with the lexer `struct:` uses and lists its comments and strings; the trigram engine judges each match
by where it is (`trigram/places.go`), never the file, so a line keeps only the hits `in:` wants, and Replace's edits
and the preview's marks go through the same check. A match that crosses places is in neither, so only `-in:` keeps
it; a regex across lines counts when the whole match is inside. Plain text and Markdown, and a notebook's markdown
cells, have nothing to tell apart, so every `in:` keeps their matches; a file in a language without comment rules is
code. `in:` is a global the plan reads itself, the one that may take a minus; it drops file names, drops definitions
when it leaves code out, warns when `struct:`, `symbol:`, `kind:` or `type:file` can find nothing under it, and
counts what it hid by searching again without it. Current files only.

- Daemon: `daemon/internal/structural/places_test.go` (each language's comments and strings, docstrings, prose and
  unknown languages, lexing unchanged), `daemon/internal/query/in_test.go` (the places each form keeps, file names and
  definitions, placement and its fixes keeping the minus, history and bad values, the warnings, value completions),
  `daemon/internal/trigram/places_test.go` (each match on its own, docstrings, prose and unknown languages, a match
  across places, a regex across lines, the note, `struct:`, Replace's edits, the preview's marks, a notebook cell by
  cell, comments inside definitions) and `daemon/internal/server/in_test.go` (the four forms end to end, the note
  and its undo).
- Host: the help example `in:comments retry` runs against the real daemon.
- Panel: `webview/test/commentsStrings.test.mjs` (the chip in plain words for each form, the note and Show them).
- Built: `docs/dev/proof/comments-strings.png`, `comments-strings-code.png` and `comments-strings-replace.png`, from
  `node scripts/screenshotPanel.mjs --workspace <three repos, web-checkout with src/api/retry.ts and docs/retry-notes.txt>
  --theme "Dark Modern" --height 800 --query 'retry in:comments'` (`retry in:code f:src/api/retry.ts --select 1`, and
  `retry in:comments f:src/api/retry.ts --replace retries`). The operator sheet and help page screenshots
  (`empty-box-recent-and-operators.png`, `help-page.png`) are retaken with the new entry.

## History through renames and merged branches (2.1)

Mock 59 (`history-renames`): history reads renames (`git log --find-renames`), so a rename is one change that adds
and removes only the lines it edited, and each changed file keeps its old path in the commit that renamed it and its
name now. Names are given newest first as commits are read (`history.names`): a rename hands the old path to the
older commits of the file now called by the new name, and a file created at a path ends that path's link to newer
names. An update that brings in a rename names the older commits again, copying only the segments whose names
change. `file:` matches a commit's file by any of the three names, so `-file:` leaves out the older names too. Copies
aren't followed (`--find-copies-harder` was 41 times slower on the Linux kernel's last 100 commits, and then gave up).
`at:` reads which files Git sees renamed between the revision and HEAD with the revision's files: such a file is
`renamed` since, with `renamedTo`, and `file:` finds it by its name now. `spelunk.index.mergedBranches` (off by
default) reads the commits each first-parent merge brought in, in landed order (`history/merges.go`: the branch's
first-parent line in the window, then `git rev-list --parents --timestamp` over what landed after the commit before
it, walked oldest first so each commit belongs to the first merge that brought it), through `git log
--no-walk=unsorted --stdin --remerge-diff`: each merge holds only what it changed itself, a resolved conflict's lines
marked `resolution` without its two sides. A merged-in commit carries its merge and when it was committed, which
`since:`, `until:`, the result order, the month facet, the history window and a file's last commit go by. Git older
than 2.36 keeps the first-parent line. The saved history format is 6.

| Linux kernel, last 2 years | Commits | Read in | First searchable | Memory after | Peak | Saved | Load |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Before (first parents, no renames) | 5,707 | 50.4 s | 3.1 s | 584 MB | 2.97 GB | 389 MB | 4.0 s |
| Renames (the default now) | 5,702 | 56.3 s | 3.7 s | 535 MB | 2.67 GB | 344 MB | 3.4 s |
| Renames and `index.mergedBranches` | 175,670 | 2 min 13 s | 0.7 s | 1,826 MB | 5.45 GB | 530 MB | 10.5 s |

On spelunk's own history (727 commits, one merge) each configuration reads in under 2 s, 20 MB in memory, 5 MB saved.
The "before" row was measured on the same clone with the code before this change; the merged-branch run reads the
first segment fast because the branch's newest merge and its commits come first.

- Daemon: `daemon/internal/history/renames_test.go` (a rename as one change with its old path; `file:` through two
  renames, an old name, `-file:`; a copy and a rewrite; a path taken over after a rename; an update naming older
  commits without touching the store it started from; the preview of a rename),
  `daemon/internal/history/merges_test.go` (landed order and each commit's merge; `author:`, a branch-only line,
  `since:` and `until:` by landing, a conflict's resolution, an octopus merge, against the first-parent default; the
  merge's preview; an update bringing in a new merge's branch; Git versions) and
  `daemon/internal/server/revisions_test.go` (`at:` renamed since, and `file:` by the name now).
- Host: `extension/src/test/settings.test.ts` (`index.mergedBranches` reaches the daemon, off by default).
- Panel: `webview/test/historyRenames.test.mjs` (a rename's row, a file's name now, the mode chip, a pure rename's
  preview, the merge, merged-in and conflict badges, a merge with no changes of its own) and
  `webview/test/pastRevision.test.mjs` (the renamed-since badge and its tooltip).
- Built: `docs/dev/proof/history-renames.png` (`file:retry_policy.py type:commit --select 1`),
  `history-renames-merged.png` (`author:bob type:commit` with `--setting index.mergedBranches=true`) and
  `history-renames-at.png` (`at:v0.1.0 next_delay`), from `node scripts/screenshotPanel.mjs --theme "Dark Modern"
  --height 800 --workspace <a copy of testdata/workspace whose payments-api history renames backoff.py to policy.py
  with an edit and then to retry_policy.py, tags v0.1.0 before the renames, copies a test file, rewrites
  exceptions.py as errors.py, merges Bob's two-commit branch two days ago over a conflicting edit, and merges three
  one-commit branches in an octopus merge; its files end as the fixture's>`.

## Ask in plain words (2.1)

Mock 62 (`ask-in-words`): a question ending in `?`, or the **Generate query** button (first in the toolbar), goes to a
provider that writes the Spelunk query, the daemon's parser checks it (one repair when it doesn't parse), and the panel
puts it in the box under a line that says what was asked and what it was searched as, with the provider's one-line
explanation and assumptions. The provider is Anthropic's Claude through the official SDK (`extension/src/ask.ts`: the
system prompt is the daemon's `query/guide` text, which the operator table writes, plus the Recipes examples and the
shared repo and team names; structured JSON output; the system prompt cached); `spelunk.ask.provider`,
`spelunk.ask.model`, `spelunk.ask.effort` (the request's `output_config.effort`; the models API's `capabilities.effort`
says which levels a model takes, asked once per model, so a level it lacks becomes the nearest it has and Claude 4.5 and
older get none) and `spelunk.ask.shareNames` set it up, **Spelunk: Choose Ask Model** picks both from `models.list`, and
**Spelunk: Set or Change Anthropic API Key** keeps the key in secret storage and turns Ask on
(`extension/src/askSetup.ts`). The panel's `ask` and `ask.setup` messages go through `AskRoute`
(`extension/src/askRoute.ts`), which also fetches the CODEOWNERS teams from the `owner:` suggestions; the webview's
`AskFlow` (`webview/src/askFlow.ts`) owns the question, the line (`render/asked.ts`) and Broaden. A question too vague
to search without guessing comes back as `clarify` (the answer schema's question back; `AskResultMessage.clarify`), so
no query is written and the box keeps the question. A test build takes a fake provider from `SPELUNK_ASK_FAKE`, which
the screenshots use, so no screenshot or test calls a real provider. To keep `panel.ts` within its size, the box's keys
moved to `webview/src/keys.ts` and the results view's handlers to `webview/src/resultHandlers.ts`.

- Daemon: `daemon/internal/server/parse_test.go` (`query/guide` names every operator).
- Host: `extension/src/test/ask.test.ts` (the question becomes the provider's query, checked by the parser; one repair
  for a query that doesn't parse, and the error shown after a second failure; Broaden's retry; no key and a failing
  provider; names kept home; the prompts' contents; answers read out of any wrapping; the fake provider),
  `extension/src/test/controller.test.ts` (ask and ask.setup routed, the team names from the owner: suggestions, off
  without Ask set up), `settings.test.ts` (the ask settings and their fallbacks) and `manifest.test.ts` (the section
  and the command).
- Panel: `webview/test/askInWords.test.mjs` (Enter on a question asks it and the answer's query runs under the line,
  Edit question, which reports the words to the host as typing would; a question back for a vague question, the box kept
  as typed and nothing searched; Generate query, first in the toolbar; a box with search words ending in `?` searched as
  typed; a query that doesn't parse shown with its error; Broaden; no key, Set API key and Search these words instead;
  Ask off).
- Built: `docs/dev/proof/ask-in-words.png` (`which methods use RetryPolicy?`),
  `docs/dev/proof/ask-in-words-broaden.png` (`functions that call charge_card?`, no results, Broaden) and
  `docs/dev/proof/ask-in-words-clarify.png` (`where are other files?`, **Spelunk asks** "Other than which files?"), from
  `node scripts/screenshotPanel.mjs --ask … --setting ask.provider=anthropic` with `SPELUNK_ASK_FAKE`.
