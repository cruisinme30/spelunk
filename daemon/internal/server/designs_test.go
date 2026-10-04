package server

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// fixtureRoots are the three repos of testdata/workspace, whose contents
// match the design mockups (docs/dev/mocks.md).
func fixtureRoots(t *testing.T) []protocol.Root {
	t.Helper()
	workspace, err := filepath.Abs("../../../testdata/workspace")
	if err != nil {
		t.Fatal(err)
	}
	var roots []protocol.Root
	for _, name := range []string{"payments-api", "web-checkout", "shared-libs"} {
		roots = append(roots, protocol.Root{ID: name, Path: filepath.Join(workspace, name), Name: name})
	}
	return roots
}

// resultLines renders results as "file repo/path" or "repo/path:line".
func resultLines(items []protocol.ResultItem) []string {
	lines := []string{}
	for _, item := range items {
		if item.Kind == "file" {
			lines = append(lines, "file "+item.RepoID+"/"+item.Path)
		} else {
			lines = append(lines, fmt.Sprintf("%s/%s:%d", item.RepoID, item.Path, item.Line))
		}
	}
	return lines
}

// The working-tree queries from the design mockups (docs/dev/mocks.md)
// return exactly the results the designs show.
func TestDesignQueriesFindWhatTheDesignsShow(t *testing.T) {
	tests := []struct {
		screen, query string
		want          []string
		hidden        []string // reason:filter:count
	}{
		{
			// @covers screen:plain-text-search
			screen: "plain-text-search", query: "retry_policy",
			want: []string{
				"file payments-api/src/payments/retry_policy.py",
				"file payments-api/tests/payments/retry_policy_test.py",
				"file shared-libs/docs/retry_policy.md",
				"payments-api/src/payments/client.py:42",
				"payments-api/src/payments/client.py:46",
				"payments-api/src/payments/client.py:50",
				"web-checkout/src/api/checkout.ts:17",
				"shared-libs/http/config.yaml:9",
			},
		},
		{
			// @covers screen:path-scoped-search
			screen: "path-scoped-search", query: `f:.*test\.py$ timeout`,
			want: []string{
				"payments-api/tests/payments/client_test.py:18",
				"payments-api/tests/payments/client_test.py:21",
				"payments-api/tests/payments/client_test.py:23",
				"payments-api/tests/payments/client_test.py:29",
				"payments-api/tests/payments/client_test.py:30",
				"payments-api/tests/payments/retry_policy_test.py:12",
				"payments-api/tests/payments/retry_policy_test.py:38",
				"web-checkout/e2e/checkout_test.py:55",
				"shared-libs/http/tests/session_test.py:8",
				"shared-libs/http/tests/session_test.py:19",
			},
		},
		{
			// @covers screen:case-regex-language
			screen: "case-regex-language", query: "case:yes /Retry(Policy|Config)/ lang:python",
			want: []string{
				"payments-api/src/payments/client.py:2",
				"payments-api/src/payments/client.py:42",
				"payments-api/src/payments/retry_policy.py:5",
				"payments-api/src/payments/retry_policy.py:12",
				"payments-api/src/payments/retry_policy.py:40",
				"payments-api/tests/payments/retry_policy_test.py:12",
				"shared-libs/http/config.py:21",
			},
			hidden: []string{"case:case:yes:1"},
		},
		{
			// @covers screen:file-names-only
			screen: "file-names-only", query: "type:file lang:python retry",
			want: []string{
				"file payments-api/scripts/retry_failed_webhooks.py",
				"file payments-api/src/payments/retry_policy.py",
				"file payments-api/tests/payments/retry_policy_test.py",
				"file shared-libs/http/retry.py",
				"file shared-libs/http/tests/retry_test.py",
			},
			hidden: []string{"type:type:file:17"},
		},
	}
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, fixtureRoots(t)...)
	for i, tt := range tests {
		t.Run(tt.screen, func(t *testing.T) {
			var result protocol.SearchResult
			searchID := fmt.Sprintf("s%d", i)
			if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: searchID, Text: tt.query}, &result); err != nil {
				t.Fatal(err)
			}
			if got := resultLines(batches.of(searchID)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s:\n got %q\nwant %q", tt.query, got, tt.want)
			}
			if result.Total != len(tt.want) {
				t.Errorf("total = %d, want %d", result.Total, len(tt.want))
			}
			hidden := []string{}
			for _, note := range result.Hidden {
				hidden = append(hidden, fmt.Sprintf("%s:%s:%d", note.Reason, note.Filter, note.Count))
			}
			if tt.hidden == nil {
				tt.hidden = []string{}
			}
			if !reflect.DeepEqual(hidden, tt.hidden) {
				t.Errorf("hidden = %q, want %q", hidden, tt.hidden)
			}
		})
	}
}

// The "N code matches hidden by type:file" count is exactly what the same
// query returns as code.
func TestTypeFileHidesExactlyWhatTypeCodeFinds(t *testing.T) {
	client := newTestClient(t)
	client.mustInitialize(t, fixtureRoots(t)...)
	var files, code protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "f", Text: "type:file lang:python retry"}, &files); err != nil {
		t.Fatal(err)
	}
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "c", Text: "type:code lang:python retry"}, &code); err != nil {
		t.Fatal(err)
	}
	if len(files.Hidden) != 1 || files.Hidden[0].Count != code.Total || code.Total == 0 {
		t.Errorf("type:file hidden = %+v, want one note counting the %d code matches of type:code", files.Hidden, code.Total)
	}
}
