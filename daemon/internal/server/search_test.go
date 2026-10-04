package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

const clientSource = "import logging\n\nclass Client:\n    def __init__(self):\n        self.retry_policy = RetryPolicy(max_attempts=3)\n"

// collectBatches records every search/batch item the client receives.
func collectBatches(client *testClient) func() []protocol.ResultItem {
	var mu sync.Mutex
	var items []protocol.ResultItem
	client.conn.OnNotify(protocol.MethodSearchBatch, func(raw json.RawMessage) {
		var batch protocol.SearchBatchParams
		_ = json.Unmarshal(raw, &batch)
		mu.Lock()
		items = append(items, batch.Items...)
		mu.Unlock()
	})
	return func() []protocol.ResultItem {
		mu.Lock()
		defer mu.Unlock()
		return append([]protocol.ResultItem(nil), items...)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantRPCCode(t *testing.T, call string, err error, want int) {
	t.Helper()
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != want {
		t.Fatalf("%s error = %v, want code %d", call, err, want)
	}
}

// A search round-trips: results stream as batches, a result previews, and
// opening it resolves to the file, line and column of the match.
//
// @covers rpc:search/start rpc:search/batch rpc:preview/get rpc:open/resolve failure:ref-stale
func TestSearchPreviewAndOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "client.py"), clientSource)
	client := newTestClient(t, Options{})
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "payments-api"})

	var result protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "retry_policy"}, &result); err != nil {
		t.Fatal(err)
	}
	items := batches()
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
	// U+212A KELVIN SIGN lowercases to ASCII "k": 3 bytes become 1.
	mustWriteFile(t, filepath.Join(dir, "units.txt"), "KK = kelvin_scale\n")
	client := newTestClient(t, Options{})
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "r"})
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "kelvin"}, nil); err != nil {
		t.Fatal(err)
	}
	items := batches()
	if len(items) != 1 || items[0].Hits[0] != (protocol.Hit{Start: 5, End: 11}) {
		t.Fatalf("search kelvin = %+v, want one hit at UTF-16 [5,11)", items)
	}
}

func TestPreviewOfCRLFFileHasNoCarriageReturns(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "one\r\ntwo needle\r\nthree\r\n")
	client := newTestClient(t, Options{})
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "r"})
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "needle"}, nil); err != nil {
		t.Fatal(err)
	}
	var preview protocol.Preview
	if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: batches()[0].Ref, ContextLines: 1}, &preview); err != nil {
		t.Fatal(err)
	}
	if got := preview.Lines[1]; got != "two needle" {
		t.Fatalf("preview line 2 = %q, want %q", got, "two needle")
	}
}

func TestMalformedRefIsStale(t *testing.T) {
	for _, ref := range []string{"", "line|r1|x|0|1|a.txt", "line|r1|1|-2|1|a.txt", "other|r1|1|0|1|a.txt"} {
		if _, err := parseLineRef(ref); err == nil {
			t.Errorf("parseLineRef(%q) = nil error, want malformed ref", ref)
		}
	}
	original := lineRef{rootID: "r1", line: 3, column: 4, length: 5, path: "dir/a|b.txt"}
	if got, err := parseLineRef(original.String()); err != nil || got != original {
		t.Errorf("parseLineRef(%q) = %+v, %v; want %+v", original.String(), got, err, original)
	}
}
