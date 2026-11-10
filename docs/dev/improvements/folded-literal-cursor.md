# Case-insensitive search: remember where each scan stopped

<!-- cspell:words benchtime -->

Measured 2026-11-10. **Status:** implemented locally; focused checks pass; integration and repository-wide
benchmarks are pending. This is an isolated line-matching improvement, not an 8× product-wide claim.

## Issue and example

Searching `content:needle` ignores case by default. In a roughly 1 MiB file containing about 23,800 repetitions
of `needle and some unrelated text on each line`, the old matcher took **370.17 ms**. Every line matched, but
the repeated search for an uppercase `N` found nothing.

## Root cause

For each matching line, the old code restarted searches for both `n` and `N` in the remaining file. When one
case was absent, it scanned to the end again. With B bytes and M matching lines, that repeated work could
approach O(B × M). It was unnecessary byte scanning, not expensive case conversion.

## Change and why it works

`foldedPositions` keeps two offsets: the next lowercase and uppercase candidate. A per-file matcher reuses
them as its position advances. An absent case gets an end-of-file sentinel; it is not searched again.
Each case's candidate scan now moves forward through the file once. Literal candidate verification and
the final regex check remain in place, so the optimization does not skip result counting or weaken matching.

The state belongs to one file scan, never the shared query cache. Unicode folds such as Kelvin sign and long
s retain their existing regex fallback. No lowercased file copy or persistent result cache was added.

## Measurements

| Approximately 1 MiB input | Before median | After median | Before / after |
| --- | --- | --- | --- |
| All lowercase | 370.17 ms | 45.86 ms | 8.07× |
| All uppercase | 366.35 ms | 41.22 ms | 8.89× |
| Alternating lowercase / uppercase | 40.64 ms | 42.12 ms | 0.965×; no demonstrated gain |

Five benchmark samples per variant; each reports Go's average ns/op over its adaptive iterations. The table
takes the median of those five values. Baseline ran before the optimized variant; this is not a confidence
interval or a randomized trial. The timed path includes line matching, result allocation and ordering checks.

Go 1.25.0, Linux amd64, Intel Xeon Platinum 8573C; `GOMAXPROCS=4`, `GOMEMLIMIT=1536MiB`,
`-test.benchtime=200ms -test.count=5`. Baseline: `a1b8baf145748562b6010c6cdc573f68b812da5e` with the
identical benchmark harness added. Optimized variant changes only `literal.go` and `match.go` in the search path.

## Validation and trade-offs

- Differential tests compare 200 generated ASCII texts × six needles at increasing offsets with an independent
  lowercased search; a separate test checks shared query facts across different files.
- The optimization-only trigram suite, focused race tests and Go lint passed. Existing Unicode fallback tests remain.
- Two transient integer offsets and a small closure allocation replace repeated work; this is not a retained-memory
  reduction. The mixed-case control shows why the improvement should not be generalized.
- Whole-query time also includes candidate selection, regex work, contexts, symbols and result handling. For example,
  making only 20% of a workload 8× faster would yield about 1.21× overall; this is illustrative, not a measured result.

## Evidence and reproduction

Implementation: `daemon/internal/trigram/literal.go` and `match.go`. Tests and benchmark:
`daemon/internal/trigram/literal_test.go`, `BenchmarkFoldedLiteralLineScan`.

```sh
GOMAXPROCS=4 GOMEMLIMIT=1536MiB go test ./internal/trigram \
  -run '^$' -bench 'BenchmarkFoldedLiteralLineScan/(lower|upper|mixed)/1048576$' \
  -benchtime=200ms -count=5
```

Run from `daemon/` with the same benchmark body on both revisions. Raw samples accompany the standalone artifact;
the separate Linux profile will report integrated latency, result completeness and process memory.
