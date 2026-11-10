# Reject incompatible internal RPC peers

**Issue.** Replacement safety depends on complete-source validation. An older extension host could understand edited
lines while ignoring the new source digest, defeating that requirement despite a correct daemon plan.

**Example.** A version 1 host must not apply a version 2 replacement plan after checking only target lines.
An updated CLI must not connect to an older reader and assume the new contract is available.

**Root cause.** Adding fields does not ensure an older peer enforces their meaning. Compatibility must be established
before opening indexes, accepting configuration or issuing requests that depend on the contract.

**Change.** Advance Spelunk's internal RPC protocol to version 2 in the shared schema and regenerate peer types.
The daemon refuses incompatible initialization before applying roots or settings. The extension reports a protocol
mismatch without repeatedly restarting an incompatible binary. CLI discovery checks `share/hello` before opening a
reader. An incompatible inner workspace cannot silently redirect a search to a compatible outer workspace.

**Why it works.** Both sides must agree to the contract requiring the source digest. Failed negotiation
stops normal operation and gives update-and-restart guidance. A compatible window for the same root can still answer;
an unrelated old window does not block a valid selection.

**Validation.** `daemon/internal/server/lifecycle_test.go` verifies rejection before configuration changes.
`daemon/internal/cli/mcp_backend_test.go` tests old, current, future and absent versions, including nested roots and
reader opening. `extension/src/test/daemon.test.ts` checks mismatch state and no restart loop. Focused owner checks
passed; complete batch validation remains pending.

**Limits.** This is compatibility enforcement, not authentication or an atomic filesystem guarantee. The standard
MCP wire protocol and webview message version are unchanged. Hosts, daemons and background readers need matching
updates; replacing files on disk does not update an already running process.

**Status and paths.** Implemented on the working branch; unpublished. Main sources:
`protocol/protocol.schema.json`, `daemon/internal/server/server.go`, `daemon/internal/cli/connect.go` and
`extension/src/daemon.ts`.
