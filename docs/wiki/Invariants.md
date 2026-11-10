# Invariants

These rules always hold. Each keeps a whole class of bugs out, so a change that breaks one needs an ADR first.

| Rule | What it means | Bug it prevents |
| --- | --- | --- |
| **One parser** | Only `daemon/internal/query` parses query text. `search/start` re-parses rather than trusting an AST | Two sides disagreeing on what a query means |
| **The webview is a pure view** | No files, Git or daemon. Fix-its, completions and toggles are text edits + `query.changed` | Logic hiding in the UI that tests can't reach |
| **`ref` is opaque** | Hosts and webviews pass refs back unchanged | Clients depending on daemon internals |
| **One writer** | Only the indexer holding the repo's writer lock writes its index; engines read published snapshots. Explicit `spelunk index` uses that same lock; search and MCP remain read-only | Searches over half-written indexes, and windows overwriting each other's saves |
| **One write path** | Only Replace all changes the user's files: the host checks the current preview, source and relevant output digests, notebook structure, target ranges and document versions, then applies one `WorkspaceEdit`. The daemon never writes source files | Edits whose unchanged lines no longer qualify, or whose ranges moved |
| **Engines own results** | The RPC layer forwards `ResultItem`s unchanged | Results changed in transit |
| **Protocol changes start in the schema** | Additive changes keep the version; breaking ones bump it | Type drift between Go and TypeScript |

---

**Next:** [[Cross-Cutting Concerns]]: cancellation, offsets, limits and budgets.
