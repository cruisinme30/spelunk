// Package cli is the spelunk command: search, status and help in a
// terminal, the background reader it starts, and the MCP server agents
// run. It never builds or saves an index. It asks the VS Code window that
// has the folder open, over that window's socket, and otherwise the
// background reader, which serves the saved index.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/server"
)

// ErrNotIndexed is Connect's error for a folder no index covers.
var ErrNotIndexed = errors.New("not indexed")

// NotIndexedError says which root has no usable index, and why.
type NotIndexedError struct {
	Root   string
	Reason string // the reader's status message
}

func (e *NotIndexedError) Error() string { return e.Root + ": " + e.Reason }

// Unwrap makes errors.Is(err, ErrNotIndexed) hold.
func (*NotIndexedError) Unwrap() error { return ErrNotIndexed }

// Session is a connection to the daemon that answers for one root.
type Session struct {
	conn  *rpc.Conn
	close func()
	// Repo is the root the session searches.
	Repo protocol.RepoStatus
	// Live is true when a VS Code window answers, false for the reader.
	Live bool

	mu      sync.Mutex
	batches map[string][]protocol.ResultItem
	next    int
}

// Options say where to look and how to report progress.
type Options struct {
	// RunDir holds the sockets (server.RunDir by default).
	RunDir string
	// Config is the index location and settings the reader uses.
	Config Config
	// Progress, when set, hears what takes a while: starting the reader,
	// loading a large index.
	Progress func(string)
}

// Connect finds the daemon that answers for folder (absolute): a VS Code
// window with it open, or else the background reader, which it starts if
// it isn't running.
func Connect(ctx context.Context, folder string, opts Options) (*Session, error) {
	if opts.RunDir == "" {
		opts.RunDir = server.RunDir()
	}
	if session := connectWindow(ctx, folder, opts.RunDir); session != nil {
		return session, nil
	}
	return connectReader(ctx, folder, opts)
}

// connectWindow asks every window's socket for the innermost open root
// that holds folder. Sockets nothing answers are left by daemons that
// crashed, and removed.
func connectWindow(ctx context.Context, folder, runDir string) *Session {
	sockets, _ := filepath.Glob(filepath.Join(runDir, "w-*.sock"))
	var best *Session
	for _, socket := range sockets {
		session, err := dial(ctx, socket)
		if err != nil {
			if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
				_ = os.Remove(socket)
			}
			continue
		}
		var hello protocol.ShareHelloResult
		if session.call(ctx, protocol.MethodShareHello, protocol.Empty{}, &hello) != nil {
			session.Close()
			continue
		}
		repo, ok := innermost(hello.Repos, folder)
		if !ok || (best != nil && len(repo.Root) <= len(best.Repo.Root)) {
			session.Close()
			continue
		}
		if best != nil {
			best.Close()
		}
		session.Repo, session.Live, best = repo, true, session
	}
	return best
}

// innermost is the repo whose root is folder or holds it, the deepest one
// when roots nest.
func innermost(repos []protocol.RepoStatus, folder string) (protocol.RepoStatus, bool) {
	var best protocol.RepoStatus
	found := false
	for i := range repos {
		root := repos[i].Root
		if root == "" || (folder != root && !strings.HasPrefix(folder, root+string(filepath.Separator))) {
			continue
		}
		if !found || len(root) > len(best.Root) {
			best, found = repos[i], true
		}
	}
	return best, found
}

// readerWait is how long Connect waits for a reader it started to listen.
var readerWait = 10 * time.Second

