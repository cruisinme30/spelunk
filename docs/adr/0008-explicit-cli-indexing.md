# 0008. Build indexes explicitly from the terminal under the same writer lock

- Status: Accepted
- Date: 2026-11-09
- Extends: [0007](0007-agents-search-through-the-daemon.md)

## Context

A fresh checkout could not be searched from the terminal until someone opened it in VS Code. For example,
`spelunk search timeout` on a server with no editor reported that the folder was not indexed. Search clients
must remain read-only, and a second uncoordinated writer could corrupt saved snapshots.

## Decision

Add `spelunk index [--in DIR] [--rebuild]` as an explicit local operation using the existing indexer, settings,
and per-repo writer lock. It refreshes files and incrementally updates history; `--rebuild` reads the configured
history again. It rejects a competing writer, fails if locking or saving fails, and does not return success until
queued work and durable saves finish. Cancellation drains workers before releasing the lock.

Search, status, shared guest connections, and MCP tools keep their read-only permissions. No writable RPC is added.
The command uses saved CLI settings when available and the standard defaults otherwise.

## Consequences

Terminal and agent users can prepare a checkout without an editor. The shared indexer remains the source of truth
for storage and locking. Index construction is deliberately explicit; a search never starts an unexpected expensive
build. Index freshness and truncation remain visible in query results.
