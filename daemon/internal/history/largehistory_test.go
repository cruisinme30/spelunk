package history

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/benchmark"
	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// BenchmarkLargeHistory reads the history of the Git repo named by
// SPELUNK_BENCH_REPO once, leaving out the changed lines of the files
// the default index.exclude leaves out, then times history searches to
// their first result and to completion. The budget is 250 ms to first
// results. For example:
//
//	git clone https://github.com/prometheus/prometheus /tmp/prom
//	SPELUNK_BENCH_REPO=/tmp/prom go test -bench LargeHistory -run '^$' ./internal/history
func BenchmarkLargeHistory(b *testing.B) {
	root := os.Getenv("SPELUNK_BENCH_REPO")
	if root == "" {
		b.Skip("set SPELUNK_BENCH_REPO to a Git repo with a long history")
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
	b.Logf("read %d commits in %v (newest searchable after %v); heap %d bytes",
		store.Commits(), time.Since(started).Round(time.Millisecond), firstPublish.Round(time.Millisecond), mem.HeapAlloc)

	repos := []Repo{{ID: "r", Name: "r", Root: root, Store: store}}
	for _, query := range []struct{ name, text string }{
		{"literal", "type:commit timeout"},
		{"regex", `type:commit /context\.WithTimeout/`},
		{"message", `msg:"fix flaky"`},
		{"author", "author:a f:_test.go$ retry"},
		{"boolean", "type:commit (timeout OR deadline) -f:vendor/ since:1y"},
		{"case", "type:commit case:yes Mutex"},
		{"no-hit", "type:commit spelunk_benchmark_absent_928374650193"},
	} {
		b.Run(query.name, func(b *testing.B) {
			p := plan(b, query.text)
			benchmark.Measure(b, func(emit func(protocol.ResultItem)) (engine.Stats, error) {
				return Search(ctx, p, repos, 1, emit)
			}, query.name == "no-hit")
		})
	}
}
