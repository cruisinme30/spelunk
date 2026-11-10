# MCP result limits and honest summaries

**Issue.** An agent's requested output limit could be overridden by the query's `count:all`. Separately, every
incomplete search was described as reaching a time limit, even when the engine stopped at its result ceiling.

**Example.** `content:retryPolicy count:all` with MCP `limit: 1` must return one result while retaining the complete
count of three fixture matches. A search stopped at 50,000 results must say “at least 50000”, rather than presenting
that lower bound as an exact total or guessing why execution stopped.

**Root cause.** The query planner owned the output count, so a query-level option could supersede the caller's
request. The display formatter also treated the generic `truncated` flag as proof of a timeout.

**Change.** Add a separately validated RPC `resultLimit`, applied after planning as the smaller of the query and
caller limits. MCP defaults to 50 shown results and accepts at most 50,000. Shared CLI/MCP summaries prefix incomplete
counts with “at least” and say “search incomplete; there may be more results”. Complete searches with a smaller
output page retain their exact total. Empty incomplete searches recommend narrowing and retrying.

**Why it works.** The caller's ceiling survives query planning, while independent total and shown counts preserve
useful information. Wording reflects only the completeness evidence the engine actually reports.

**Validation.** `daemon/internal/cli/mcp_test.go` exercises the real daemon and MCP handler, including `count:all`,
smaller query counts, invalid limits and zero matches. `mcp_summary_test.go` checks both interfaces for result-cap,
other incomplete, complete-page and empty-incomplete cases. Focused owner checks passed; final integration is pending.

**Limits.** A smaller output page does not guarantee proportionally less scanning or counting. This change does not
identify the precise reason an engine reports an incomplete answer.

**Status and paths.** Implemented on the working branch; not yet a published release. Main code:
`daemon/internal/server/search.go`, `daemon/internal/cli/mcp_tools.go`, `daemon/internal/cli/format.go` and
`protocol/protocol.schema.json`.
