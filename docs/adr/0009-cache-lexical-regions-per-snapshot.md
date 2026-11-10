# 0009. Cache lexical regions within immutable index snapshots

- Status: Accepted
- Date: 2026-11-10

## Context

Repeating `timeout in:code` reclassified the same files on every query. Profiling identified lexical scanning as
a substantial part of context-filtered search. Reusing query results would complicate correctness when settings,
overlays or queries changed. Retaining unbounded lexical data would trade latency for uncontrolled memory growth.

## Decision

Cache only lexical regions and normalized line offsets within each immutable working-tree shard. Entries are keyed
by that shard's document identity. A mutex protects publication; parsing runs outside the lock. Concurrent first
reads may compute twice but publish one immutable value. No extra source-text copy or query result is retained.

The cache has an 8 MiB accounting budget per shard for retained slices and conservative metadata. Each document
also gains one pointer (4 or 8 bytes). Once the budget is exhausted, later reads compute without retaining data.
This budget does not bound temporary allocations, total daemon RSS, or the aggregate of multiple live shards.

Builds, overlays and loads own fresh caches. Runtime cache fields are not persisted. An edited document cannot
reuse an earlier snapshot's classification. Standalone text readers continue to compute their own regions.

## Consequences

Repeated context queries can avoid lexical work while preserving the same matches. First reads remain expensive,
and workloads larger than the cache may see partial benefits. There is no promised improvement for plain regex
searches. Validation covers concurrent readers, budget exhaustion, normalized offsets, changed overlays and disk
reloads. Benchmarks must distinguish first-read latency from warm latency and sample memory after warming.
