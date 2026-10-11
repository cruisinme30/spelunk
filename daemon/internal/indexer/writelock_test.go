package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// followSettings check for new commits, and so for newer saves, every second.
func followSettings(t *testing.T) protocol.Settings {
	t.Helper()
	settings := testSettings(t)
	settings.CommitCheckSeconds = 1
	return settings
}

// hasPath reports whether ix's only repo has path in its shard or overlay.
func hasPath(ix *Indexer, path string) bool {
	repos := ix.Repos()
	if len(repos) != 1 {
		return false
	}
	if slices.Contains(docPaths(repos[0]), path) && !repos[0].Masked[path] {
		return true
	}
	if overlay := repos[0].Overlay; overlay != nil {
		for _, doc := range overlay.Docs {
			if doc.Path == path {
				return true
			}
		}
	}
	return false
}

func TestASecondWindowOnTheSameRepoFollowsTheFirstInsteadOfWriting(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := followSettings(t)
	roots := []protocol.Root{{ID: "r1", Path: root, Name: "app"}}
	writer := New(settings, nil)
	defer writer.Close(time.Second)
	writer.SetRoots(roots)
	waitFor(t, writer, protocol.IndexStateReady)
	saved := modTime(shardPath(settings.Location, roots[0]))

	follower := New(settings, nil)
	defer follower.Close(time.Second)
	follower.SetRoots(roots)
	waitFor(t, follower, protocol.IndexStateReady)
	if !hasPath(follower, "a.txt") {
		t.Fatalf("follower doesn't serve the saved index: %+v", follower.Repos())
	}
	if err := follower.Rebuild("r1"); !errors.Is(err, ErrNotWriter) {
		t.Errorf("follower Rebuild = %v, want ErrNotWriter", err)
	}
	if err := follower.Rebuild(""); err != nil {
		t.Errorf("follower Rebuild of every repo = %v, want nil (it skips the ones it follows)", err)
	}
	time.Sleep(1500 * time.Millisecond) // a follow tick, and any build it would wrongly start
	if got := modTime(shardPath(settings.Location, roots[0])); !got.Equal(saved) {
		t.Errorf("saved shard rewritten at %v by the follower, want it left at %v", got, saved)
	}
	if got := follower.Status()[0]; got.Tree != protocol.IndexStateReady || strings.Contains(got.Message, "Another") {
		t.Errorf("follower status = %+v, want ready, with no word of the other window", got)
	}
}

