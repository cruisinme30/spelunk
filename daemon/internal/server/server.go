package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/indexer"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
)

// DaemonVersion is reported by initialize.
const DaemonVersion = "0.2.0"

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
	// Now replaces the clock for date-relative logic (SPELUNK_NOW in
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
	conn.Handle(protocol.MethodShutdown, s.shutdown) // answered again after shutdown, as LSP allows
	conn.OnNotify(protocol.MethodExit, func(json.RawMessage) { s.exit() })
	s.handle(protocol.MethodInitialize, s.initialize)
	s.onNotify(protocol.MethodWorkspaceSetRoots, s.setRoots)
	s.onNotify(protocol.MethodSettingsUpdate, s.updateSettings)
	s.onNotify(protocol.MethodWorkspaceDidChangeFiles, s.didChangeFiles)
	s.handle(protocol.MethodQueryParse, s.parse)
	s.registerSearch()
	s.registerIndex()
	return s
}

// handle registers h for method, refusing the request once shutdown has
// been asked for: the index work it would start or read is stopped.
func (s *Server) handle(method string, h rpc.Handler) {
	s.conn.Handle(method, func(ctx context.Context, params json.RawMessage) (any, error) {
		if s.shuttingDown() {
			return nil, rpc.Errorf(rpc.CodeInvalidRequest, "%s after shutdown", method)
		}
		return h(ctx, params)
	})
}

// onNotify registers h for method, ignoring the notification once shutdown
// has been asked for, so no new index work starts on a stopped indexer.
func (s *Server) onNotify(method string, h rpc.NotificationHandler) {
	s.conn.OnNotify(method, func(params json.RawMessage) {
		if !s.shuttingDown() {
			h(params)
		}
	})
}

// shuttingDown reports whether shutdown has been asked for.
func (s *Server) shuttingDown() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shutdownRequested
}

// Close stops index work, waiting up to the shutdown grace period, so a
// daemon ending without a shutdown request (stdin closed, a signal) does
// not leave a build half-written. Safe to call more than once.
func (s *Server) Close() {
	s.index.Close(shutdownGrace)
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
		Location:       "~/.spelunk/index",
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
	// Settings are required by the schema, but a client that sends none
	// keeps the defaults rather than an all-zero settings object.
	var present struct {
		Settings json.RawMessage `json:"settings"`
	}
	_ = decode(raw, &present) // raw already decoded above
	if len(present.Settings) > 0 && string(present.Settings) != "null" {
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

// didChangeFiles handles workspace/didChangeFiles: the editor saw files
// saved, created or deleted, and the indexer re-reads them.
func (s *Server) didChangeFiles(raw json.RawMessage) {
	var params protocol.DidChangeFilesParams
	if decode(raw, &params) == nil {
		s.index.FilesChanged(params.Changes)
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
	settings = withDefaults(settings)
	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()
	s.index.SetSettings(s.Settings())
}

// withDefaults replaces the settings values the daemon cannot use with
// their defaults. VS Code does not enforce a setting's minimum, and a
// client may omit fields: an empty location would put the index in the
// daemon's working directory, and a size limit of 0 or less would index
// files of any size.
func withDefaults(settings protocol.Settings) protocol.Settings {
	defaults := DefaultSettings()
	if settings.DefaultCount < 1 {
		settings.DefaultCount = defaults.DefaultCount
	}
	if settings.MaxFileSizeKB < 1 {
		settings.MaxFileSizeKB = defaults.MaxFileSizeKB
	}
	if !slices.Contains([]string{"6m", "2y", "all"}, settings.HistoryDepth) {
		settings.HistoryDepth = defaults.HistoryDepth
	}
	if strings.TrimSpace(settings.Location) == "" {
		settings.Location = defaults.Location
	}
	if settings.Exclude == nil {
		settings.Exclude = []string{}
	}
	return settings
}

// usableRoots keeps the roots a ref can name: an ID that is set, unique
// and free of the ref separator "|", and an absolute path (a relative one
// would resolve against the daemon's working directory). Folders that do
// not exist are kept, so their status can say so.
func usableRoots(roots []protocol.Root) []protocol.Root {
	usable := make([]protocol.Root, 0, len(roots))
	seen := map[string]bool{}
	for _, root := range roots {
		if root.ID == "" || strings.Contains(root.ID, "|") || seen[root.ID] || !filepath.IsAbs(root.Path) {
			continue
		}
		seen[root.ID] = true
		usable = append(usable, root)
	}
	return usable
}

// applyRoots stores the workspace roots and passes them to the indexer,
// which starts indexing new roots and drops removed ones.
func (s *Server) applyRoots(roots []protocol.Root) {
	roots = usableRoots(roots)
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
