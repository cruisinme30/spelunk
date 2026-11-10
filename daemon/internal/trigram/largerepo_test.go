package trigram

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/benchmark"
	"github.com/cruisinme30/spelunk/daemon/internal/engine"
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
	b.Logf("indexed %d files in %v; heap %d bytes", len(shard.Docs), time.Since(started).Round(time.Millisecond), mem.HeapAlloc)

	repos := []Repo{{ID: "r", Name: "r", Root: root, Shard: shard}}
	settings := protocol.Settings{DefaultCount: 500}
	for _, query := range []struct{ name, text string }{
		{"literal", "ErrUnexpectedEOF"},
		{"conjunction", "func NewReader"},
		{"regex", `/func \(\w+ \*Reader\) Read/`},
		{"case-filter", "case:yes Mutex lang:go"},
		{"exclude", "timeout -f:_test.go"},
		{"file", "type:file reader"},
		{"no-hit", "content:spelunk_benchmark_absent_928374650193"},
	} {
		b.Run(query.name, func(b *testing.B) {
			plan := mustPlan(b, query.text, settings, "")
			benchmark.Measure(b, func(emit func(protocol.ResultItem)) (engine.Stats, error) {
				return Search(ctx, plan, repos, 1, emit)
			}, query.name == "no-hit")
		})
	}
}
