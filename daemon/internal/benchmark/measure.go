// Package benchmark measures complete search workloads for the release gate.
// It is used only by benchmarks, not by the shipped daemon.
package benchmark

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// Measure reports nearest-rank p95 latency and checks stable, complete results.
// A search with no matches responds when it completes, never at zero elapsed time.
// The empty case is a deliberate negative control; other queries may have no hits
// on an arbitrary benchmark corpus. All queries must finish without truncation.
func Measure(b *testing.B, run func(func(protocol.ResultItem)) (engine.Stats, error), empty bool) {
	b.Helper()
	responses := make([]time.Duration, 0, b.N)
	totals := make([]time.Duration, 0, b.N)
	count := -1
	for range b.N {
		start := time.Now()
		first := time.Duration(0)
		emitted := 0
		stats, err := run(func(protocol.ResultItem) {
			emitted++
			if emitted == 1 {
				first = time.Since(start)
			}
		})
		total := time.Since(start)
		if err != nil {
			b.Fatal(err)
		}
		if stats.Truncated || (count >= 0 && count != stats.Total) || (empty && stats.Total != 0) {
			b.Fatalf("search returned total=%d truncated=%t; previous total=%d, negative control=%t", stats.Total, stats.Truncated, count, empty)
		}
		if (stats.Total == 0) != (emitted == 0) {
			b.Fatalf("search counted %d results but emitted %d", stats.Total, emitted)
		}
		count = stats.Total
		if emitted == 0 {
			first = total
		}
		responses = append(responses, first)
		totals = append(totals, total)
	}
	b.ReportMetric(milliseconds(percentile95(responses)), "ms-response-p95")
	b.ReportMetric(milliseconds(percentile95(totals)), "ms-total-p95")
	b.ReportMetric(float64(count), "matches")
	b.ReportMetric(0, "truncated")
	b.ReportMetric(float64(b.N), "samples")
}

// percentile95 returns the nearest-rank 95th percentile of nonempty samples.
func percentile95(values []time.Duration) time.Duration {
	slices.Sort(values)
	return values[int(math.Ceil(float64(len(values))*.95))-1]
}

func milliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}