// connectReader asks the background reader for folder, starting it first
// if it isn't running.
func connectReader(ctx context.Context, folder string, opts Options) (*Session, error) {
	socket := server.ReaderSocket(opts.RunDir)
	session, err := dial(ctx, socket)
	if err != nil {
		if opts.Progress != nil {
			opts.Progress("Starting Spelunk's background reader…")
		}
		if startErr := startReader(ctx, opts); startErr != nil {
			return nil, fmt.Errorf("start the background reader: %w", startErr)
		}
		deadline := time.Now().Add(readerWait)
		for session, err = dial(ctx, socket); err != nil; session, err = dial(ctx, socket) {
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("the background reader didn't start: %w", err)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	loading := time.AfterFunc(time.Second, func() {
		if opts.Progress != nil {
			opts.Progress("Loading " + filepath.Base(folder) + "'s saved index (a large repo takes a while the first time)…")
		}
	})
	var status protocol.RepoStatus
	err = session.call(ctx, protocol.MethodShareOpen, protocol.ShareOpenParams{Path: folder}, &status)
	loading.Stop()
	if err != nil {
		session.Close()
		return nil, err
	}
	if status.Tree != protocol.IndexStateReady {
		session.Close()
		return nil, &NotIndexedError{Root: status.Root, Reason: status.Message}
	}
	session.Repo = status
	return session, nil
}

// startReader starts the background reader, detached from this process.
// It outlives ctx: other commands and agents use it after this one ends.
func startReader(ctx context.Context, opts Options) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if mkdirErr := os.MkdirAll(opts.RunDir, 0o700); mkdirErr != nil {
		return mkdirErr
	}
	config, err := json.Marshal(opts.Config)
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(opts.RunDir, "reader.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()                                                                             //nolint:errcheck // the child has its own copy
	cmd := exec.CommandContext(context.WithoutCancel(ctx), exe, "reader", "--config", string(config)) //nolint:gosec // G204: our own executable
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.Env = append(os.Environ(), "SPELUNK_RUN_DIR="+opts.RunDir)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// dial connects to the daemon socket at path.
func dial(ctx context.Context, path string) (*Session, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var dialer net.Dialer
	socket, err := dialer.DialContext(dialCtx, "unix", path)
	if err != nil {
		return nil, err
	}
	session := &Session{conn: rpc.NewConn(socket, socket), batches: map[string][]protocol.ResultItem{}}
	session.conn.OnNotify(protocol.MethodSearchBatch, func(raw json.RawMessage) {
		var batch protocol.SearchBatchParams
		if json.Unmarshal(raw, &batch) == nil {
			session.mu.Lock()
			session.batches[batch.SearchID] = append(session.batches[batch.SearchID], batch.Items...)
			session.mu.Unlock()
		}
	})
	serveCtx, stop := context.WithCancel(context.WithoutCancel(ctx)) // the session outlives the dial
	go func() { _ = session.conn.Serve(serveCtx) }()
	session.close = func() {
		stop()
		_ = socket.Close()
	}
	return session, nil
}

// Close ends the session.
func (s *Session) Close() { s.close() }

// call makes one request.
func (s *Session) call(ctx context.Context, method string, params, out any) error {
	return s.conn.Call(ctx, method, params, out)
}

// refresh reads the session's repo status again.
func (s *Session) refresh(ctx context.Context) {
	var status protocol.IndexStatusResult
	if s.call(ctx, protocol.MethodIndexStatus, protocol.Empty{}, &status) != nil {
		return
	}
	if repo, ok := innermost(status.Repos, s.Repo.Root); ok && repo.Root == s.Repo.Root {
		s.Repo = repo
	}
}

// SearchOptions shape one search.
type SearchOptions struct {
	// Count is how many results to return when the query has no count:.
	Count int
	// TimeLimitMs, when set, replaces the daemon's agent time limit.
	TimeLimitMs int
}

// Search runs text on the session's root and returns the results with
// the totals.
func (s *Session) Search(ctx context.Context, text string, opts SearchOptions) (protocol.SearchResult, []protocol.ResultItem, error) {
	s.mu.Lock()
	s.next++
	id := fmt.Sprintf("cli-%d", s.next)
	s.mu.Unlock()
	params := protocol.SearchStartParams{SearchID: id, Text: text, Roots: []string{s.Repo.Root}, Count: opts.Count, TimeLimitMs: opts.TimeLimitMs}
	var result protocol.SearchResult
	err := s.call(ctx, protocol.MethodSearchStart, params, &result)
	s.mu.Lock()
	items := s.batches[id]
	delete(s.batches, id)
	s.mu.Unlock()
	return result, items, err
}

// Preview returns the text around a result.
func (s *Session) Preview(ctx context.Context, ref string, contextLines int) (protocol.Preview, error) {
	var preview protocol.Preview
	err := s.call(ctx, protocol.MethodPreviewGet, protocol.PreviewParams{Ref: ref, ContextLines: contextLines}, &preview)
	return preview, err
}

// QueryError is the message of a search refused for a bad query, without
// the RPC wrapping: "kind: needs a value".
func QueryError(err error) (string, bool) {
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != protocol.CodeQueryInvalid {
		return "", false
	}
	return strings.TrimPrefix(rpcErr.Message, "query has errors: "), true
}
