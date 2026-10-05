//go:build unix

package trigram

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

func TestFIFOInTheTreeNeverBlocksIndexing(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan []string)
	go func() {
		listed, _ := ListFiles(context.Background(), root, WalkOptions{})
		// As if the FIFO replaced a file after it was listed.
		shard, _ := Build(context.Background(), root, []File{{Path: "a.txt"}, {Path: "pipe"}}, BuildOptions{})
		selected := SelectFiles(context.Background(), root, []string{"pipe"}, WalkOptions{})
		_, previewErr := Preview(&Repo{Root: root}, Ref{Path: "pipe", Line: 1}, nil, 1)
		if previewErr == nil {
			t.Errorf("Preview of a FIFO = nil error, want ErrStale")
		}
		done <- []string{paths(listed)[0], shard.Docs[0].Path, string(rune('0' + len(shard.Docs) + len(selected)))}
	}()
	select {
	case got := <-done:
		if got[0] != "a.txt" || got[1] != "a.txt" || got[2] != "1" {
			t.Errorf("listed, built, counted = %q; want only a.txt, never the FIFO", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("indexing blocked reading a FIFO")
	}
}
