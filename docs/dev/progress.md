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
| 25 | `retry order:path` | [path-order.png](proof/path-order.png) |
| 26 | `retry is:test` | [test-files.png](proof/test-files.png) |
| 27 | `author:jane is:test retry` | [test-history.png](proof/test-history.png) |
| 28 | `word:yes retry` | [whole-words.png](proof/whole-words.png) |
| 29 | `case:smart Retry` | [smart-case.png](proof/smart-case.png) |
| 30 | `retry`; `type:commit timeout author:"Jane Doe" -repo:^shared-libs$` after `type:commit timeout` | [result-facets.png](proof/result-facets.png), [result-facets-commits.png](proof/result-facets-commits.png) |
| 31 | An empty box with a named and an unnamed pin, the second being named | [pinned-queries.png](proof/pinned-queries.png) |
| 32 | `RetryPolicy`, replaced with `BackoffPolicy` (`--replace`) | [replace-in-files.png](proof/replace-in-files.png) |
| 33 | `cfg` | [fuzzy-file-names.png](proof/fuzzy-file-names.png) |
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
