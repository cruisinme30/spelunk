package indexer

import (
	"fmt"
	"slices"
	"testing"
	"time"

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
