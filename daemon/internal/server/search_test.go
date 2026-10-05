package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// kelvinSign is U+212A KELVIN SIGN. It looks like K and lowercases to an
// ASCII k, so a search for "kelvin" ignoring case matches it.
const kelvinSign = "\u212a"

// clientSource is a small Python file with one retry_policy match, on line 5.
const clientSource = "import logging\n\nclass Client:\n    def __init__(self):\n        self.retry_policy = RetryPolicy(max_attempts=3)\n"

// A search round-trips: results stream as batches, a result previews, and
// opening it resolves to the file, line and column of the match.
func TestSearchPreviewAndOpenRoundTrip(t *testing.T) {
	// @covers rpc:search/start rpc:search/batch rpc:preview/get rpc:open/resolve failure:ref-stale
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "client.py"), clientSource)
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "payments-api"})

	var result protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "retry_policy"}, &result); err != nil {
		t.Fatal(err)
	}
	items := batches.all()
	if result.Total != 1 || len(items) != 1 {
		t.Fatalf("search retry_policy: total %d, %d items; want 1 and 1", result.Total, len(items))
	}
	item := items[0]
	if item.Path != "client.py" || item.Line != 5 || item.Hits[0] != (protocol.Hit{Start: 13, End: 25}) {
		t.Fatalf("result = %s:%d hits %+v, want client.py:5 hits [{13 25 0}]", item.Path, item.Line, item.Hits)
	}

	var preview protocol.Preview
	if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: item.Ref, ContextLines: 2}, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.FocusLine != 5 || preview.FirstLine != 3 || len(preview.Lines) != 4 {
		t.Fatalf("preview = focus %d, first %d, %d lines; want 5, 3, 4", preview.FocusLine, preview.FirstLine, len(preview.Lines))
	}

	var target protocol.OpenTarget
	if err := client.call(protocol.MethodOpenResolve, protocol.OpenResolveParams{Ref: item.Ref}, &target); err != nil {
		t.Fatal(err)
	}
	want := protocol.OpenTarget{Path: filepath.Join(dir, "client.py"), Line: 5, Column: 14, Length: 12}
	if target != want {
		t.Fatalf("open/resolve = %+v, want %+v", target, want)
	}

	if err := os.Remove(filepath.Join(dir, "client.py")); err != nil {
		t.Fatal(err)
	}
	err := client.call(protocol.MethodOpenResolve, protocol.OpenResolveParams{Ref: item.Ref}, nil)
	wantRPCCode(t, "open/resolve after delete", err, protocol.CodeRefStale)
}

func TestMatchOffsetsAreUTF16EvenWhenCaseFoldingChangesByteLength(t *testing.T) {
	dir := t.TempDir()
	// Each KELVIN SIGN is 3 bytes in UTF-8 but 1 UTF-16 unit, like the k it
	// folds to.
	mustWriteFile(t, filepath.Join(dir, "units.txt"), kelvinSign+kelvinSign+" = kelvin_scale\n")
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "r"})
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "kelvin"}, nil); err != nil {
		t.Fatal(err)
	}
	items := batches.all()
	if len(items) != 1 || items[0].Hits[0] != (protocol.Hit{Start: 5, End: 11}) {
		t.Fatalf("search kelvin = %+v, want one hit at UTF-16 [5,11)", items)
	}
}

func TestPreviewOfCRLFFileHasNoCarriageReturns(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "one\r\ntwo needle\r\nthree\r\n")
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "r"})
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "needle"}, nil); err != nil {
		t.Fatal(err)
	}
	var preview protocol.Preview
	if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: batches.all()[0].Ref, ContextLines: 1}, &preview); err != nil {
		t.Fatal(err)
	}
	if got := preview.Lines[1]; got != "two needle" {
		t.Fatalf("preview line 2 = %q, want %q", got, "two needle")
	}
}

func TestMalformedOrForeignRefsAreStale(t *testing.T) {
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: t.TempDir(), Name: "r"})
	for _, ref := range []string{"", "line|r1|1|0|1|a.txt", "tree|1|other-root|1|0|1|a.txt"} {
		err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: ref, ContextLines: 1}, nil)
		wantRPCCode(t, "preview/get "+ref, err, protocol.CodeRefStale)
	}
}

func TestInvalidQueriesAreRejected(t *testing.T) {
	client := newTestClient(t)
	client.mustInitialize(t)
	err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "(retry"}, nil)
	wantRPCCode(t, "search/start (retry", err, protocol.CodeQueryInvalid)
}

// Load more: the cursor from one page starts the next.
func TestCursorLoadsTheNextPage(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "x1 hit\nx2 hit\nx3 hit\n")
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "r"})

	var first, second protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "count:2 hit"}, &first); err != nil {
		t.Fatal(err)
	}
	if first.Total != 3 || first.NextCursor == "" {
		t.Fatalf("first page = %+v, want total 3 and a next cursor", first)
	}
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s2", Text: "count:2 hit", Cursor: first.NextCursor}, &second); err != nil {
		t.Fatal(err)
	}
	items := batches.all()
	if len(items) != 3 {
		t.Fatalf("after two pages: %d items, want 3", len(items))
	}
	if items[2].Line != 3 || second.NextCursor != "" {
		t.Errorf("after two pages: last line %d, next cursor %q; want line 3 and no more pages", items[2].Line, second.NextCursor)
	}
}

func TestIndexStatusReportsEveryRoot(t *testing.T) {
	// @covers rpc:index/status
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: t.TempDir(), Name: "web"})
	var status protocol.IndexStatusResult
	if err := client.call(protocol.MethodIndexStatus, protocol.Empty{}, &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Repos) != 1 || status.Repos[0].Name != "web" || status.Repos[0].Tree != protocol.IndexStateReady {
		t.Errorf("index/status = %+v, want web ready", status.Repos)
	}
}

func TestAPartBatchIsSentWithoutWaitingForTheSearchToEnd(t *testing.T) {
	client := newTestClient(t)
	batches := collectBatches(client)
	b := &batcher{conn: client.server.conn, searchID: "s1"}
	b.add(protocol.ResultItem{Kind: "line", Ref: "x", Path: "a.go", Line: 1})
	deadline := time.Now().Add(2 * time.Second)
	for len(batches.of("s1")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("a single result was never sent; want it within batchDelay")
		}
		time.Sleep(time.Millisecond)
	}
	b.flush() // nothing left: sends nothing more
	if got := len(batches.all()); got != 1 {
		t.Errorf("%d items sent, want 1", got)
	}
}
