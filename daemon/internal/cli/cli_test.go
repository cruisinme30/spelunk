package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/server"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// TestMain lets the test binary be the background reader the command
// starts: Connect runs os.Executable() reader --config ...
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "reader" && os.Args[2] == "--config" {
		os.Exit(RunReader(context.Background(), strings.Join(os.Args[3:], " "), os.Stderr))
	}
	testutil.IsolateGit()
	os.Exit(m.Run())
}

// fixture is a repo, an index location and a run folder for one test.
type fixture struct {
	root, location, runDir string
	window                 *server.Server
	host                   *rpc.Conn
}

// newFixture indexes a small repo with a window daemon that shares it
// when share is set.
func newFixture(t *testing.T, share bool) *fixture {
	t.Helper()
	runDir, err := os.MkdirTemp("/tmp", "spc") //nolint:usetesting // t.TempDir's path is too long for a socket
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runDir) })
	t.Setenv("SPELUNK_RUN_DIR", runDir)
	t.Setenv("SPELUNK_READER_IDLE", "8s") // readers the tests start exit soon after
	f := &fixture{
		root: testutil.WriteTree(t, map[string]string{
			"src/retry.go":      "package src\n\n// retryPolicy decides how often to retry.\nfunc retryPolicy() int { return 3 }\n",
			"src/retry_test.go": "package src\n\nfunc testRetry() { retryPolicy() }\n",
		}),
		location: t.TempDir(), runDir: runDir,
	}
	serverIn, hostOut := io.Pipe()
	hostIn, serverOut := io.Pipe()
	serverConn := rpc.NewConn(serverIn, serverOut)
	f.host = rpc.NewConn(hostIn, hostOut)
	f.window = server.New(serverConn, server.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = serverConn.Serve(ctx) }()
	go func() { _ = f.host.Serve(ctx) }()
	t.Cleanup(func() {
		f.window.Close()
		cancel()
		_ = hostOut.Close()
		_ = serverOut.Close()
	})
	settings := server.DefaultSettings()
	settings.Location, settings.ShareWindow = f.location, share
	params := protocol.InitializeParams{Protocol: protocol.Version, Roots: []protocol.Root{{ID: "app", Path: f.root, Name: "app"}}, Settings: settings}
	if err := f.host.Call(ctx, protocol.MethodInitialize, params, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var status protocol.IndexStatusResult
		_ = f.host.Call(ctx, protocol.MethodIndexStatus, protocol.Empty{}, &status)
		if len(status.Repos) == 1 && status.Repos[0].Tree == protocol.IndexStateReady && status.Repos[0].SavedAt != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never indexed: %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return f
}

// run runs the command in cwd and returns its exit code and output.
func (f *fixture) run(t *testing.T, cwd string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	settings := server.DefaultSettings()
	settings.Location = f.location
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	code = Main(ctx, Env{
		Args: args, Stdout: &out, Stderr: &errOut, Cwd: cwd,
		Options: Options{RunDir: f.runDir, Config: Config{Settings: settings}},
	})
	return code, out.String(), errOut.String()
}

