package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/server"
)

func TestMCPSavedReaderReportsFreshnessAndReadsChangedFiles(t *testing.T) {
	f := newFixture(t, false)
	f.window.Close()
	backend := f.savedMCPBackend(t)
	saved, err := time.Parse(time.RFC3339, backend.Repo.SavedAt)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, "src", "retry.go")
	if err := os.WriteFile(path, []byte("package src\n\nfunc backoffPolicy() int { return 3 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	env := Env{Cwd: f.root, Now: func() time.Time { return saved.Add(2 * time.Hour) }}
	client := connectMCPTestClient(t, mcpServerWithConnector(env, func(context.Context, string, Options) (*Session, error) { return backend, nil }))
	text, isError := callText(t, client, "spelunk_search", map[string]any{"query": "symbol:backoffPolicy"})
	if isError || !strings.Contains(text, "src/retry.go:3:function backoffPolicy") || !strings.Contains(text, "index saved 2 h ago") || !strings.Contains(text, "1 changed file re-read") {
		t.Fatalf("saved-index search = %q, error=%v", text, isError)
	}
	text, isError = callText(t, client, "spelunk_status", nil)
	if isError || !strings.Contains(text, "index saved 2 h ago") || !strings.Contains(text, "1 changed file re-read") {
		t.Errorf("saved-index status = %q, error=%v", text, isError)
	}
}

// savedMCPBackend exercises the real read-only guest API and persisted
// index loading, substituting pipes only for the environment's blocked socket.
func (f *fixture) savedMCPBackend(t *testing.T) *Session {
	t.Helper()
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	serverConn := rpc.NewConn(serverIn, serverOut)
	client := rpc.NewConn(clientIn, clientOut)
	reader := server.New(rpc.NewConn(strings.NewReader(""), io.Discard), server.Options{Reader: true})
	settings := server.DefaultSettings()
	settings.Location = f.location
	reader.Configure(settings)
	ctx, cancel := context.WithCancel(t.Context())
	go func() { _ = reader.ServeGuest(ctx, serverConn) }()
	go func() { _ = client.Serve(ctx) }()
	t.Cleanup(func() {
		reader.Close()
		cancel()
		_ = serverIn.Close()
		_ = serverOut.Close()
		_ = clientIn.Close()
		_ = clientOut.Close()
	})
	session := &Session{conn: client, close: func() {}, batches: map[string][]protocol.ResultItem{}}
	watchMCPBatches(session)
	if _, err := openReader(t.Context(), session, f.root, Options{}); err != nil {
		t.Fatal(err)
	}
	return session
}
