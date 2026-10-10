package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// since: and until: suggestions count the files the search would keep:
// in Git a file changed when it was last committed, or now when it has
// uncommitted changes, not when it was last written on disk. A fresh
// clone's files are all written at once, so every window said they had
// all changed.
func TestSinceAndUntilSuggestionsCountWhatTheSearchKeeps(t *testing.T) {
	// @covers op:since op:until
	root := gitRepo(t, gitCommit{
		author: "Ada", subject: "first", at: time.Now().AddDate(0, -1, 0),
		files: map[string]string{"old.py": "x = 1\n", "edited.py": "y = 1\n"},
	})
	mustWriteFile(t, filepath.Join(root, "edited.py"), "y = 2\n")
	mustWriteFile(t, filepath.Join(root, "new.py"), "z = 1\n")
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "r"})
	client.waitForHistory(t)

	note := func(text string) string {
		t.Helper()
		var parsed protocol.ParseResult
		if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: text, Cursor: len(text)}, &parsed); err != nil {
			t.Fatal(err)
		}
		for _, completion := range parsed.Completions {
			if completion.Label == "2w" {
				return completion.Note
			}
		}
		t.Fatalf("no 2w suggestion for %q in %+v", text, parsed.Completions)
		return ""
	}
	total := func(text string) int {
		t.Helper()
		var result protocol.SearchResult
		if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: text, Text: text}, &result); err != nil {
			t.Fatal(err)
		}
		return result.Total
	}
	if got, files := note("since:"), total("type:file since:2w /py/"); got != "2 files changed" || files != 2 {
		t.Errorf("since:2w suggestion says %q and the search finds %d files; want 2 files changed, and 2", got, files)
	}
	if got, files := note("until:"), total("type:file until:2w /py/"); got != "1 file not changed since" || files != 1 {
		t.Errorf("until:2w suggestion says %q and the search finds %d files; want 1 file not changed since, and 1", got, files)
	}
}