func TestSearchAsksTheWindowThatHasTheFolderOpen(t *testing.T) {
	f := newFixture(t, true)
	code, out, errOut := f.run(t, filepath.Join(f.root, "src"), "search", "retryPolicy", "-file:_test")
	if code != exitFound {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if want := "retry.go:3:// retryPolicy decides how often to retry.\nretry.go:4:func retryPolicy() int { return 3 }\n"; out != want {
		t.Errorf("stdout =\n%s\nwant paths relative to the current folder, test files left out:\n%s", out, want)
	}
	if !strings.Contains(errOut, "2 results") || !strings.Contains(errOut, "live, from VS Code") {
		t.Errorf("summary = %q, want 2 results, live from VS Code", errOut)
	}
	if entries, _ := filepath.Glob(filepath.Join(f.runDir, "reader.*")); len(entries) != 0 {
		t.Errorf("a reader started although a window had the folder: %v", entries)
	}
}

func TestSearchFallsBackToTheReaderWhenNoWindowShares(t *testing.T) {
	f := newFixture(t, false)
	f.window.Close() // the window closes; its index stays saved
	code, out, errOut := f.run(t, f.root, "search", "symbol:retryPolicy")
	if code != exitFound {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if out != "src/retry.go:4:function retryPolicy\n" {
		t.Errorf("stdout = %q, want the definition", out)
	}
	if !strings.Contains(errOut, "Starting Spelunk's background reader") || !strings.Contains(errOut, "index saved just now") {
		t.Errorf("stderr = %q, want the reader starting, then a summary from the saved index", errOut)
	}

	// A file edited since the save is searched as it is now.
	later := time.Now().Add(2 * time.Second)
	path := filepath.Join(f.root, "src", "retry.go")
	if err := os.WriteFile(path, []byte("package src\n\nfunc backoffPolicy() int { return 3 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5100 * time.Millisecond) // past the reader's sweep interval
	code, out, errOut = f.run(t, f.root, "search", "symbol:backoffPolicy")
	if code != exitFound || out != "src/retry.go:3:function backoffPolicy\n" || !strings.Contains(errOut, "1 changed file re-read") || strings.Contains(errOut, "Starting") {
		t.Errorf("after an edit: exit %d, stdout %q, stderr %q; want the new definition from the running reader, one file re-read", code, out, errOut)
	}
}

func TestSearchExitCodes(t *testing.T) {
	f := newFixture(t, true)
	if code, _, _ := f.run(t, f.root, "search", `"no such phrase anywhere"`); code != exitNothing {
		t.Errorf("no results: exit %d, want %d", code, exitNothing)
	}
	code, out, errOut := f.run(t, f.root, "search", "kind:")
	if code != exitError || out != "" || !strings.Contains(errOut, "spelunk: kind: needs a value") || strings.Contains(errOut, "rpc error") {
		t.Errorf("bad query: exit %d, stdout %q, stderr %q; want 2 and the parser's message alone", code, out, errOut)
	}
	if code, _, errOut := f.run(t, f.root, "search"); code != exitError || !strings.Contains(errOut, "needs a query") {
		t.Errorf("no query: exit %d, stderr %q", code, errOut)
	}
	if code, _, _ := f.run(t, f.root, "fly"); code != exitError {
		t.Errorf("unknown command: exit %d, want 2", code)
	}
}

func TestSearchOfAFolderNothingIndexed(t *testing.T) {
	f := newFixture(t, true)
	elsewhere := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	code, out, errOut := f.run(t, elsewhere, "search", "alpha")
	if code != exitError || out != "" || !strings.Contains(errOut, "Not indexed yet. Open this folder in VS Code") {
		t.Errorf("exit %d, stdout %q, stderr %q; want 2 and a pointer to VS Code", code, out, errOut)
	}
}

func TestSearchJSONAndVimgrep(t *testing.T) {
	f := newFixture(t, true)
	_, out, _ := f.run(t, f.root, "search", "--json", "-n", "1", "retryPolicy")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("--json printed %d lines, want a result and a summary: %q", len(lines), out)
	}
	var result Result
	var summary Summary
	if err := json.Unmarshal([]byte(lines[0]), &result); err != nil || result.Type != "line" || result.Path == "" || result.Line == 0 || result.Ref != "" {
		t.Errorf("result = %+v (%v), want a line with a path and line number and no internal ref", result, err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &summary); err != nil || summary.Type != "summary" || summary.Source != "vscode" || summary.Shown != 1 {
		t.Errorf("summary = %+v (%v)", summary, err)
	}
	_, out, _ = f.run(t, f.root, "search", "--vimgrep", "--in", "src", `"func retryPolicy"`)
	if out != "src/retry.go:4:1:func retryPolicy() int { return 3 }\n" {
		t.Errorf("--vimgrep = %q, want path:line:column:text", out)
	}
}

func TestStatusSaysWhoAnswers(t *testing.T) {
	f := newFixture(t, true)
	code, out, _ := f.run(t, f.root, "status")
	if code != exitFound || !strings.Contains(out, "Answered:  by VS Code, live") || !strings.Contains(out, "Files:     ready") {
		t.Errorf("status = exit %d\n%s", code, out)
	}
}

func TestHelp(t *testing.T) {
	f := &fixture{}
	if code, out, _ := f.run(t, "/", "help"); code != exitFound || !strings.Contains(out, "spelunk search [options] QUERY") {
		t.Errorf("help: exit %d\n%s", code, out)
	}
	if code, out, _ := f.run(t, "/", "help", "query"); code != exitFound || !strings.Contains(out, "Spelunk query language") {
		t.Errorf("help query: exit %d\n%s", code, out)
	}
}

func TestParseSearchArgsLeavesQueryWordsAlone(t *testing.T) {
	for _, tt := range []struct {
		args  []string
		query string
		count int
	}{
		{[]string{"retry", "-file:_test"}, "retry -file:_test", defaultCount},
		{[]string{"-test", "retry"}, "-test retry", defaultCount},
		{[]string{"-n", "5", "--", "-n", "retry"}, "-n retry", 5},
		{[]string{"--count", "7", `"read timeout"`}, `"read timeout"`, 7},
	} {
		got, err := parseSearchArgs(tt.args, "/r")
		if err != nil || got.query != tt.query || got.count != tt.count {
			t.Errorf("parseSearchArgs(%q) = %q, %d, %v; want %q, %d", tt.args, got.query, got.count, err, tt.query, tt.count)
		}
	}
	if _, err := parseSearchArgs([]string{"-n", "zero", "x"}, "/r"); err == nil {
		t.Error("-n zero: no error")
	}
}

func TestByteColumnCountsUTF16Units(t *testing.T) {
	if got := byteColumn("café au lait", 5); got != 7 {
		t.Errorf("byteColumn = %d, want 7 (é is two bytes, one UTF-16 unit)", got)
	}
}

// mcpSession connects an MCP client to the server for cwd.
func (f *fixture) mcpSession(t *testing.T, cwd string) *mcp.ClientSession {
	t.Helper()
	settings := server.DefaultSettings()
	settings.Location = f.location
	env := Env{Cwd: cwd, Stderr: io.Discard, Now: time.Now, Options: Options{RunDir: f.runDir, Config: Config{Settings: settings}}}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := NewMCPServer(env).Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// callText calls a tool and returns its text and whether it is an error.
func callText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String(), result.IsError
}

func TestAgentsSearchThroughMCP(t *testing.T) {
	// @covers screen:agent-access
	f := newFixture(t, true)
	session := f.mcpSession(t, f.root)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s isn't marked read-only", tool.Name)
		}
	}
	if strings.Join(names, ",") != "spelunk_preview,spelunk_search,spelunk_status,spelunk_syntax" {
		t.Errorf("tools = %v", names)
	}

	text, isError := callText(t, session, "spelunk_search", map[string]any{"query": "symbol:retryPolicy"})
	if isError || !strings.Contains(text, "src/retry.go:4:function retryPolicy  [ref ") || !strings.Contains(text, "live, from VS Code") {
		t.Fatalf("spelunk_search = %q", text)
	}
	ref := text[strings.Index(text, "[ref ")+len("[ref ") : strings.LastIndex(text, "]")]
	text, isError = callText(t, session, "spelunk_preview", map[string]any{"ref": ref, "contextLines": 1})
	if isError || !strings.Contains(text, ">     4  func retryPolicy() int { return 3 }") {
		t.Errorf("spelunk_preview = %q", text)
	}
	text, isError = callText(t, session, "spelunk_search", map[string]any{"query": "kind:"})
	if !isError || !strings.Contains(text, "Query error: kind: needs a value") {
		t.Errorf("bad query = %q, error %v; want the parser's message as a tool error", text, isError)
	}
	text, _ = callText(t, session, "spelunk_syntax", nil)
	if !strings.Contains(text, "symbol:") || !strings.Contains(text, "Examples:") {
		t.Errorf("spelunk_syntax = %q", text)
	}
	text, _ = callText(t, session, "spelunk_status", nil)
	if !strings.Contains(text, "A VS Code window answers, live.") {
		t.Errorf("spelunk_status = %q", text)
	}
}

func TestClaudeAddArgsUseClaudeCodesOwnCommand(t *testing.T) {
	got := strings.Join(claudeAddArgs("/home/me/.local/bin/spelunk"), " ")
	if got != "mcp add --scope user spelunk -- /home/me/.local/bin/spelunk mcp" {
		t.Errorf("claude %s", got)
	}
}
