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
| **Hard-coded search round-trips** | `daemon/internal/server/m0search_test.go` (RPC level) and `extension/src/test/controller.test.ts` (host controller → real daemon): `query.changed` → `parse.result` → `search.batch` → `search.done`, then `result.select` → `preview.result` |
| Opening a file at a line | Same controller test: `result.open` resolves to `{path, line 3, column 14, length 12}` and calls `openTarget(…, "side")`; the panel hides (closeOnOpen); F4 reopens the next result. The final `showTextDocument` call is VS Code glue in `extension.ts` (**not yet run**: needs VS Code) |
| Visual check | [`proof/m0-panel.png`](proof/m0-panel.png): real daemon results rendered by the real webview bundle |

The M0 search is a deliberate placeholder: the whole box is one case-insensitive literal (`daemon/internal/server/m0search.go`).
M1 replaces it with the parser, planner and tree engine.

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
