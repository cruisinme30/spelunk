package history

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

// BenchmarkLargeHistory reads the history of the Git repo named by
// UNIFIED_SEARCH_BENCH_REPO once, leaving out the changed lines of the files
// the default index.exclude leaves out, then times history searches to
// their first result and to completion. The budget is 250 ms to first
// results. For example:
//
//	git clone https://github.com/prometheus/prometheus /tmp/prom
//	UNIFIED_SEARCH_BENCH_REPO=/tmp/prom go test -bench LargeHistory -run '^$' ./internal/history
func BenchmarkLargeHistory(b *testing.B) {
	root := os.Getenv("UNIFIED_SEARCH_BENCH_REPO")
	if root == "" {
		b.Skip("set UNIFIED_SEARCH_BENCH_REPO to a Git repo with a long history")
	}
	ctx := context.Background()
	started := time.Now()
	var firstPublish time.Duration
	skip := trigram.NewExcluder([]string{"**/vendor/**", "**/node_modules/**", "**/*.min.js"}).Excludes
	store, err := Ingest(ctx, root, Options{Skip: skip}, nil, func(*Store) {
		if firstPublish == 0 {
			firstPublish = time.Since(started)
		}
	})
	if err != nil {
		b.Fatal(err)
	}
	var mem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&mem)
	b.Logf("read %d commits in %v (newest searchable after %v); heap %d MB",
		store.Commits(), time.Since(started).Round(time.Millisecond), firstPublish.Round(time.Millisecond), mem.HeapAlloc>>20)

	repos := []Repo{{ID: "r", Name: "r", Root: root, Store: store}}
	for _, text := range []string{
		"type:commit timeout",
		"type:commit /context\\.WithTimeout/",
		`msg:"fix flaky"`,
		"author:a f:_test.go$ retry",
		"type:commit (timeout OR deadline) -f:vendor/ since:1y",
		"type:commit case:yes Mutex",
	} {
		b.Run(text, func(b *testing.B) {
			p := plan(b, text)
			var first, total time.Duration
			for range b.N {
				start := time.Now()
				seen := false
				if _, err := Search(ctx, p, repos, 1, func(protocol.ResultItem) {
					if !seen {
						first += time.Since(start)
						seen = true
					}
				}); err != nil {
					b.Fatal(err)
				}
				total += time.Since(start)
			}
			b.ReportMetric(float64(first.Microseconds())/float64(b.N)/1000, "ms-to-first")
			b.ReportMetric(float64(total.Microseconds())/float64(b.N)/1000, "ms-total")
		})
	}
}
