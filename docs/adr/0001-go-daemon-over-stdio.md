# 0001. Run search in a Go daemon spoken to over JSON-RPC on stdio

- Status: Accepted
- Date: 2026-10-03

## Context

Search needs a trigram index, a commit store, file watching and RE2 regexes, all fast enough to answer on every
keystroke across up to five repos. VS Code extension hosts are single-threaded Node processes that are shared with
other extensions. The extension has to work the same way in desktop VS Code and in code-server.

## Options considered

1. **Everything in the extension host (TypeScript).** No second process. But indexing would block the host,
   JavaScript regexes can backtrack forever, and there's no mature trigram index for Node.
2. **A native daemon over stdio (Go).** Indexing and search run off the host. Go's `regexp` is RE2 (linear time),
   and the language has good libraries for indexing.
3. **A daemon over a local TCP port.** Same as 2, but it opens a port that other local processes could reach, and
   it complicates code-server and containers.

## Decision

Option 2. The extension host spawns one daemon per window and speaks JSON-RPC 2.0 with LSP-style Content-Length
framing on stdin/stdout.

## Consequences

- We ship a daemon binary per platform (platform-specific VSIXs).
- A crash in the daemon can't take the editor down; the host restarts it up to 3 times a minute.
- The protocol is a real boundary, so it needs versioning and contract tests ([0002](0002-json-schema-as-protocol-source.md)).
- Parts of the daemon, such as a future history engine, could move to another language without changing the
  extension, because only the JSON-RPC protocol is shared.
