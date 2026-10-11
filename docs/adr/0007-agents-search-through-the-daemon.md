# 0007. Let agents and the terminal search through the daemon, over MCP with the official SDK

- Status: Accepted
- Date: 2026-10-11

## Context

Claude Code, Copilot and a shell could not use Spelunk: only the panel could ask the daemon. Agents grep instead,
which on a large repo is slow and can't search history the way `type:added` does. Indexes are saved to disk, but a
command that loads them on every run takes 31–46 s and 8 GB for Linux, and a second writer would break the single
writer the index depends on. The implementation plan also promised a per-repo lock that the code never had.

## Options considered

1. **A one-shot command that loads the saved index.** Simple; unusable at Linux's size.
2. **A separate search tool with its own index** (for example a Zoekt-based code-index MCP server). A second index of
   every repo, without history, symbols or Spelunk's query language.
3. **Ask the daemon that already has the index.** A window's daemon answers over an owner-only socket; when no window
   has the folder, a detached read-only reader loads the saved index once and stays up until idle. MCP rides on the
   same client, written with the official Go SDK, or by hand.

## Decision

Option 3, in `daemon/internal/cli`, with the per-repo lock built so a reader can never write. MCP uses the official
`github.com/modelcontextprotocol/go-sdk` rather than a hand-written protocol: the user asked to reuse existing work,
and the specification keeps moving (four versions in two years), which the SDK tracks. It is the daemon's first module
dependency and needs Go 1.25, which `daemon/go.mod` now names.

## Consequences

Agents and the terminal get the panel's query language, history and symbols, live when a window shares. Agents still
reach for grep unless told, so Add to Claude Code offers a note for `CLAUDE.md`. The build now downloads modules
(CI and `npm run presubmit` already can; a machine without network needs a module cache). Revisit the SDK if its
transitive dependencies grow, or if a stdio-only server becomes simpler to keep by hand.
