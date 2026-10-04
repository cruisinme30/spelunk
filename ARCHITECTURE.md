# Architecture

This document is a map of the code for new contributors: what lives where, how the pieces talk to each other, and
which rules hold everywhere. For the full design, read [docs/dev/implementation-plan.md](docs/dev/implementation-plan.md).
Decisions and their reasons are in [docs/adr/](docs/adr/).

## Bird's-eye view

You type one query, for example `author:jane f:.*test\.py$ timeout`, and get file names, code lines, symbol
definitions or commits from every repo in the workspace, updated as you type.

Three processes split the work:

```text
 webview (panel UI)  ──postMessage──▶  extension host  ──JSON-RPC over stdio──▶  search daemon (Go)
   renders, debounces                  VS Code commands,                         parses, plans, searches,
   edits query text                    settings, editors                         indexes files and history
```

A keystroke goes from webview to host to daemon. The daemon parses the query, plans it, runs **one** engine, and
streams results back in batches along the same path. Only the daemon touches files, Git or indexes.

## Codemap

### `protocol/`

`protocol.schema.json` is the single source of truth for every message that crosses a process boundary: the parsed
query (Contract 1), webview ↔ host messages (Contract 2) and host ↔ daemon methods (Contract 3). `gen.mjs` writes
`extension/src/protocol.gen.ts`, `webview/src/protocol.gen.ts` and `daemon/internal/protocol/protocol_gen.go`.
Nothing else defines a cross-boundary type.

### `daemon/` (Go)

- `cmd/unified-search-daemon` is the binary the extension spawns, one per VS Code window.
- `internal/rpc` handles JSON-RPC framing, request concurrency and cancellation. It knows nothing about search.
- `internal/server` maps each RPC method to the parts below: search IDs, batching and request lifecycle. It never
  reads an index.
- `internal/query` is the only query parser. It also produces diagnostics, fix-its and completions, and has the
  planner that turns a parsed query into a `Plan` (Contract 4).
- `internal/lang` detects a file's language from its name and shebang, and resolves `lang:` values.
- Planned: `internal/trigram` (working-tree index and engine), `internal/history` (commit store and engine)
  and `internal/indexer` (the only writer to the index directory).

### `extension/` (TypeScript, VS Code extension host)

- `src/extension.ts` is the VS Code glue: commands, context keys, settings, the status bar, opening editors.
- `src/daemon.ts` spawns the daemon and supervises it (handshake, crash restarts, graceful stop).
- `src/jsonRpc.ts` is the client side of Contract 3.
- `src/controller.ts` is the host side of Contract 2: it turns panel messages into daemon calls. It has no `vscode`
  import, so it is tested against the real daemon in plain Node.
- `src/panel.ts` hosts the webview. `src/commitDocuments.ts` shows commits as read-only diffs.

### `webview/` (TypeScript, the search panel)

- `src/panel.ts` (`SearchPanel`) handles input, keyboard and host messages, and calls the renderers.
- `src/state.ts` holds `ViewState`; `src/host.ts` is the only code that talks to VS Code.
- `src/render/` contains functions that take state and callbacks and write DOM: the chrome, the empty state,
  results and the preview.
- `src/queryEdit.ts` holds the pure text edits behind fix-its and the Aa / .* toggles.

### `testdata/`, `scripts/`

Fixture repos (built by a script, byte for byte the same every time) and golden files. `scripts/test-all.sh` runs
every test layer.

## Invariants

These rules must always hold. Each one keeps a whole class of bugs out.

- **One parser.** Only `daemon/internal/query` parses query text. The webview, the host and the engines consume its
  output. `search/start` re-parses the text rather than trusting an AST sent by someone else.
- **The webview is a pure view.** It never reads files, calls Git or reaches the daemon. Fix-its, completions and
  toggles are text edits followed by an ordinary `query.changed`.
- **`ref` is opaque** outside the daemon. Hosts and webviews pass refs back unchanged and never build or parse them.
- **One writer.** Only the indexer writes to the index directory. Engines read published snapshots, so a search
  never sees a half-written index.
- **Engines build results; nobody else changes them.** The RPC layer forwards `ResultItem`s unchanged.
- **Protocol changes start in the schema.** After Contracts 1–3 freeze (end of M1), any change bumps the protocol
  version.

## Cross-cutting concerns

- **Cancellation.** Every keystroke cancels the previous search with `$/cancelRequest`. Daemon code checks
  `ctx.Done()` in loops, and the webview drops any response with an older `seq`.
- **Offsets.** Every `Span`, `Range` and `Hit` uses UTF-16 code units, because that is what JavaScript strings and
  VS Code count in.
- **Regexes** are RE2 (Go's `regexp`), which runs in linear time, so no query can hang the daemon.
- **Testing.** Each contract has its own tests, so a change breaks the cheapest layer first. See
  [docs/dev/test-plan.md](docs/dev/test-plan.md) and `scripts/test-all.sh`.
- **Observability.** `UNIFIED_SEARCH_TRACE=<file>` records the JSON-RPC transcript, and the extension logs to the
  "Unified Search" output channel.
