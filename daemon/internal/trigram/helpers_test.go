package trigram

// Test helpers shared by this package's test files.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// fixedNow stamps test docs, so results don't depend on the clock.
var fixedNow = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)

// shardOf indexes in-memory files, as Build would index them from disk.
func shardOf(files map[string]string) *Shard {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	s := &Shard{BuiltAt: fixedNow, postings: map[uint32][]uint32{}}
	for _, path := range paths {
		content := []byte(files[path])
		s.add(Doc{Path: path, Lang: lang.Detect(path, content), ModTime: fixedNow, Content: content})
	}
	return s
}

// writeTree creates files under a new temp directory.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// defaultSettings are the extension's default search settings.
var defaultSettings = protocol.Settings{DefaultCount: 500, HistoryDepth: "2y"}

// repoOf is a repo named name whose shard holds files (path to content).
func repoOf(name string, files map[string]string) Repo {
	return Repo{ID: name, Name: name, Root: "/repos/" + name, Shard: shardOf(files)}
}

// mustPlan parses and plans text; cursor is the Load more cursor ("" for
// the first page).
func mustPlan(tb testing.TB, text string, settings protocol.Settings, cursor string) *query.Plan {
	tb.Helper()
	parsed := query.Parse(text, nil)
	plan, _, err := query.NewPlan(parsed, settings, fixedNow, cursor)
	if err != nil {
		tb.Fatalf("NewPlan(%q): %v (diagnostics %+v)", text, err, parsed.Diagnostics)
	}
	return plan
}

// run searches repos and returns the emitted results, summarised.
func run(t *testing.T, text string, repos ...Repo) ([]string, engine.Stats) {
	t.Helper()
	items, stats := runItems(t, text, defaultSettings, "", repos...)
	return summarize(items), stats
}

// runItems searches repos and returns the emitted results. Every result
// carries plan ID 7.
func runItems(t *testing.T, text string, settings protocol.Settings, cursor string, repos ...Repo) ([]protocol.ResultItem, engine.Stats) {
	t.Helper()
	var items []protocol.ResultItem
	stats, err := Search(context.Background(), mustPlan(t, text, settings, cursor), repos, 7, func(item protocol.ResultItem) {
		items = append(items, item)
	})
	if err != nil {
		t.Fatalf("Search(%q): %v", text, err)
	}
	return items, stats
}

// summarize renders results as "file path" or "path:line text".
func summarize(items []protocol.ResultItem) []string {
	out := []string{}
	for _, item := range items {
		if item.Kind == query.KindFile {
			out = append(out, "file "+item.Path)
		} else {
			out = append(out, item.Path+":"+strconv.Itoa(item.Line)+" "+item.Text)
		}
	}
	return out
}
