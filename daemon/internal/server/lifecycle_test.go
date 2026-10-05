package server

import (
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
