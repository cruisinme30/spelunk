package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

func TestMCPAndCLISummariesDistinguishIncompleteCountsFromOutputLimits(t *testing.T) {
	session := &Session{Repo: protocol.RepoStatus{Name: "repo", Root: "/repo"}}
	results := []Result{{Type: "line", Path: "a.go", Line: 1, Text: "match", Ref: "r"}}
	now := time.Unix(0, 0)
	for _, test := range []struct {
		name      string
		total     int
		truncated bool
		want      string
	}{
		{"result ceiling", 50_000, true, "at least 50000 results (showing 1) · 7 ms · live, from VS Code · search incomplete; there may be more results"},
		{"other incomplete scan", 7, true, "at least 7 results (showing 1) · 7 ms · live, from VS Code · search incomplete; there may be more results"},
		{"complete count with limited output", 3, false, "3 results (showing 1) · 7 ms · live, from VS Code"},
	} {
		t.Run(test.name, func(t *testing.T) {
			summary := &Summary{Total: test.total, Shown: 1, Ms: 7, Source: "vscode", Truncated: test.truncated}
			if got := summaryLine(summary, now); got != test.want {
				t.Errorf("CLI summary = %q, want %q", got, test.want)
			}
			if got := searchText(session, results, summary, now); !strings.HasPrefix(got, "Repo repo (/repo): "+test.want+"\n") {
				t.Errorf("MCP summary = %q, want shared summary %q", got, test.want)
			}
		})
	}
}

func TestMCPIncompleteSearchWithNoMatchesDoesNotSuggestWidening(t *testing.T) {
	session := &Session{Repo: protocol.RepoStatus{Name: "repo", Root: "/repo"}}
	text := searchText(session, nil, &Summary{Truncated: true}, time.Unix(0, 0))
	if !strings.Contains(text, "No results found before the search stopped. Narrow the query and retry.") || strings.Contains(text, "widen") {
		t.Errorf("incomplete empty search = %q", text)
	}
}
