package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
)

func connectMCPTestClient(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestMCPRejectsInvalidArgumentsBeforeConnecting(t *testing.T) {
	connected := 0
	client := connectMCPTestClient(t, mcpServerWithConnector(Env{Cwd: t.TempDir()}, func(context.Context, string, Options) (*Session, error) {
		connected++
		return nil, errors.New("unexpected connection")
	}))
	for _, test := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"spelunk_search", map[string]any{}, "query"},
		{"spelunk_search", map[string]any{"query": 42}, "query"},
		{"spelunk_search", map[string]any{"query": nil}, "query"},
		{"spelunk_search", map[string]any{"query": " \t"}, "query must"},
		{"spelunk_search", map[string]any{"query": "kind:"}, "Query error"},
		{"spelunk_search", map[string]any{"query": strings.Repeat("x", 1<<20)}, "at most 1000"},
		{"spelunk_search", map[string]any{"query": "retry", "limit": -1}, "limit must"},
		{"spelunk_search", map[string]any{"query": "retry", "limit": query.MaxResults + 1}, "limit must"},
		{"spelunk_search", map[string]any{"query": "retry", "limit": 1.5}, "limit"},
		{"spelunk_search", map[string]any{"query": "retry", "path": "relative"}, "absolute folder"},
		{"spelunk_search", map[string]any{"query": "retry", "path": "/tmp/\x00"}, "NUL"},
		{"spelunk_preview", map[string]any{}, "ref"},
		{"spelunk_preview", map[string]any{"ref": " "}, "ref must"},
		{"spelunk_preview", map[string]any{"ref": "r", "contextLines": -1}, "contextLines"},
		{"spelunk_preview", map[string]any{"ref": "r", "contextLines": json.Number("9223372036854775808")}, "contextLines"},
		{"spelunk_syntax", map[string]any{"unexpected": true}, "unexpected"},
	} {
		text, isError := callText(t, client, test.tool, test.args)
		if !isError || !strings.Contains(text, test.want) {
			t.Errorf("invalid %s arguments = %q, error=%v; want %q", test.tool, text, isError, test.want)
		}
	}
	if connected != 0 {
		t.Errorf("invalid arguments opened %d backend connections", connected)
	}
	if text, isError := callText(t, client, "spelunk_syntax", nil); isError || !strings.Contains(text, "Examples:") {
		t.Fatalf("valid request after invalid requests = %q, error=%v", text, isError)
	}
}

// mcpBackend sends real search/preview RPC over pipes to the indexed fixture;
// only socket discovery is substituted, so the complete tool-to-engine path runs.
func (f *fixture) mcpBackend(t *testing.T) *Session {
	t.Helper()
	var status protocol.IndexStatusResult
	if err := f.host.Call(t.Context(), protocol.MethodIndexStatus, protocol.Empty{}, &status); err != nil {
		t.Fatal(err)
	}
	session := &Session{conn: f.host, close: func() {}, Repo: status.Repos[0], Live: true, batches: map[string][]protocol.ResultItem{}}
	watchMCPBatches(session)
	return session
}

func watchMCPBatches(session *Session) {
	session.conn.OnNotify(protocol.MethodSearchBatch, func(raw json.RawMessage) {
		var batch protocol.SearchBatchParams
		if json.Unmarshal(raw, &batch) == nil {
			session.mu.Lock()
			session.batches[batch.SearchID] = append(session.batches[batch.SearchID], batch.Items...)
			session.mu.Unlock()
		}
	})
}