func TestAFollowerLoadsTheWritersNewerSaves(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := followSettings(t)
	roots := []protocol.Root{{ID: "r1", Path: root, Name: "app"}}
	writer := New(settings, nil)
	defer writer.Close(time.Second)
	writer.SetRoots(roots)
	waitFor(t, writer, protocol.IndexStateReady)
	follower := New(settings, nil)
	defer follower.Close(time.Second)
	follower.SetRoots(roots)

	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("beta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Saves have one-second timestamps on some file systems.
	time.Sleep(1100 * time.Millisecond)
	if err := writer.Rebuild("r1"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the follower serves the writer's rebuilt index", func() bool { return hasPath(follower, "b.txt") })
}

func TestAFollowerSeesFilesTheEditorSavesBeforeTheWriterRebuilds(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := followSettings(t)
	roots := []protocol.Root{{ID: "r1", Path: root, Name: "app"}}
	writer := New(settings, nil)
	defer writer.Close(time.Second)
	writer.SetRoots(roots)
	waitFor(t, writer, protocol.IndexStateReady)
	follower := New(settings, nil)
	defer follower.Close(time.Second)
	follower.SetRoots(roots)

	path := filepath.Join(root, "c.txt")
	if err := os.WriteFile(path, []byte("gamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	follower.FilesChanged([]protocol.FileChange{{Path: path, Type: "created"}})
	eventually(t, "the follower's overlay has the saved file", func() bool { return hasPath(follower, "c.txt") })
}

func TestAFollowerTakesOverWhenTheWriterCloses(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := followSettings(t)
	roots := []protocol.Root{{ID: "r1", Path: root, Name: "app"}}
	writer := New(settings, nil)
	writer.SetRoots(roots)
	waitFor(t, writer, protocol.IndexStateReady)
	follower := New(settings, nil)
	defer follower.Close(time.Second)
	follower.SetRoots(roots)

	writer.Close(time.Second)
	eventually(t, "the follower becomes the writer", func() bool { return follower.Rebuild("r1") == nil })
	if err := os.WriteFile(filepath.Join(root, "d.txt"), []byte("delta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := follower.Rebuild("r1"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the new writer's rebuild has the new file", func() bool { return hasPath(follower, "d.txt") })
}

func TestDroppingARepoReleasesItsLock(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := followSettings(t)
	roots := []protocol.Root{{ID: "r1", Path: root, Name: "app"}}
	writer := New(settings, nil)
	defer writer.Close(time.Second)
	writer.SetRoots(roots)
	waitFor(t, writer, protocol.IndexStateReady)
	writer.SetRoots(nil)

	second := New(settings, nil)
	defer second.Close(time.Second)
	second.SetRoots(roots)
	if err := second.Rebuild("r1"); err != nil {
		t.Errorf("Rebuild after the first indexer closed the folder = %v, want this one to write it", err)
	}
}

func TestAFollowerWithNoSavedIndexWaitsForTheWriter(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := followSettings(t)
	roots := []protocol.Root{{ID: "r1", Path: root, Name: "app"}}
	release := make(chan struct{})
	writer := New(settings, nil)
	defer writer.Close(time.Second)
	writer.mu.Lock()
	writer.beforeBuild = func(string) { <-release }
	writer.mu.Unlock()
	writer.SetRoots(roots)

	follower := New(settings, nil)
	defer follower.Close(time.Second)
	follower.SetRoots(roots)
	status := follower.Status()[0]
	if status.Tree != protocol.IndexStateQueued || !strings.Contains(status.Message, "Another VS Code window is indexing") {
		t.Errorf("follower status before the first save = %+v, want queued, waiting on the other window", status)
	}
	close(release)
	eventually(t, "the follower loads the first save", func() bool { return hasPath(follower, "a.txt") })
}

func TestAReadOnlyIndexerNeverWrites(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := followSettings(t)
	reader := NewReadOnly(settings, nil)
	defer reader.Close(time.Second)
	reader.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	time.Sleep(1500 * time.Millisecond) // a follow tick
	status := reader.Status()[0]
	if status.Tree != protocol.IndexStateQueued || !strings.Contains(status.Message, "Not indexed yet") {
		t.Errorf("read-only status of a never-indexed folder = %+v, want queued, not indexed yet", status)
	}
	if entries, _ := os.ReadDir(settings.Location); len(entries) != 0 {
		t.Errorf("read-only indexer wrote %v under the index location, want nothing", entries)
	}
	if err := reader.Rebuild("r1"); !errors.Is(err, ErrNotWriter) {
		t.Errorf("read-only Rebuild = %v, want ErrNotWriter", err)
	}
}

func TestAReadOnlyIndexerLeavesTheLockToAWindow(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := followSettings(t)
	roots := []protocol.Root{{ID: "r1", Path: root, Name: "app"}}
	reader := NewReadOnly(settings, nil)
	defer reader.Close(time.Second)
	reader.SetRoots(roots)

	window := New(settings, nil)
	defer window.Close(time.Second)
	window.SetRoots(roots)
	waitFor(t, window, protocol.IndexStateReady)
	eventually(t, "the reader loads the window's first save", func() bool { return hasPath(reader, "a.txt") })
}

func TestAReaderSweepsInFilesChangedSinceTheSave(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"keep.txt": "same\n", "edit.txt": "old\n", "gone.txt": "bye\n"})
	settings := followSettings(t)
	roots := []protocol.Root{{ID: "r1", Path: root, Name: "app"}}
	window := New(settings, nil)
	window.SetRoots(roots)
	waitFor(t, window, protocol.IndexStateReady)
	window.Close(time.Second)

	reader := NewReadOnly(settings, nil)
	defer reader.Close(time.Second)
	reader.SetRoots(roots)
	later := time.Now().Add(2 * time.Second) // a different modification time, whatever the clock's resolution
	if err := os.WriteFile(filepath.Join(root, "edit.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "edit.txt"), later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "added.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	reader.Sweep(context.Background(), root)

	if !hasPath(reader, "added.txt") || hasPath(reader, "gone.txt") || !hasPath(reader, "keep.txt") {
		t.Errorf("after a sweep: added %v, gone %v, keep %v; want true, false, true",
			hasPath(reader, "added.txt"), hasPath(reader, "gone.txt"), hasPath(reader, "keep.txt"))
	}
	if got := reader.Status()[0].Reread; got != 3 {
		t.Errorf("re-read count = %d, want 3 (edited, added, deleted)", got)
	}
	repo := reader.Repos()[0]
	for _, doc := range repo.Overlay.Docs {
		if doc.Path == "edit.txt" && string(doc.Content) != "new\n" {
			t.Errorf("edit.txt in the overlay = %q, want the new text", doc.Content)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(settings.Location, "tree")); len(entries) != 1 {
		t.Errorf("tree folder holds %d files after the sweep, want the one saved shard", len(entries))
	}
}
