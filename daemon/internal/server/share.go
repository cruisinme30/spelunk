package server

// Sharing: the spelunk command and AI agents search a VS Code window's
// roots, or the background reader's, through a Unix socket in the run
// folder that only the user can open. A guest gets the read-only methods
// only: nothing it asks for builds, saves, replaces or rebuilds.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/indexer"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
)

// DefaultAgentTimeLimitMs is the time limit of a guest's search when
// spelunk.agent.timeLimitMs is unset: long enough for a large history,
// since an agent can't tell a truncated answer to try again.
const DefaultAgentTimeLimitMs = 30000

// RunDir is the folder of the daemons' sockets: SPELUNK_RUN_DIR when set
// (tests), otherwise ~/.spelunk/run.
func RunDir() string {
	if dir := os.Getenv("SPELUNK_RUN_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "spelunk-run")
	}
	return filepath.Join(home, ".spelunk", "run")
}

// WindowSocket is where the window daemon with process ID pid listens.
func WindowSocket(runDir string, pid int) string {
	return filepath.Join(runDir, "w-"+strconv.Itoa(pid)+".sock")
}

// ReaderSocket is where the background reader listens.
func ReaderSocket(runDir string) string { return filepath.Join(runDir, "reader.sock") }

// ServeGuest answers one guest connection until it closes or ctx ends.
func (s *Server) ServeGuest(ctx context.Context, conn *rpc.Conn) error {
	guest := func(h rpc.Handler) rpc.Handler {
		return func(ctx context.Context, params json.RawMessage) (any, error) {
			if s.shuttingDown() {
				return nil, rpc.Errorf(rpc.CodeInvalidRequest, "the daemon is shutting down")
			}
			s.touch()
			return h(ctx, params)
		}
	}
	conn.Handle(protocol.MethodShareHello, guest(s.hello))
	conn.Handle(protocol.MethodShareOpen, guest(s.openShared))
	conn.Handle(protocol.MethodIndexStatus, guest(s.indexStatus))
	conn.Handle(protocol.MethodQueryParse, guest(s.parse))
	conn.Handle(protocol.MethodPreviewGet, guest(s.preview))
	conn.Handle(protocol.MethodSearchStart, guest(func(ctx context.Context, raw json.RawMessage) (any, error) {
		return s.searchOn(ctx, raw, conn, true)
	}))
	return conn.Serve(ctx)
}

// hello answers share/hello.
func (s *Server) hello(context.Context, json.RawMessage) (any, error) {
	return protocol.ShareHelloResult{
		DaemonVersion: DaemonVersion, Protocol: protocol.Version, Reader: s.reader,
		Location: s.Settings().Location, Repos: s.index.Status(),
	}, nil
}

// openShared answers share/open: the background reader starts serving
// the saved index of the root that holds the path, and answers once it
// is loaded. A window serves only its own roots.
func (s *Server) openShared(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.ShareOpenParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	if !s.reader {
		return nil, rpc.Errorf(rpc.CodeMethodNotFound, "only the background reader opens folders")
	}
	if !filepath.IsAbs(params.Path) {
		return nil, rpc.Errorf(rpc.CodeInvalidParams, "path must be absolute: %q", params.Path)
	}
	root := indexer.SavedRoot(s.Settings().Location, filepath.Clean(params.Path))
	if status, ok := s.repoStatus(root); ok {
		return status, nil
	}
	roots := append(s.Roots(), protocol.Root{ID: root, Path: root, Name: filepath.Base(root)})
	s.applyRoots(roots)
	for {
		if status, ok := s.repoStatus(root); ok {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// repoStatus is the status of the repo at root, once the indexer has it.
func (s *Server) repoStatus(root string) (protocol.RepoStatus, bool) {
	statuses := s.index.Status()
	for i := range statuses {
		if statuses[i].Root == root {
			return statuses[i], true
		}
	}
	return protocol.RepoStatus{}, false
}

// touch records a guest's request, for the reader's idle exit.
func (s *Server) touch() {
	s.lastGuest.Store(time.Now().UnixNano())
}

// IdleFor is how long ago a guest last asked for anything; since the
// server started when none has.
func (s *Server) IdleFor() time.Duration {
	return time.Since(time.Unix(0, s.lastGuest.Load()))
}

// Configure applies settings as settings/update does, for the background
// reader, which has no editor to send them.
func (s *Server) Configure(settings protocol.Settings) { s.applySettings(settings) }

// sharing is a window's socket while spelunk.agent.shareWindow is on.
type sharing struct {
	mu       sync.Mutex
	listener net.Listener
	cancel   context.CancelFunc
}

// updateSharing opens or closes the window's socket to match
// spelunk.agent.shareWindow. The background reader always shares (see
// Listen), so it doesn't come here.
func (s *Server) updateSharing(ctx context.Context) {
	if s.reader {
		return
	}
	on := s.Settings().ShareWindow
	s.share.mu.Lock()
	defer s.share.mu.Unlock()
	switch {
	case on && s.share.listener == nil:
		listener, err := Listen(ctx, RunDir(), WindowSocket(RunDir(), os.Getpid()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "spelunk-daemon: not sharing this window: %v\n", err)
			return
		}
		acceptCtx, cancel := context.WithCancel(ctx)
		s.share.listener, s.share.cancel = listener, cancel
		go s.Accept(acceptCtx, listener)
	case !on && s.share.listener != nil:
		s.closeSharing()
	}
}

// closeSharing closes the window's socket. Callers hold share.mu.
func (s *Server) closeSharing() {
	if s.share.listener == nil {
		return
	}
	s.share.cancel()
	_ = s.share.listener.Close() // also removes the socket file
	s.share.listener, s.share.cancel = nil, nil
}

// Listen opens the Unix socket at path, in runDir, which only the user
// can open: a socket left by a daemon that crashed is replaced.
func Listen(ctx context.Context, runDir, path string) (net.Listener, error) {
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	// A folder the user made earlier, more open, is closed up.
	if err := os.Chmod(runDir, 0o700); err != nil { //nolint:gosec // G302: a folder needs x to be entered; 0700 is owner-only
		return nil, err
	}
	var dialer net.Dialer
	if conn, err := dialer.DialContext(ctx, "unix", path); err == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s is in use", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var config net.ListenConfig
	return config.Listen(ctx, "unix", path)
}

// Accept serves each guest that connects to listener until ctx ends.
func (s *Server) Accept(ctx context.Context, listener net.Listener) {
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return // closed
		}
		go func() {
			defer conn.Close() //nolint:errcheck // the guest is gone either way
			_ = s.ServeGuest(ctx, rpc.NewConn(conn, conn))
		}()
	}
}
