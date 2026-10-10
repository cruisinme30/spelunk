package trigram

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// Build reads files several at a time but indexes them in the order it
// was given, whichever read finishes first: the same files make the same
// shard, doc ids and posting lists alike. Files it can't index (binary
// here) leave no gap.
func TestBuildIndexesFilesInOrderWhileReadingSeveralAtOnce(t *testing.T) {
	files := map[string]string{}
	var list []File
	for i := range 300 {
		path := fmt.Sprintf("f%03d.go", i)
		content := fmt.Sprintf("package p\n\nfunc F%d() { return %q }\n", i, strings.Repeat("x", i*37%500))
		if i%7 == 0 {
			content = "\x00binary"
		}
		files[path] = content
		list = append(list, File{Path: path})
	}
	root := testutil.WriteTree(t, files)
	var first *Shard
	for range 5 {
		shard, err := Build(context.Background(), root, list, BuildOptions{Symbols: true})
		if err != nil {
			t.Fatal(err)
		}
		for i := range shard.Docs {
			if i > 0 && shard.Docs[i-1].Path >= shard.Docs[i].Path {
				t.Fatalf("doc %d %q comes after %q", i, shard.Docs[i].Path, shard.Docs[i-1].Path)
			}
		}
		if len(shard.Docs) != 300-43 {
			t.Fatalf("Build indexed %d files, want %d", len(shard.Docs), 300-43)
		}
		if first == nil {
			first = shard
		} else if !reflect.DeepEqual(shard.postings, first.postings) {
			t.Fatal("two builds of the same files made different posting lists")
		}
	}
}

// A cancelled build stops starting reads and returns the context's error,
// after the reads it started have finished.
func TestACancelledBuildReturnsItsError(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.go": "package a\n"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, root, []File{{Path: "a.go"}}, BuildOptions{}); err == nil {
		t.Fatal("Build after cancel succeeded, want the context's error")
	}
}
