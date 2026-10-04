package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"unifiedsearch/daemon/protocol"
)

// M0 exit gate: one hard-coded search round-trips, and opening a result
// resolves to the file, line and column of the match.
// @covers rpc:search/start rpc:search/batch rpc:preview/get rpc:open/resolve
func TestM0SearchRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := "import logging\n\nclass Client:\n    def __init__(self):\n        self.retry_policy = RetryPolicy(max_attempts=3)\n"
	if err := os.WriteFile(filepath.Join(dir, "client.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, Options{})
	var mu sync.Mutex
	var got []protocol.ResultItem
	h.client.OnNotify(protocol.MethodSearchBatch, func(p json.RawMessage) {
		var b protocol.SearchBatchParams
		_ = json.Unmarshal(p, &b)
		mu.Lock()
		got = append(got, b.Items...)
		mu.Unlock()
	})
	root := protocol.Root{ID: "r1", Path: dir, Name: "payments-api"}
	if err := h.call(protocol.MethodInitialize, protocol.InitializeParams{Protocol: 1, Roots: []protocol.Root{root}, Settings: DefaultSettings()}, nil); err != nil {
		t.Fatal(err)
	}
	var res protocol.SearchResult
	if err := h.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "retry_policy"}, &res); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if res.Total != 1 || len(got) != 1 {
		t.Fatalf("total=%d items=%d", res.Total, len(got))
	}
	it := got[0]
	if it.Path != "client.py" || it.Line != 5 || it.Hits[0].Start != 13 || it.Hits[0].End != 25 {
		t.Fatalf("item = %+v", it)
	}
	var pv protocol.Preview
	if err := h.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: it.Ref, ContextLines: 2}, &pv); err != nil {
		t.Fatal(err)
	}
	if pv.FocusLine != 5 || pv.FirstLine != 3 || len(pv.Lines) != 4 {
		t.Fatalf("preview = %+v", pv)
	}
	var target protocol.OpenTarget
	if err := h.call(protocol.MethodOpenResolve, protocol.OpenResolveParams{Ref: it.Ref}, &target); err != nil {
		t.Fatal(err)
	}
	if target.Path != filepath.Join(dir, "client.py") || target.Line != 5 || target.Column != 14 || target.Length != 12 {
		t.Fatalf("target = %+v", target)
	}
	// A deleted file makes the ref stale.
	os.Remove(filepath.Join(dir, "client.py"))
	err := h.call(protocol.MethodOpenResolve, protocol.OpenResolveParams{Ref: it.Ref}, nil)
	if err == nil {
		t.Fatal("want RefStale")
	}
}
