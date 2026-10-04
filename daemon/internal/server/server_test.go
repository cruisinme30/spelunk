package server

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

// testClient talks to a Server over in-process pipes.
type testClient struct {
	server *Server
	conn   *rpc.Conn
}

func newTestClient(t *testing.T, opts Options) *testClient {
	t.Helper()
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	serverConn := rpc.NewConn(serverIn, serverOut)
	clientConn := rpc.NewConn(clientIn, clientOut)
	srv := New(serverConn, opts)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = serverConn.Serve(ctx) }()
	go func() { _ = clientConn.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		clientOut.Close()
		serverOut.Close()
	})
	return &testClient{server: srv, conn: clientConn}
}

func (c *testClient) call(method string, params, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.conn.Call(ctx, method, params, out)
}

func (c *testClient) mustInitialize(t *testing.T, roots ...protocol.Root) {
	t.Helper()
	params := protocol.InitializeParams{Protocol: protocol.Version, Roots: roots, Settings: DefaultSettings()}
	if err := c.call(protocol.MethodInitialize, params, nil); err != nil {
		t.Fatalf("initialize: %v", err)
	}
}

func waitForExitCode(t *testing.T, srv *Server) int {
	t.Helper()
	select {
	case code := <-srv.Exited():
		return code
	case <-time.After(2 * time.Second):
		t.Fatal("no exit code 2s after exit, want one")
		return -1
	}
}

// @covers rpc:initialize rpc:shutdown rpc:exit
func TestInitializeThenShutdownThenExitExitsCleanly(t *testing.T) {
	client := newTestClient(t, Options{})
	var got protocol.InitializeResult
	err := client.call(protocol.MethodInitialize, protocol.InitializeParams{
		Protocol: protocol.Version,
		Roots:    []protocol.Root{{ID: "abc", Path: t.TempDir(), Name: "r"}},
		Settings: DefaultSettings(),
	}, &got)
	if err != nil {
		t.Fatal(err)
	}
	if want := (protocol.InitializeResult{DaemonVersion: DaemonVersion, Protocol: protocol.Version}); got != want {
		t.Fatalf("initialize = %+v, want %+v", got, want)
	}
	if roots := client.server.Roots(); len(roots) != 1 || roots[0].Name != "r" {
		t.Fatalf("Roots() = %+v, want one root named r", roots)
	}
	if err := client.call(protocol.MethodShutdown, nil, nil); err != nil {
		t.Fatal(err)
	}
	_ = client.conn.Notify(protocol.MethodExit, nil)
	if code := waitForExitCode(t, client.server); code != 0 {
		t.Fatalf("exit code after shutdown = %d, want 0", code)
	}
}

// @covers rpc:exit
func TestExitWithoutShutdownReportsFailure(t *testing.T) {
	client := newTestClient(t, Options{})
	_ = client.conn.Notify(protocol.MethodExit, nil)
	if code := waitForExitCode(t, client.server); code != 1 {
		t.Fatalf("exit code without shutdown = %d, want 1", code)
	}
}

func TestSettingsReturnsACopy(t *testing.T) {
	client := newTestClient(t, Options{})
	settings := client.server.Settings()
	settings.Exclude[0] = "changed"
	if got := client.server.Settings().Exclude[0]; got == "changed" {
		t.Fatalf("Settings().Exclude[0] = %q after editing a copy, want the original", got)
	}
}
