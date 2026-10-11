package server

import (
	"slices"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// The mock's workspace in small: retry in a comment, a string and code, a
// docstring, and notes in plain text.
func inWorkspace(t *testing.T) *testClient {
	t.Helper()
	root := testutil.WriteTree(t, map[string]string{
		"api/retry.ts":     "retryCount++; // retry again\nthrow new Error(\"retry budget spent\");\n",
		"api/client.py":    "def charge():\n    \"\"\"Charge, retrying on errors.\"\"\"\n    return retry(post)\n",
		"docs/retries.txt": "We retry after 429s.\n",
	})
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r", Path: root, Name: "web-checkout"})
	return client
}

func TestInSearchesCommentsStringsOrCodeEndToEnd(t *testing.T) {
	// @covers op:in screen:comments-strings rpc:search/start
	client := inWorkspace(t)
	tests := map[string][]string{
		"retry in:comments":  {"r/api/client.py:2", "r/api/retry.ts:1", "r/docs/retries.txt:1"},
		"retry in:strings":   {"r/api/retry.ts:2", "r/docs/retries.txt:1"},
		"retry in:code":      {"r/api/client.py:3", "r/api/retry.ts:1", "r/docs/retries.txt:1"},
		"retry -in:comments": {"r/api/client.py:3", "r/api/retry.ts:1", "r/api/retry.ts:2", "r/docs/retries.txt:1"},
	}
	for text, want := range tests {
		got, _ := paths(t, client, text)
		if !slices.Equal(sorted(got), want) {
			t.Errorf("%s = %q, want %q", text, sorted(got), want)
		}
	}
	_, result := paths(t, client, "retry in:comments")
	var note *protocol.HiddenNote
	for i := range result.Hidden {
		if result.Hidden[i].Reason == "in" {
			note = &result.Hidden[i]
		}
	}
	if note == nil || note.Count != 2 || note.Filter != "in:comments" {
		t.Fatalf("hidden = %+v, want 2 matches hidden by in:comments", result.Hidden)
	}
	if everywhere := query.ApplyFix("retry in:comments", note.Undo); everywhere != "retry" {
		t.Errorf("Show them makes %q", everywhere)
	}
}
