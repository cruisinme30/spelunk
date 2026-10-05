package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

func TestAFloodOfChangesKeepsOnlyWhatIsInTheRoot(t *testing.T) {
	files := map[string]string{}
	for i := range 200 {
		files[fmt.Sprintf("d%d/f%d.txt", i%10, i)] = fmt.Sprintf("file %d\n", i)
	}
	root := writeFiles(t, files)
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)

	if err := os.Remove(filepath.Join(root, "d0/f0.txt")); err != nil {
		t.Fatal(err)
	}
	events := 10_000
	if testing.Short() {
		events = 1_000
	}
	changes := []protocol.FileChange{
		{Path: filepath.Join(root, "d0/f0.txt"), Type: "deleted"},
		{Path: filepath.Join(root, "never-existed.txt"), Type: "deleted"},
		{Path: root}, // the root itself
		{Path: filepath.Join(root, "..", "sibling.txt")},
		{Path: filepath.Join(root, ".git", "index")},
		{Path: "relative/path.txt"},
		{Path: ""},
	}
	for i := range events {
		changes = append(changes, protocol.FileChange{Path: filepath.Join(root, fmt.Sprintf("d%d/f%d.txt", i%10, i%200)), Type: "changed"})
	}
	ix.FilesChanged(changes)

	docs, masked := overlayPaths(t, ix)
	if len(docs) != 199 || slices.Contains(docs, "d0/f0.txt") {
		t.Errorf("overlay holds %d files, want the 199 that still exist", len(docs))
	}
	for _, path := range masked {
		if path != "." && path != "never-existed.txt" && !slices.Contains(docs, path) && path != "d0/f0.txt" {
			t.Errorf("masked %q, which is not a changed path in the root", path)
		}
	}
}

func TestRootsChangingWhileIndexingLeaveNoWorkBehind(t *testing.T) {
	before := runtime.NumGoroutine()
	a := writeFiles(t, map[string]string{"a.txt": "alpha\n"})
	b := writeFiles(t, map[string]string{"b.txt": "beta\n"})
	gitInit(t, b)
	ix := New(testSettings(t), nil)
	for i := range 100 {
		switch i % 3 {
		case 0:
			ix.SetRoots([]protocol.Root{{ID: "r1", Path: a, Name: "a"}, {ID: "r2", Path: b, Name: "b"}})
		case 1:
			ix.SetRoots([]protocol.Root{{ID: "r2", Path: a, Name: "a"}}) // the same ID, another folder
		default:
			ix.SetRoots(nil)
		}
		ix.FilesChanged([]protocol.FileChange{{Path: filepath.Join(a, "a.txt")}, {Path: filepath.Join(b, "b.txt")}})
	}
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: a, Name: "a"}, {ID: "r2", Path: b, Name: "b"}})
	waitFor(t, ix, protocol.IndexStateReady)
	repos := ix.Repos()
	if len(repos) != 2 || docPaths(repos[0])[0] != "a.txt" || docPaths(repos[1])[0] != "b.txt" {
		t.Errorf("repos after the churn = %+v, want a.txt in r1 and b.txt in r2", repos)
	}
	ix.Close(5 * time.Second)
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("%d goroutines after Close, %d before New: the workers or a git command leaked", after, before)
	}
}
