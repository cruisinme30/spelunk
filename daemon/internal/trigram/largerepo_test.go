package trigram

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// BenchmarkLargeRepo indexes the folder named by SPELUNK_BENCH_ROOT
// once, then times searches to their first result and to completion.
// For example, on Go's standard library:
//
//	SPELUNK_BENCH_ROOT=$(go env GOROOT)/src go test -bench LargeRepo -run '^$' ./internal/trigram
func BenchmarkLargeRepo(b *testing.B) {
	root := os.Getenv("SPELUNK_BENCH_ROOT")
	if root == "" {
		b.Skip("set SPELUNK_BENCH_ROOT to a large source tree")
	}
	ctx := context.Background()
	started := time.Now()
	files, err := ListFiles(ctx, root, WalkOptions{MaxFileBytes: 1 << 20})
	if err != nil {
		b.Fatal(err)
	}
	shard, err := Build(ctx, root, files, BuildOptions{Symbols: true})
	if err != nil {
		b.Fatal(err)
	}
	var mem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&mem)
	b.Logf("indexed %d files in %v; heap %d MB", len(shard.Docs), time.Since(started).Round(time.Millisecond), mem.HeapAlloc>>20)

	repos := []Repo{{ID: "r", Name: "r", Root: root, Shard: shard}}
	settings := protocol.Settings{DefaultCount: 500}
	for _, text := range []string{"ErrUnexpectedEOF", "func NewReader", `/func \(\w+ \*Reader\) Read/`, "case:yes Mutex lang:go", "timeout -f:_test.go", "type:file reader"} {
		b.Run(text, func(b *testing.B) {
			plan := mustPlan(b, text, settings, "")
			var first, total time.Duration
			for range b.N {
				start := time.Now()
				seen := false
				if _, err := Search(ctx, plan, repos, 1, func(protocol.ResultItem) {
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
