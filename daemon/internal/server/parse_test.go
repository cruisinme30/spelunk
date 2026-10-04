package server

import (
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// @covers rpc:query/parse
func TestParseReturnsTheParsedQueryAndCompletions(t *testing.T) {
	client := newTestClient(t, Options{})
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
	if len(result.Completions) != 2 || result.Completions[0].Label != "since:" {
		t.Errorf("completions = %+v, want since: and sym:", result.Completions)
	}
}

func TestParseReportsDiagnosticsWithoutFailing(t *testing.T) {
	client := newTestClient(t, Options{})
	client.mustInitialize(t)
	var result protocol.ParseResult
	if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: "sinse:6m", Cursor: 8}, &result); err != nil {
		t.Fatalf("query/parse with a bad query: %v, want a result with diagnostics", err)
	}
	if len(result.Query.Diagnostics) != 1 || result.Query.Diagnostics[0].Code != "unknown_operator" {
		t.Errorf("diagnostics = %+v, want one unknown_operator", result.Query.Diagnostics)
	}
}
