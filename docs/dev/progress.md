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
| Go layout | Module `github.com/cruisinme30/unified-search/daemon`, packages under `internal/`, a `doc.go` per package, `.golangci.yml` | `go vet` and tests pass |
| TypeScript | camelCase whole-word files, tests in `src/test/`, `verbatimModuleSyntax`, webview `main.ts` split into state, host, layout and `render/` modules | 11 extension and 10 webview tests pass; tsc strict |
| Tooling | npm workspaces at the root, ESLint (typescript-eslint, type-aware) and Prettier configs, `.editorconfig`, `.gitattributes` | `prettier --check` in test-all; ESLint needs `npm install` (not available offline) |
| Docs | README, ARCHITECTURE, CONTRIBUTING, CHANGELOG, SECURITY, CODE_OF_CONDUCT, MIT LICENSE, `.github/` templates; docs in Diátaxis folders; ADRs 0001-0003 | `docs/README.md` |
| Review | An independent readability review found 10 bugs and about 30 readability issues; all fixed. Each bug has a regression test that fails on the old code | See below |

Bugs found by the review, each now covered by a test:

1. A reopened panel dropped every query: the host never reset its last `seq`.
2. "1 file hidden" rendered as "1 fil hidden".
3. Match columns were wrong (or panicked) where lowercasing changes byte length (U+212A KELVIN SIGN).
4. `UNIFIED_SEARCH_NOW` made search durations come out as years.
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
