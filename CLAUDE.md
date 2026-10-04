# Unified Search — working notes for Claude

Read `docs/implementation-plan.md` before changing anything; `docs/test-plan.md` before adding tests.

## Boundaries that must hold
- The daemon owns the only query parser. Webview, extension host and engines never re-parse raw query text.
- The webview is a pure view: no file, Git or daemon access.
- Types that cross a process boundary are generated from `protocol/` JSON Schemas — never hand-written on either side.
- Only the indexer writes to the index directory; engines read published snapshots only.
- `ResultItem`s are built only inside engines; the RPC layer forwards them unchanged. `ref` is opaque outside the daemon.
- Contracts 1–3 freeze at the end of M1; later changes bump the protocol version and update schemas first.

## Conventions
- Regex is RE2 everywhere. All `Range`/`Span` offsets are UTF-16.
- Every test tags the spec IDs it covers (`@covers op:since diag:unclosed_paren`); CI fails on uncovered IDs.
- universal-ctags runs as a separate process (GPL) — never linked into the daemon.
