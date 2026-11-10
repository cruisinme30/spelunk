# Reuse lexical classification within immutable snapshots

**Issue.** Repeating a context-filtered query classified the same unchanged files again.

**Example.** `content:timeout in:code` needs to distinguish executable code from comments and strings. Even after
an earlier query had identified those regions, the next query repeated that work.

**Root cause.** Lexical regions lived only for a query. Caching complete search results would complicate changes to
queries, settings and overlays; retaining unlimited classifications would increase memory without a bound.

**Change.** Immutable index shards now own a lazy cache of lexical regions and normalized line offsets, keyed by
document identity. Each shard accounts for at most 8 MiB of retained cache data. Parsing happens outside a mutex;
publication is protected. Concurrent first readers may compute twice, but only one immutable entry is retained.

**Why it works.** Repeated queries reuse the expensive classification while still doing their own matching and
counting. New index generations, loaded shards and edited-file overlays receive fresh caches, so a changed file
cannot inherit stale regions. No extra source-text copy or complete query result is cached.

| Historical engine workload | Warm total p50 before | After | Retained heap change |
| --- | --- | --- | --- |
| Go source, 10,259 files | 280.203 ms | 65.193 ms | +7.16 MiB |
| Prometheus, 1,761 files | 106.449 ms | 24.273 ms | +2.62 MiB |

**Evidence.** These are the earlier audit measurements, not the new Linux campaign: 20 warm samples after an
initial query, four Go workers, Go 1.25.0, identical complete counts (2,092 and 794), at most 500 emitted rows.
Baseline `4337e12165cd1cc0ed13cdc240f979e1d9f22c88`; candidate `a1b8baf`. The [progress record](../progress.md)
contains p95, initial-query, corpus and memory details. Cache concurrency, exhaustion, offsets, overlays and reload
tests passed; the earlier batch passed CI.

**Trade-offs.** Warm latency improves at a memory cost. Each document gains a pointer. The 8 MiB budget covers
accounted retained cache entries per shard, not temporary allocations, all live shards or total process RSS.
First reads remain expensive: the Go initial sample increased from 279.706 to 321.254 ms. No cold-query or general
regex speedup is claimed.

**Status.** Earlier PR #4 is open and unreleased. See
[ADR 0009](../../adr/0009-cache-lexical-regions-per-snapshot.md) for the ownership and accounting design.
