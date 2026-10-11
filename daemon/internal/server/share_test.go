package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// shortRunDir is a run folder with a path short enough for a Unix socket
// (macOS allows 104 bytes; t.TempDir is often longer).
func shortRunDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "spr") //nolint:usetesting // t.TempDir's path is too long for a socket
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("SPELUNK_RUN_DIR", dir)
	return dir
}

// guest is a connection to a daemon's socket, with the batches it receives.
type guest struct {
	conn    *rpc.Conn
	mu      sync.Mutex
	batches []protocol.ResultItem
}

func dialGuest(t *testing.T, path string) *guest {
	t.Helper()
	var dialer net.Dialer
	socket, err := dialer.DialContext(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	g := &guest{conn: rpc.NewConn(socket, socket)}
	g.conn.OnNotify(protocol.MethodSearchBatch, func(raw json.RawMessage) {
		var batch protocol.SearchBatchParams
		_ = json.Unmarshal(raw, &batch)
		g.mu.Lock()
		g.batches = append(g.batches, batch.Items...)
		g.mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = g.conn.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = socket.Close()
	})
	return g
}

func (g *guest) call(method string, params, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return g.conn.Call(ctx, method, params, out)
}

func (g *guest) items() []protocol.ResultItem {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]protocol.ResultItem(nil), g.batches...)
}

// shareWindow turns spelunk.agent.shareWindow on or off for client's daemon.
func shareWindow(t *testing.T, client *testClient, location string, on bool) {
	t.Helper()
	settings := DefaultSettings()
	settings.Location, settings.ShareWindow = location, on
	if err := client.conn.Notify(protocol.MethodSettingsUpdate, protocol.SettingsUpdateParams{Settings: settings}); err != nil {
		t.Fatal(err)
	}
}

func TestAGuestSearchesTheWindowsRootsThroughItsSocket(t *testing.T) {
	// @covers rpc:share/hello setting:agent.shareWindow
	runDir := shortRunDir(t)
	appRoot := testutil.WriteTree(t, map[string]string{"main.go": "package main\n\nfunc retryPolicy() {}\n"})
	otherRoot := testutil.WriteTree(t, map[string]string{"other.go": "package other\n\nfunc retryPolicy() {}\n"})
	client := newTestClient(t)
	hostBatches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "app", Path: appRoot, Name: "app"}, protocol.Root{ID: "other", Path: otherRoot, Name: "other"})
	socket := WindowSocket(runDir, os.Getpid())
	if _, err := os.Stat(socket); err == nil {
		t.Fatal("the window shares before spelunk.agent.shareWindow is on")
	}
	shareWindow(t, client, client.server.Settings().Location, true)
	waitUntil(t, "the window's socket", func() bool { _, err := os.Stat(socket); return err == nil })
	if info, err := os.Stat(runDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("run folder mode = %v, %v; want 0700, so only the user can reach the socket", info.Mode().Perm(), err)
	}

	g := dialGuest(t, socket)
	var hello protocol.ShareHelloResult
	if err := g.call(protocol.MethodShareHello, protocol.Empty{}, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.Reader || len(hello.Repos) != 2 || hello.Repos[0].Root != appRoot || !hello.Repos[0].Writer {
		t.Fatalf("hello = %+v, want this window, writing both roots", hello)
	}
	var result protocol.SearchResult
	params := protocol.SearchStartParams{SearchID: "g1", Text: "retryPolicy", Roots: []string{appRoot}}
	if err := g.call(protocol.MethodSearchStart, params, &result); err != nil {
		t.Fatal(err)
	}
	items := g.items()
	if result.Total != 1 || len(items) != 1 || items[0].Path != "main.go" {
		t.Errorf("guest search = %d results %+v, want main.go only (roots limit it to app)", result.Total, items)
	}
	if got := hostBatches.all(); len(got) != 0 {
		t.Errorf("the editor received the guest's results: %+v", got)
	}
}

func TestAGuestCannotChangeAnything(t *testing.T) {
	runDir := shortRunDir(t)
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r", Path: root, Name: "r"})
	shareWindow(t, client, client.server.Settings().Location, true)
	socket := WindowSocket(runDir, os.Getpid())
	waitUntil(t, "the window's socket", func() bool { _, err := os.Stat(socket); return err == nil })
	g := dialGuest(t, socket)
	for method, params := range map[string]any{
		protocol.MethodIndexRebuild:      protocol.RebuildParams{},
		protocol.MethodReplacePlan:       protocol.ReplacePlanParams{Text: "alpha", Replacement: "beta"},
		protocol.MethodSettingsUpdate:    protocol.SettingsUpdateParams{Settings: DefaultSettings()},
		protocol.MethodWorkspaceSetRoots: protocol.SetRootsParams{},
		protocol.MethodShutdown:          protocol.Empty{},
	} {
		wantRPCCode(t, method, g.call(method, params, nil), rpc.CodeMethodNotFound)
	}
	wantRPCCode(t, "share/open", g.call(protocol.MethodShareOpen, protocol.ShareOpenParams{Path: root}, nil), rpc.CodeMethodNotFound)
}

