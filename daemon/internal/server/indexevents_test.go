package server

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// workspace/setRoots and workspace/didChangeFiles are applied off the
// read loop: while the indexer is still busy with them, requests are
// answered. Applied on the read loop, twenty saves in a large repo kept a
// search waiting five seconds, and a cancel with it.
func TestRequestsAreAnsweredWhileTheIndexerAppliesNotifications(t *testing.T) {
	// @covers rpc:workspace/setRoots rpc:workspace/didChangeFiles
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "r"})
	busy, release := make(chan []indexEvent, 1), make(chan struct{})
	client.server.events.beforeApply = func(batch []indexEvent) {
		busy <- batch
		<-release
	}
	other := testutil.WriteTree(t, map[string]string{"b.txt": "beta\n"})
	roots := []protocol.Root{{ID: "r1", Path: root, Name: "r"}, {ID: "r2", Path: other, Name: "other"}}
	_ = client.conn.Notify(protocol.MethodWorkspaceSetRoots, protocol.SetRootsParams{Roots: roots})
	first := <-busy // the indexer is now busy with the roots
	for i := range 3 {
		mustWriteFile(t, filepath.Join(other, "b.txt"), "beta "+string(rune('a'+i))+"\n")
		_ = client.conn.Notify(protocol.MethodWorkspaceDidChangeFiles, protocol.DidChangeFilesParams{Changes: []protocol.FileChange{{Path: filepath.Join(other, "b.txt"), Type: "changed"}}})
	}
	var parsed protocol.ParseResult
	if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: "alpha", Cursor: 5}, &parsed); err != nil {
		t.Fatalf("query/parse while the indexer is busy: %v", err)
	}
	release <- struct{}{}
	// The saves queued meanwhile are applied together, after the roots.
	second := <-busy
	release <- struct{}{}
	if len(first) != 1 || !first[0].isRoots || len(second) != 1 || len(second[0].changes) != 3 {
		t.Fatalf("batches = %+v then %+v, want the roots, then the three saves as one", first, second)
	}
	client.server.events.beforeApply = nil
	waitUntil(t, "the added root's saved file to be searchable", func() bool {
		var result protocol.SearchResult
		_ = client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s", Text: "beta c"}, &result)
		return result.Total == 1
	})
}

func TestQueuedIndexEventsCoalesceInOrder(t *testing.T) {
	change := func(path string) indexEvent {
		return indexEvent{changes: []protocol.FileChange{{Path: path}}}
	}
	roots := func(id string) indexEvent { return indexEvent{isRoots: true, roots: []protocol.Root{{ID: id}}} }
	got := coalesce([]indexEvent{change("a"), change("b"), roots("1"), roots("2"), change("c")})
	if len(got) != 3 || len(got[0].changes) != 2 || got[1].roots[0].ID != "2" || got[2].changes[0].Path != "c" {
		t.Fatalf("coalesce = %+v, want [a b] [roots 2] [c]", got)
	}
	if !slices.Equal([]string{got[0].changes[0].Path, got[0].changes[1].Path}, []string{"a", "b"}) {
		t.Fatalf("changes = %+v, want a then b", got[0].changes)
	}
}
