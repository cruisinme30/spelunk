package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

func writeFiles(t *testing.T, files map[string]string) string {
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

func testSettings(t *testing.T) protocol.Settings {
	t.Helper()
	return protocol.Settings{DefaultCount: 500, Exclude: []string{"**/vendor/**"}, MaxFileSizeKB: 1024, Location: t.TempDir()}
}

// recorder keeps every status the indexer reports.
type recorder struct {
	mu      sync.Mutex
	history [][]protocol.RepoStatus
}

func (r *recorder) notify(statuses []protocol.RepoStatus) {
	r.mu.Lock()
	r.history = append(r.history, statuses)
	r.mu.Unlock()
}

func (r *recorder) sawState(id string, state protocol.IndexState) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, statuses := range r.history {
		for _, s := range statuses {
			if s.RepoID == id && s.Tree == state {
				return true
			}
		}
	}
	return false
}

// waitFor polls until every repo reports state.
func waitFor(t *testing.T, ix *Indexer, state protocol.IndexState) []protocol.RepoStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		statuses := ix.Status()
		done := len(statuses) > 0
		for _, s := range statuses {
			done = done && s.Tree == state
		}
		if done {
			return statuses
		}
		if time.Now().After(deadline) {
			t.Fatalf("repos never all %s: %+v", state, statuses)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func docPaths(repo trigram.Repo) []string {
	var paths []string
	for _, d := range repo.Shard.Docs {
		paths = append(paths, d.Path)
	}
	return paths
}

func TestIndexesRootsAndReportsProgress(t *testing.T) {
	// @covers rpc:index/progress
	root := writeFiles(t, map[string]string{"main.go": "package main\n", "vendor/x/x.go": "package x\n"})
	rec := &recorder{}
	ix := New(testSettings(t), rec.notify)
	defer ix.Close(time.Second)

	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	statuses := waitFor(t, ix, protocol.IndexStateReady)
	if statuses[0].Name != "app" || statuses[0].History != protocol.IndexStateOff {
		t.Errorf("status = %+v, want app with history off", statuses[0])
	}
	if !rec.sawState("r1", protocol.IndexStateIndexing) {
		t.Errorf("never reported indexing before ready")
	}
	repos := ix.Repos()
	if len(repos) != 1 || len(repos[0].Shard.Docs) != 1 || repos[0].Shard.Docs[0].Path != "main.go" {
		t.Errorf("published repos = %+v, want app with main.go (vendor excluded)", repos)
	}
}

func TestSavedShardsServeTheNextSessionAtOnce(t *testing.T) {
	root := writeFiles(t, map[string]string{"a.txt": "alpha\n"})
	settings := testSettings(t)
	first := New(settings, nil)
	first.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, first, protocol.IndexStateReady)
	first.Close(time.Second)

	second := New(settings, nil)
	defer second.Close(time.Second)
	second.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	// Loaded synchronously by SetRoots, before any build runs.
	if repos := second.Repos(); len(repos) != 1 || len(repos[0].Shard.Docs) != 1 {
		t.Fatalf("Repos right after SetRoots = %+v, want the saved shard", repos)
	}
	if got := second.Status()[0].Tree; got != protocol.IndexStateReady {
		t.Errorf("status with a saved shard = %s, want ready while it refreshes", got)
	}
}

func TestRemovedRootsAreDropped(t *testing.T) {
	a := writeFiles(t, map[string]string{"a.txt": "a\n"})
	b := writeFiles(t, map[string]string{"b.txt": "b\n"})
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "a", Path: a, Name: "a"}, {ID: "b", Path: b, Name: "b"}})
	waitFor(t, ix, protocol.IndexStateReady)
	ix.SetRoots([]protocol.Root{{ID: "b", Path: b, Name: "b"}})
	if repos := ix.Repos(); len(repos) != 1 || repos[0].ID != "b" {
		t.Errorf("Repos after removing a = %+v, want only b", repos)
	}
}

func TestChangingWhatIsIndexedRebuilds(t *testing.T) {
	// @covers setting:index.exclude setting:index.maxFileSizeKB
	root := writeFiles(t, map[string]string{
		"keep.go": "package k\n", "gen/out.go": "package g\n", "data.json": strings.Repeat("x", 2048),
	})
	settings := testSettings(t)
	ix := New(settings, nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)

	settings.Exclude = append(settings.Exclude, "gen/**")
	settings.MaxFileSizeKB = 1
	ix.SetSettings(settings)
	waitFor(t, ix, protocol.IndexStateReady)
	if got := docPaths(ix.Repos()[0]); len(got) != 1 || got[0] != "keep.go" {
		t.Errorf("docs after excluding gen/** and files over 1 KB = %q, want [keep.go]", got)
	}
}

func TestRebuildPicksUpNewFiles(t *testing.T) {
	// @covers rpc:index/rebuild
	root := writeFiles(t, map[string]string{"a.txt": "a\n"})
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)

	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild("r1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(ix.Repos()[0].Shard.Docs) != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("rebuild never picked up b.txt: %q", docPaths(ix.Repos()[0]))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := ix.Rebuild("nope"); err == nil {
		t.Errorf("Rebuild of an unknown repo succeeded")
	}
}

func TestMissingFolderIsAnError(t *testing.T) {
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: filepath.Join(t.TempDir(), "gone"), Name: "gone"}})
	statuses := waitFor(t, ix, protocol.IndexStateError)
	if statuses[0].Message == "" {
		t.Errorf("error status has no message")
	}
	if len(ix.Repos()) != 0 {
		t.Errorf("a failed repo was published")
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	tests := map[string]string{
		"~":           home,
		"~/idx":       filepath.Join(home, "idx"),
		"~alice/idx":  "~alice/idx",
		"/abs/index":  "/abs/index",
		"relative/ix": "relative/ix",
	}
	for in, want := range tests {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}