func TestTurningSharingOffClosesTheSocket(t *testing.T) {
	runDir := shortRunDir(t)
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r", Path: root, Name: "r"})
	location := client.server.Settings().Location
	socket := WindowSocket(runDir, os.Getpid())
	shareWindow(t, client, location, true)
	waitUntil(t, "the window's socket", func() bool { _, err := os.Stat(socket); return err == nil })
	shareWindow(t, client, location, false)
	waitUntil(t, "the socket to go", func() bool { _, err := os.Stat(socket); return os.IsNotExist(err) })
}

// newReader starts a background reader server on its socket.
func newReader(t *testing.T, runDir, location string) {
	t.Helper()
	srv := New(rpc.NewConn(strings.NewReader(""), io.Discard), Options{Reader: true})
	settings := DefaultSettings()
	settings.Location = location
	srv.applySettings(settings)
	listener, err := Listen(context.Background(), runDir, ReaderSocket(runDir))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Accept(ctx, listener)
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
}

func TestTheReaderServesASavedIndexWithoutWriting(t *testing.T) {
	// @covers rpc:share/open
	runDir := shortRunDir(t)
	root := testutil.WriteTree(t, map[string]string{"src/main.go": "package main\n\nfunc retryPolicy() {}\n"})
	window := newTestClient(t)
	window.mustInitialize(t, protocol.Root{ID: "app", Path: root, Name: "app"})
	location := window.server.Settings().Location
	window.server.Close() // the window closes; its index stays saved
	saved := listFiles(t, location)

	newReader(t, runDir, location)
	g := dialGuest(t, ReaderSocket(runDir))
	var status protocol.RepoStatus
	if err := g.call(protocol.MethodShareOpen, protocol.ShareOpenParams{Path: filepath.Join(root, "src")}, &status); err != nil {
		t.Fatal(err)
	}
	if status.Root != root || status.Tree != protocol.IndexStateReady || status.Writer || status.SavedAt == "" {
		t.Fatalf("opened %+v, want the root above src, ready from its saved index, not writing", status)
	}
	var result protocol.SearchResult
	if err := g.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "r1", Text: "sym:retryPolicy", Roots: []string{root}}, &result); err != nil {
		t.Fatal(err)
	}
	if items := g.items(); len(items) != 1 || items[0].Path != "src/main.go" {
		t.Errorf("reader search = %+v, want src/main.go", items)
	}
	time.Sleep(1200 * time.Millisecond) // a commit check, when a writer would build or save
	if got := listFiles(t, location); !equalFiles(got, saved) {
		t.Errorf("index files changed while only the reader had them:\nbefore %v\nafter  %v", saved, got)
	}
}

func TestTheReaderSaysWhenAFolderIsNotIndexed(t *testing.T) {
	runDir := shortRunDir(t)
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	newReader(t, runDir, t.TempDir())
	g := dialGuest(t, ReaderSocket(runDir))
	var status protocol.RepoStatus
	if err := g.call(protocol.MethodShareOpen, protocol.ShareOpenParams{Path: root}, &status); err != nil {
		t.Fatal(err)
	}
	if status.Tree != protocol.IndexStateQueued || !strings.Contains(status.Message, "Not indexed yet") {
		t.Errorf("status of an never-indexed folder = %+v, want queued, not indexed yet", status)
	}
}

func TestAGuestSearchGetsTheAgentTimeLimit(t *testing.T) {
	// @covers setting:agent.timeLimitMs
	srv := &Server{settings: DefaultSettings()}
	srv.settings.TimeLimitMs = 2000
	if got := srv.searchSettings(protocol.SearchStartParams{}, false).TimeLimitMs; got != 2000 {
		t.Errorf("panel search time limit = %d, want the panel's 2000", got)
	}
	if got := srv.searchSettings(protocol.SearchStartParams{}, true).TimeLimitMs; got != DefaultAgentTimeLimitMs {
		t.Errorf("guest search time limit = %d, want %d", got, DefaultAgentTimeLimitMs)
	}
	srv.settings.AgentTimeLimitMs = 45000
	if got := srv.searchSettings(protocol.SearchStartParams{}, true).TimeLimitMs; got != 45000 {
		t.Errorf("guest search time limit = %d, want the setting's 45000", got)
	}
	if got := srv.searchSettings(protocol.SearchStartParams{TimeLimitMs: 900, Count: 7}, true); got.TimeLimitMs != 900 || got.DefaultCount != 7 {
		t.Errorf("search's own limits = %d ms, %d results; want 900 and 7", got.TimeLimitMs, got.DefaultCount)
	}
}

// listFiles lists every file under dir with its size and modification time.
func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, path+" "+info.ModTime().String())
		return nil
	})
	return files
}

func equalFiles(a, b []string) bool {
	return strings.Join(a, "\n") == strings.Join(b, "\n")
}
