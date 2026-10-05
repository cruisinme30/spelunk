package server

import (
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// waitForExitCode returns the exit code srv delivers, failing after 2s.
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

func TestInitializeThenShutdownThenExitExitsCleanly(t *testing.T) {
	// @covers rpc:initialize rpc:shutdown rpc:exit
	client := newTestClient(t)
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

func TestExitWithoutShutdownReportsFailure(t *testing.T) {
	// @covers rpc:exit
	client := newTestClient(t)
	_ = client.conn.Notify(protocol.MethodExit, nil)
	if code := waitForExitCode(t, client.server); code != 1 {
		t.Fatalf("exit code without shutdown = %d, want 1", code)
	}
}

func TestSettingsReturnsACopy(t *testing.T) {
	client := newTestClient(t)
	settings := client.server.Settings()
	settings.Exclude[0] = "changed"
	if got := client.server.Settings().Exclude[0]; got == "changed" {
		t.Fatalf("Settings().Exclude[0] = %q after editing a copy, want the original", got)
	}
}
