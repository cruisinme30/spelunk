package server

import (
	"context"
	"io"
	"testing"
	"time"

	"unifiedsearch/daemon/protocol"
	"unifiedsearch/daemon/rpc"
)

// harness runs a Server against an in-process client over pipes.
type harness struct {
	t      *testing.T
	srv    *Server
	client *rpc.Conn
	stop   func()
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	sconn := rpc.NewConn(sr, sw)
	client := rpc.NewConn(cr, cw)
	srv := New(sconn, opts)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = sconn.Serve(ctx) }()
	go func() { _ = client.Serve(ctx) }()
	h := &harness{t: t, srv: srv, client: client}
	h.stop = func() { cancel(); cw.Close(); sw.Close() }
	t.Cleanup(h.stop)
	return h
}

func (h *harness) call(method string, params, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h.client.Call(ctx, method, params, out)
}

func TestLifecycle(t *testing.T) {
	h := newHarness(t, Options{})
	var res protocol.InitializeResult
	err := h.call(protocol.MethodInitialize, protocol.InitializeParams{
		Protocol: protocol.Version,
		Roots:    []protocol.Root{{ID: "abc", Path: t.TempDir(), Name: "r"}},
		Settings: DefaultSettings(),
	}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if res.Protocol != protocol.Version || res.DaemonVersion != DaemonVersion {
		t.Fatalf("initialize = %+v", res)
	}
	if got := h.srv.Roots(); len(got) != 1 || got[0].Name != "r" {
		t.Fatalf("roots = %+v", got)
	}
	if err := h.call(protocol.MethodShutdown, nil, nil); err != nil {
		t.Fatal(err)
	}
	_ = h.client.Notify(protocol.MethodExit, nil)
	select {
	case code := <-h.srv.Exited():
		if code != 0 {
			t.Fatalf("exit code after shutdown = %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no exit")
	}
}

func TestExitWithoutShutdownIsError(t *testing.T) {
	h := newHarness(t, Options{})
	_ = h.client.Notify(protocol.MethodExit, nil)
	if code := <-h.srv.Exited(); code != 1 {
		t.Fatalf("exit code = %d", code)
	}
}
