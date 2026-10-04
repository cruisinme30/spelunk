# 0003. Build the working-tree index in-house instead of embedding Zoekt

- Status: Accepted (revisit when the build can fetch modules)
- Date: 2026-10-04

## Context

The plan proposed Zoekt shards for the working-tree index. The project is being built where neither the Go module
proxy nor GitHub is reachable, so third-party Go modules can't be added. The rest of the daemon reaches the
index through one function, `trigram.Search`, which runs a `query.Plan`, so what's behind it can change without
touching anything else.

## Options considered

1. **Wait for network access to add Zoekt.** This blocks working-tree search and everything built on it.
2. **A trigram index written for this project** (Go standard library only). Index lowercase trigrams per file, narrow
   candidates by intersecting posting lists, then verify with RE2. Store shards in a simple binary format.
3. **Shell out to ripgrep on every query.** No index, but it can't meet the 100 ms budget on 2 million lines, and
   ripgrep is the test oracle, so it can't also be the implementation.

## Decision

Option 2, in `daemon/internal/trigram`. It keeps Zoekt's core idea (trigram candidates, regex verification, immutable
shards) at about a tenth of the code, because we only need what the plan's operators use.

## Consequences

- The differential tests against ripgrep become the main guard on correctness.
- Performance budgets are measured on our own index. If it misses them, swapping Zoekt in behind `trigram.Search` is
  the fallback.
- No Apache-2.0 dependency to track for now.
