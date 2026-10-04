// Package server wires the Contract 3 methods to the daemon's parts.
// It owns request lifecycle only: it never reads or writes an index.
package server

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"unifiedsearch/daemon/protocol"
	"unifiedsearch/daemon/rpc"
)

// DaemonVersion is reported by initialize.
const DaemonVersion = "0.1.0"

// Server is one daemon instance bound to one connection.
type Server struct {
	conn *rpc.Conn
	now  func() time.Time

	mu          sync.RWMutex
	roots       []protocol.Root
	settings    protocol.Settings
	initialized bool
	shutdown    bool

	exitOnce sync.Once
	exitCh   chan int
}

// Options configure a Server.
type Options struct {
	// Now overrides the clock (UNIFIED_SEARCH_NOW in tests).
	Now func() time.Time
}

// New registers every handler on conn.
func New(conn *rpc.Conn, opts Options) *Server {
	s := &Server{conn: conn, now: opts.Now, exitCh: make(chan int, 1)}
	if s.now == nil {
		s.now = time.Now
	}
	s.settings = DefaultSettings()
	conn.Handle(protocol.MethodInitialize, s.initialize)
	conn.Handle(protocol.MethodShutdown, s.handleShutdown)
	conn.OnNotify(protocol.MethodExit, func(json.RawMessage) { s.exit() })
	conn.OnNotify(protocol.MethodWorkspaceSetRoots, s.setRoots)
	conn.OnNotify(protocol.MethodSettingsUpdate, s.updateSettings)
	s.registerM0()
	return s
}

// DefaultSettings mirrors the package.json defaults (Contract 5).
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

// Exited is closed with the process exit code once `exit` arrives.
func (s *Server) Exited() <-chan int { return s.exitCh }

func (s *Server) exit() {
	s.exitOnce.Do(func() {
		s.mu.RLock()
		code := 1
		if s.shutdown {
			code = 0
		}
		s.mu.RUnlock()
		s.exitCh <- code
	})
}

func decode(p json.RawMessage, v any) error {
	if len(p) == 0 || string(p) == "null" {
		return nil
	}
	if err := json.Unmarshal(p, v); err != nil {
		return rpc.Errorf(rpc.CodeInvalidParams, "invalid params: %v", err)
	}
	return nil
}

func (s *Server) initialize(ctx context.Context, p json.RawMessage) (any, error) {
	var params protocol.InitializeParams
	if err := decode(p, &params); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.initialized = true
	if params.Settings.DefaultCount != 0 {
		s.settings = params.Settings
	}
	s.mu.Unlock()
	s.applyRoots(params.Roots)
	return protocol.InitializeResult{DaemonVersion: DaemonVersion, Protocol: protocol.Version}, nil
}

func (s *Server) handleShutdown(ctx context.Context, _ json.RawMessage) (any, error) {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	s.onShutdown()
	return protocol.Empty{}, nil
}

func (s *Server) setRoots(p json.RawMessage) {
	var params protocol.SetRootsParams
	if decode(p, &params) == nil {
		s.applyRoots(params.Roots)
	}
}

func (s *Server) updateSettings(p json.RawMessage) {
	var params protocol.SettingsUpdateParams
	if decode(p, &params) == nil {
		s.mu.Lock()
		s.settings = params.Settings
		s.mu.Unlock()
		s.onSettingsChanged()
	}
}

func (s *Server) applyRoots(roots []protocol.Root) {
	s.mu.Lock()
	s.roots = append([]protocol.Root(nil), roots...)
	s.mu.Unlock()
	s.onRootsChanged()
}

// Settings returns a copy of the current settings.
func (s *Server) Settings() protocol.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// Roots returns a copy of the current roots.
func (s *Server) Roots() []protocol.Root {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]protocol.Root(nil), s.roots...)
}

// Hooks filled in as the indexer and engines land.
func (s *Server) onRootsChanged()    {}
func (s *Server) onSettingsChanged() {}
func (s *Server) onShutdown()        {}
