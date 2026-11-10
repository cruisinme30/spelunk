package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
)

func TestMCPReaderRejectsIncompatibleDaemonBeforeOpeningIndex(t *testing.T) {
	for _, version := range []int{0, protocol.Version - 1, protocol.Version, protocol.Version + 1} {
		var opened atomic.Int32
		session, peer, closed := mcpReaderPeer(t)
		peer.Handle(protocol.MethodShareHello, func(context.Context, json.RawMessage) (any, error) {
			return protocol.ShareHelloResult{Protocol: version, DaemonVersion: "test"}, nil
		})
		peer.Handle(protocol.MethodShareOpen, func(context.Context, json.RawMessage) (any, error) {
			opened.Add(1)
			return protocol.RepoStatus{Root: "/repo", Tree: protocol.IndexStateReady}, nil
		})
		got, err := openReader(t.Context(), session, "/repo", Options{})
		if version == protocol.Version {
			if err != nil || got != session || opened.Load() != 1 || closed.Load() {
				t.Errorf("current daemon: session=%v error=%v opens=%d closed=%v", got, err, opened.Load(), closed.Load())
			}
			continue
		}
		if got != nil || err == nil || opened.Load() != 0 || !closed.Load() || !strings.Contains(err.Error(), "Update Spelunk and restart") {
			t.Errorf("protocol %d: session=%v error=%v opens=%d closed=%v", version, got, err, opened.Load(), closed.Load())
		}
	}
}

func mcpReaderPeer(t *testing.T) (*Session, *rpc.Conn, *atomic.Bool) {
	t.Helper()
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	peer := rpc.NewConn(serverIn, serverOut)
	client := rpc.NewConn(clientIn, clientOut)
	ctx, cancel := context.WithCancel(t.Context())
	go func() { _ = peer.Serve(ctx) }()
	go func() { _ = client.Serve(ctx) }()
	var once sync.Once
	closed := &atomic.Bool{}
	session := &Session{conn: client, close: func() {
		once.Do(func() {
			closed.Store(true)
			cancel()
			_ = serverIn.Close()
			_ = serverOut.Close()
			_ = clientIn.Close()
			_ = clientOut.Close()
		})
	}}
	t.Cleanup(session.Close)
	return session, peer, closed
}

func TestMCPWindowSelectionDoesNotHideProtocolMismatch(t *testing.T) {
	for _, test := range []struct {
		name         string
		roots        []string
		versions     []int
		wantRoot     string
		incompatible bool
	}{
		{"old only", []string{"/repo"}, []int{protocol.Version - 1}, "", true},
		{"compatible same root", []string{"/repo", "/repo"}, []int{protocol.Version - 1, protocol.Version}, "/repo", false},
		{"old inner root", []string{"/repo", "/repo/nested"}, []int{protocol.Version, protocol.Version - 1}, "", true},
		{"current inner root", []string{"/repo", "/repo/nested"}, []int{protocol.Version - 1, protocol.Version}, "/repo/nested", false},
		{"unrelated old window", []string{"/other", "/repo"}, []int{protocol.Version - 1, protocol.Version}, "/repo", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var selection windowSelection
			closed := 0
			for i, root := range test.roots {
				session := &Session{close: func() { closed++ }}
				hello := protocol.ShareHelloResult{Protocol: test.versions[i], Repos: []protocol.RepoStatus{{Root: root}}}
				selection.consider(session, hello, "/repo/nested/src")
			}
			session, err := selection.result()
			if (err != nil) != test.incompatible {
				t.Fatalf("selection error = %v", err)
			}
			if test.wantRoot != "" {
				if session == nil || session.Repo.Root != test.wantRoot {
					t.Fatalf("selected %v, want %s", session, test.wantRoot)
				}
				session.Close()
			} else if session != nil {
				t.Error("incompatible selection returned a session")
			}
			if closed != len(test.roots) {
				t.Errorf("closed %d sessions, want %d", closed, len(test.roots))
			}
		})
	}
}

func TestMCPCancelledConnectionDoesNotStartReader(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runDir := filepath.Join(t.TempDir(), "not-created")
	if _, err := Connect(ctx, t.TempDir(), Options{RunDir: runDir}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled connection = %v", err)
	}
	if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cancelled connection created reader state: %v", err)
	}
}
