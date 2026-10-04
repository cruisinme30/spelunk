# Unified Search

One search box for VS Code (desktop and code-server) that answers a single query language over current files **and** Git history, across every repo in the workspace, as you type.

```
author:jane f:.*test\.py$ (timeout OR retry) -f:vendor/ since:6m
```

## Layout

| Path | Language | What lives here |
| --- | --- | --- |
| `protocol/` | JSON Schema | Contracts 2 and 3; generates the TypeScript and Go types. Single source of truth for anything crossing a process boundary |
| `extension/` | TypeScript | VS Code extension host: commands, keybindings, settings, spawning the daemon, opening editors |
| `webview/` | TypeScript | The search panel UI (pure view) |
| `daemon/` | Go | `rpc/`, `query/` (parser + planner), `tree/` (Zoekt engine), `history/` (SQLite FTS5 engine), `indexer/` |
| `testdata/` | — | Fixture repos (built by script) and golden files |

## Docs

- [Implementation plan](docs/implementation-plan.md) — scope, architecture, the five contracts, indexing, budgets, milestones, risks
- [Test plan](docs/test-plan.md) — test layers, E2E harness, scenario catalog (E01–E18, X01–X12), CI gates
- [Mocks](docs/mocks.md) — index of the 18 UI mocks

## Status

M0 · Skeleton — not started.
