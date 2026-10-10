package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// A deleted or renamed folder masks the indexed files inside it, and only
// those: not a sibling whose name starts the same.
func TestWithFilesUnderAddsEachFolderFileOnce(t *testing.T) {
	shard := trigram.NewShard([]trigram.Doc{
		{Path: "a-b/x.go"}, {Path: "a.go"}, {Path: "a/b/c.go"}, {Path: "a/d.go"}, {Path: "ab/e.go"}, {Path: "b/f.go"},
	})
	got := withFilesUnder([]string{"a", "a/b", "gone", "b/f.go"}, shard)
	want := []string{"a", "a/b", "a/b/c.go", "a/d.go", "b/f.go", "gone"}
	if !slices.Equal(got, want) {
		t.Fatalf("withFilesUnder = %q, want %q", got, want)
	}
}

// A checkout that changes thousands of folders of a large repo costs a
// lookup each: comparing every path with every file took seconds, while
// searches waited for the indexer.
func TestWithFilesUnderScalesWithTheFoldersFiles(t *testing.T) {
	const folders, filesEach = 20_000, 10
	docs := make([]trigram.Doc, 0, folders*filesEach)
	paths := make([]string, 0, folders)
	for f := range folders {
		folder := fmt.Sprintf("pkg/m%05d", f)
		paths = append(paths, folder)
		for i := range filesEach {
			docs = append(docs, trigram.Doc{Path: fmt.Sprintf("%s/f%d.go", folder, i)})
		}
	}
	shard := trigram.NewShard(docs)
	start := time.Now()
	got := withFilesUnder(paths, shard)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("withFilesUnder took %v for %d folders of %d files", took, folders, len(docs))
	}
	if len(got) != folders+len(docs) {
		t.Errorf("withFilesUnder = %d paths, want %d", len(got), folders+len(docs))
	}
}

// A folder renamed or copied in arrives as one change to the folder: its
// files are read, and the renamed folder's old files go. Before, a renamed
// folder's files vanished from searches until the next rebuild.
func TestAFolderCreatedOrRenamedIsReadWhole(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n", "old/b.txt": "beta\n", "old/deep/c.txt": "gamma\n"})
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)

	if err := os.Rename(filepath.Join(root, "old"), filepath.Join(root, "new")); err != nil {
		t.Fatal(err)
	}
	ix.FilesChanged([]protocol.FileChange{{Path: filepath.Join(root, "old"), Type: "deleted"}, {Path: filepath.Join(root, "new"), Type: "created"}})
	if docs, _ := overlayPaths(t, ix); !slices.Equal(docs, []string{"new/b.txt", "new/deep/c.txt"}) {
		t.Fatalf("after the rename the overlay holds %q, want new/b.txt and new/deep/c.txt", docs)
	}
	testutil.WriteFiles(t, root, map[string]string{"copied/d.txt": "delta\n", "copied/.git/HEAD": "ref: x\n"})
	ix.FilesChanged([]protocol.FileChange{{Path: filepath.Join(root, "copied"), Type: "created"}})
	if docs, _ := overlayPaths(t, ix); !slices.Contains(docs, "copied/d.txt") || slices.Contains(docs, "copied/.git/HEAD") {
		t.Fatalf("after copying a folder in the overlay holds %q, want copied/d.txt and nothing from .git", docs)
	}
	// Renamed again: the files the overlay read from it go too.
	if err := os.Rename(filepath.Join(root, "new"), filepath.Join(root, "renamed")); err != nil {
		t.Fatal(err)
	}
	ix.FilesChanged([]protocol.FileChange{{Path: filepath.Join(root, "new"), Type: "deleted"}, {Path: filepath.Join(root, "renamed"), Type: "created"}})
	if docs, _ := overlayPaths(t, ix); slices.Contains(docs, "new/b.txt") || !slices.Contains(docs, "renamed/deep/c.txt") {
		t.Fatalf("after a second rename the overlay holds %q, want renamed/ and not new/", docs)
	}
}
