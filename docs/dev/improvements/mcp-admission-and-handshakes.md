# Bounded agent calls and stalled handshakes

**Issue.** Parallel MCP calls could create unrestricted backend work. A socket that accepted a connection but never
answered its initial handshake could retain that work indefinitely.

**Example.** Twelve simultaneous searches attempted twelve connections. A peer could stop reading the handshake
request or never respond; cancellation alone did not reliably release a blocked transport write.

**Root cause.** Each tool connected independently without an admission ceiling. Socket dialing had a timeout, but
the subsequent `share/hello` exchange did not bound the entire request, including writing and cancellation.

**Change.** Each MCP server admits at most eight concurrent search, preview or status calls. Excess calls receive an
immediate retry message rather than joining a queue. Syntax help does not need a backend slot. A shared handshake
helper imposes a two-second deadline and closes the unusable transport when that deadline expires. Window discovery
and saved-reader opening both use the helper. Slots and connections are released when admitted work finishes.

**Why it works.** Admission happens before connecting, so a burst cannot start unlimited connections in one process.
Closing the transport unblocks a peer that ignores cancellation or never reads, allowing normal cleanup to run.
The next request can use the released slot.

**Validation.** `daemon/internal/cli/mcp_admission_test.go` checks burst rejection and slot reuse after success and
cancellation. `mcp_handshake_test.go` covers both discovery paths against peers that never read or never respond.
`mcp_test.go` checks cancellation propagation to the backend. Focused owner tests passed; final integration is pending.

**Limits.** The ceiling applies per MCP process, not across every client on a machine. Two seconds bounds each
handshake; discovering multiple windows or starting a background reader can take longer overall. It is not a total
search deadline or a global memory quota.

**Status and paths.** Implemented on the working branch; unpublished. Main code:
`daemon/internal/cli/mcp.go`, `daemon/internal/cli/mcp_tools.go` and `daemon/internal/cli/connect.go`.