func TestMCPSearchHardLimitAndPreview(t *testing.T) {
	// @covers screen:agent-access rpc:search/start rpc:preview/get
	f := newFixture(t, false)
	backend := f.mcpBackend(t)
	result, items, err := backend.Search(t.Context(), "content:retryPolicy count:all", SearchOptions{ResultLimit: 1})
	if err != nil || len(items) != 1 || result.Total != 3 || result.NextCursor != "1" || result.Truncated {
		t.Fatalf("daemon hard cap = %d items, %+v, %v", len(items), result, err)
	}
	for _, limit := range []int{-1, query.MaxResults + 1} {
		_, _, err := backend.Search(t.Context(), "content:retryPolicy", SearchOptions{ResultLimit: limit})
		var rpcErr *rpc.Error
		if !errors.As(err, &rpcErr) || rpcErr.Code != rpc.CodeInvalidParams {
			t.Errorf("invalid resultLimit %d = %v", limit, err)
		}
	}
	client := connectMCPTestClient(t, mcpServerWithConnector(Env{Cwd: f.root}, func(context.Context, string, Options) (*Session, error) { return backend, nil }))
	for _, test := range []struct {
		query string
		limit int
		shown int
	}{
		{"content:retryPolicy count:all", 1, 1},
		{"content:retryPolicy count:1", 2, 1},
		{"content:retryPolicy", 0, 3},
		{"content:no_such_identifier", 1, 0},
	} {
		text, isError := callText(t, client, "spelunk_search", map[string]any{"query": test.query, "limit": test.limit})
		if isError || strings.Count(text, "[ref ") != test.shown {
			t.Fatalf("%s limit %d = %q, error=%v", test.query, test.limit, text, isError)
		}
		if test.shown == 1 && !strings.Contains(text, "3 results (showing 1)") {
			t.Errorf("limited answer hides its completeness: %q", text)
		}
	}
	text, isError := callText(t, client, "spelunk_search", map[string]any{"query": "symbol:retryPolicy"})
	if isError || !strings.Contains(text, "[ref ") {
		t.Fatalf("search = %q", text)
	}
	ref := text[strings.Index(text, "[ref ")+len("[ref ") : strings.LastIndex(text, "]")]
	text, isError = callText(t, client, "spelunk_preview", map[string]any{"ref": ref, "contextLines": 1_000_000})
	if isError || !strings.Contains(text, "func retryPolicy()") {
		t.Errorf("large preview context = %q, error=%v", text, isError)
	}
	text, isError = callText(t, client, "spelunk_preview", map[string]any{"ref": "stale"})
	if !isError || !strings.Contains(text, "spelunk_search again") {
		t.Errorf("stale preview = %q, error=%v", text, isError)
	}
}

func TestMCPMissingIndexExplainsRecovery(t *testing.T) {
	root := t.TempDir()
	client := connectMCPTestClient(t, mcpServerWithConnector(Env{Cwd: root}, func(context.Context, string, Options) (*Session, error) {
		return nil, &NotIndexedError{Root: root}
	}))
	text, isError := callText(t, client, "spelunk_status", nil)
	if !isError || !strings.Contains(text, "spelunk index") || !strings.Contains(text, "use other tools meanwhile") {
		t.Errorf("missing index = %q, error=%v", text, isError)
	}
}

func TestMCPCancellationReachesBackend(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	client := connectMCPTestClient(t, mcpServerWithConnector(Env{Cwd: t.TempDir()}, func(ctx context.Context, _ string, _ Options) (*Session, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "spelunk_search", Arguments: map[string]any{"query": "retry"}})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("tool never reached backend")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation never reached backend")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled tool error = %v", err)
	}
	if _, isError := callText(t, client, "spelunk_syntax", nil); isError {
		t.Error("cancelling one call broke the session")
	}
}

func TestMCPErrorTextKeepsQueryAndStaleGuidance(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{rpc.Errorf(protocol.CodeQueryInvalid, "query has errors: bad query"), "Query error: bad query"},
		{rpc.Errorf(protocol.CodeRefStale, "gone"), "spelunk_search again"},
		{io.ErrUnexpectedEOF, "Spelunk couldn't answer"},
	} {
		if got := mcpErrorText(test.err); !strings.Contains(got, test.want) {
			t.Errorf("error %v = %q, want %q", test.err, got, test.want)
		}
	}
}
