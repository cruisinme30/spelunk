package server

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/indexer"
	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

// DaemonVersion is reported by initialize.
const DaemonVersion = "0.1.0"

// shutdownGrace is how long shutdown waits for index work to stop (the
// shutdown request promises to finish within 2 seconds).
const shutdownGrace = 2 * time.Second

// Server is one daemon instance bound to one connection. It is safe for
// concurrent use: handlers run on their own goroutines.
type Server struct {
	conn  *rpc.Conn
	now   func() time.Time
	index *indexer.Indexer
	plans planMemory

	mu                sync.RWMutex
	roots             []protocol.Root
	settings          protocol.Settings
	shutdownRequested bool

	exitOnce sync.Once
	exitCode chan int
}

// Options configure a Server.
type Options struct {
	// Now replaces the clock for date-relative logic (UNIFIED_SEARCH_NOW in
	// tests). It is never used to time requests.
	Now func() time.Time
}

// New returns a Server with a handler for every daemon method registered on conn.
func New(conn *rpc.Conn, opts Options) *Server {
	s := &Server{conn: conn, now: opts.Now, exitCode: make(chan int, 1), settings: DefaultSettings()}
	if s.now == nil {
		s.now = time.Now
	}
	s.index = indexer.New(s.settings, s.notifyIndexStatus)
	conn.Handle(protocol.MethodInitialize, s.initialize)
	conn.Handle(protocol.MethodShutdown, s.shutdown)
	conn.OnNotify(protocol.MethodExit, func(json.RawMessage) { s.exit() })
	conn.OnNotify(protocol.MethodWorkspaceSetRoots, s.setRoots)
	conn.OnNotify(protocol.MethodSettingsUpdate, s.updateSettings)
	conn.Handle(protocol.MethodQueryParse, s.parse)
	s.registerSearch()
	s.registerIndex()
	return s
}

// DefaultSettings mirrors the defaults declared in extension/package.json.
func DefaultSettings() protocol.Settings {
	return protocol.Settings{
		CaseSensitive:  false,
		DefaultCount:   500,
		HistoryDepth:   "2y",
		Symbols:        true,
		Exclude:        []string{"**/vendor/**", "**/node_modules/**", "**/*.min.js"},
		IncludeIgnored: false,
		MaxFileSizeKB:  1024,
		Location:       "~/.unified-search/index",
	}
}

// Exited delivers the process exit code once, when exit arrives: 0 after a
// shutdown request, 1 otherwise (as in LSP).
func (s *Server) Exited() <-chan int { return s.exitCode }

// exit handles the exit notification by delivering the exit code once.
func (s *Server) exit() {
	s.exitOnce.Do(func() {
		s.mu.RLock()
		code := 1
		if s.shutdownRequested {
			code = 0
		}
		s.mu.RUnlock()
		s.exitCode <- code
	})
}

// decode unmarshals params, reporting bad input as CodeInvalidParams.
func decode(params json.RawMessage, v any) error {
	if len(params) == 0 || string(params) == "null" {
		return nil
	}
	if err := json.Unmarshal(params, v); err != nil {
		return rpc.Errorf(rpc.CodeInvalidParams, "invalid params: %v", err)
	}
	return nil
}

// initialize answers initialize: it applies the client's settings and
// roots, then reports the daemon and protocol versions.
func (s *Server) initialize(_ context.Context, raw json.RawMessage) (any, error) {
	var params protocol.InitializeParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	// Settings are required by the schema; DefaultCount is at least 1 in any
	// real settings object, so 0 means a client sent none.
	if params.Settings.DefaultCount != 0 {
		s.applySettings(params.Settings)
	}
	s.applyRoots(params.Roots)
	return protocol.InitializeResult{DaemonVersion: DaemonVersion, Protocol: protocol.Version}, nil
}

// shutdown stops index work, waiting up to shutdownGrace, and records that
// the client asked, so the exit that follows reports success.
func (s *Server) shutdown(_ context.Context, _ json.RawMessage) (any, error) {
	s.mu.Lock()
	s.shutdownRequested = true
	s.mu.Unlock()
	s.index.Close(shutdownGrace)
	return protocol.Empty{}, nil
}

// setRoots handles workspace/setRoots.
func (s *Server) setRoots(raw json.RawMessage) {
	var params protocol.SetRootsParams
	if decode(raw, &params) == nil {
		s.applyRoots(params.Roots)
	}
}

// updateSettings handles settings/update.
func (s *Server) updateSettings(raw json.RawMessage) {
	var params protocol.SettingsUpdateParams
	if decode(raw, &params) != nil {
		return
	}
	s.applySettings(params.Settings)
}

// applySettings stores new settings and passes them to the indexer, which
// rebuilds if they change what is indexed.
func (s *Server) applySettings(settings protocol.Settings) {
	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()
	s.index.SetSettings(s.Settings())
}

// applyRoots stores the workspace roots and passes them to the indexer,
// which starts indexing new roots and drops removed ones.
func (s *Server) applyRoots(roots []protocol.Root) {
	s.mu.Lock()
	s.roots = slices.Clone(roots)
	s.mu.Unlock()
	s.index.SetRoots(s.Roots())
}

// Settings returns a copy of the current settings.
func (s *Server) Settings() protocol.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	settings := s.settings
	settings.Exclude = slices.Clone(settings.Exclude)
	return settings
}

// Roots returns a copy of the current workspace roots.
func (s *Server) Roots() []protocol.Root {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.roots)
}
