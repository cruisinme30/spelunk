package server

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

func TestRequestsAfterShutdownAreRefused(t *testing.T) {
	// @covers rpc:shutdown
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: t.TempDir(), Name: "r"})
	if err := client.call(protocol.MethodShutdown, nil, nil); err != nil {
		t.Fatal(err)
	}
	for method, params := range map[string]any{
		protocol.MethodSearchStart:  protocol.SearchStartParams{SearchID: "s", Text: "x"},
		protocol.MethodQueryParse:   protocol.ParseParams{Text: "x"},
		protocol.MethodIndexStatus:  nil,
		protocol.MethodIndexRebuild: protocol.RebuildParams{},
		protocol.MethodInitialize:   protocol.InitializeParams{Protocol: protocol.Version},
	} {
		wantRPCCode(t, method+" after shutdown", client.call(method, params, nil), rpc.CodeInvalidRequest)
	}
	// A second shutdown is still answered, and roots sent now are ignored:
	// the indexer is stopped and would only queue them forever.
	if err := client.call(protocol.MethodShutdown, nil, nil); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
	_ = client.conn.Notify(protocol.MethodWorkspaceSetRoots, protocol.SetRootsParams{Roots: []protocol.Root{{ID: "late", Path: t.TempDir(), Name: "late"}}})
	_ = client.conn.Notify(protocol.MethodExit, nil)
	if code := waitForExitCode(t, client.server); code != 0 {
		t.Fatalf("exit code after shutdown = %d, want 0", code)
	}
	if roots := client.server.Roots(); len(roots) != 1 || roots[0].ID != "r1" {
		t.Fatalf("Roots() after setRoots following shutdown = %+v, want only r1", roots)
	}
}

func TestUnusableSettingsFallBackToDefaults(t *testing.T) {
	// @covers rpc:settings/update
	client := newTestClient(t)
	client.mustInitialize(t)
	// Every field missing or out of range, as an older client or a
	// hand-edited settings.json can send.
	raw := json.RawMessage(`{"settings":{"defaultCount":-1,"maxFileSizeKB":0,"historyDepth":"forever","location":""}}`)
	if err := client.conn.Notify(protocol.MethodSettingsUpdate, raw); err != nil {
		t.Fatal(err)
	}
	defaults := DefaultSettings()
	waitUntil(t, "settings/update applied", func() bool { return !client.server.Settings().Symbols })
	got := client.server.Settings()
	if got.DefaultCount != defaults.DefaultCount || got.MaxFileSizeKB != defaults.MaxFileSizeKB ||
		got.HistoryDepth != defaults.HistoryDepth || got.Location != defaults.Location || got.Exclude == nil {
		t.Fatalf("Settings() after unusable values = %+v, want defaults for count, size, depth and location", got)
	}
}

func TestInitializeWithoutSettingsKeepsTheDefaults(t *testing.T) {
	// @covers rpc:initialize
	client := newTestClient(t)
	if err := client.call(protocol.MethodInitialize, json.RawMessage(`{"protocol":1,"roots":[]}`), nil); err != nil {
		t.Fatal(err)
	}
	got, want := client.server.Settings(), DefaultSettings()
	if got.Location != want.Location || !got.Symbols || got.DefaultCount != want.DefaultCount {
		t.Fatalf("Settings() after initialize without settings = %+v, want the defaults", got)
	}
}

func TestRootsARefCannotNameAreDropped(t *testing.T) {
	// @covers rpc:workspace/setRoots
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "hello world\n")
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t,
		protocol.Root{ID: "r1", Path: dir, Name: "a"},
		protocol.Root{ID: "r1", Path: dir, Name: "same id again"},
		protocol.Root{ID: "", Path: dir, Name: "no id"},
		protocol.Root{ID: "a|b", Path: dir, Name: "separator in id"},
		protocol.Root{ID: "rel", Path: "relative/dir", Name: "relative"},
	)
	roots := client.server.Roots()
	if len(roots) != 1 || roots[0].Name != "a" {
		t.Fatalf("Roots() = %+v, want only the first r1, named a", roots)
	}
	var result protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s", Text: "hello"}, &result); err != nil {
		t.Fatal(err)
	}
	items := batches.all()
	if result.Total != 1 || len(items) != 1 {
		t.Fatalf("search hello: total %d, %d items; want each file once", result.Total, len(items))
	}
	if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: items[0].Ref, ContextLines: 1}, nil); err != nil {
		t.Fatalf("preview of the only result: %v", err)
	}
}
