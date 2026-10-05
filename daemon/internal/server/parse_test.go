package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

func TestParseReturnsTheParsedQueryAndCompletions(t *testing.T) {
	// @covers rpc:query/parse
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: t.TempDir(), Name: "web-checkout"})

	var result protocol.ParseResult
	if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: "repo:web timeout s", Cursor: 18}, &result); err != nil {
		t.Fatal(err)
	}
	root := result.Query.Root
	if root == nil || root.Kind != "and" || len(root.Children) != 3 {
		t.Fatalf("parsed root = %+v, want and of 3", root)
	}
	if resolved := root.Children[0].Resolved; resolved == nil || resolved.Label != "web-checkout" {
		t.Errorf("repo:web resolved = %+v, want web-checkout (from the open roots)", resolved)
	}
	if len(result.Completions) != 2 || result.Completions[0].Label != "symbol:" {
		t.Errorf("completions = %+v, want symbol: (s is its short name) and since:", result.Completions)
	}
}

func TestParseReportsDiagnosticsWithoutFailing(t *testing.T) {
	client := newTestClient(t)
	client.mustInitialize(t)
	var result protocol.ParseResult
	if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: "sinse:6m", Cursor: 8}, &result); err != nil {
		t.Fatalf("query/parse with a bad query: %v, want a result with diagnostics", err)
	}
	if len(result.Query.Diagnostics) != 1 || result.Query.Diagnostics[0].Code != "unknown_operator" {
		t.Errorf("diagnostics = %+v, want one unknown_operator", result.Query.Diagnostics)
	}
}

func TestParseIncludesPlannerWarnings(t *testing.T) {
	client := newTestClient(t)
	client.mustInitialize(t)
	var result protocol.ParseResult
	if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: "type:commit /a.b/", Cursor: 17}, &result); err != nil {
		t.Fatal(err)
	}
	if d := result.Query.Diagnostics; len(d) != 1 || d[0].Code != "history_full_scan" || d[0].Severity != protocol.SeverityWarning {
		t.Errorf("diagnostics = %+v, want one history_full_scan warning", d)
	}
}

func TestValueSuggestionsDescribeTheIndexedWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o750); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "src", "retry.py"), "timeout = 3\n")
	mustWriteFile(t, filepath.Join(dir, "src", "client.py"), "retry()\n")
	mustWriteFile(t, filepath.Join(dir, "README.md"), "# web\n")
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "web"})

	suggest := func(text string) []protocol.Completion {
		t.Helper()
		var result protocol.ParseResult
		if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: text, Cursor: len(text)}, &result); err != nil {
			t.Fatal(err)
		}
		return result.Completions
	}
	if got := suggest("f:"); len(got) < 2 || got[0].Label != "*.py" || got[0].Context != "2 files · web" || got[0].Section != "File types" {
		t.Errorf("f: suggestions = %+v, want *.py (2 files · web) first, under File types", got)
	}
	if got := suggest("since:"); len(got) == 0 || got[0].Label != "today" || got[0].Note != "3 files changed" {
		t.Errorf("since: suggestions = %+v, want today first, with the 3 files just written", got)
	}
	if got := suggest("repo:"); len(got) != 1 || got[0].Note != "Index ready" || got[0].Detail != dir || got[0].Context != "3 files" {
		t.Errorf("repo: suggestions = %+v, want web with its path, 3 files and Index ready", got)
	}
}
