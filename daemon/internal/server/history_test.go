package server

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// checkoutRepo is a repo with three commits, a few days apart.
func checkoutRepo(t *testing.T) string {
	t.Helper()
	now := time.Now()
	return gitRepo(t,
		gitCommit{"Jane Doe", "Add retry policy", now.AddDate(0, 0, -40), map[string]string{"src/retry.py": "max_attempts = 3\n"}},
		gitCommit{"Jason Kim", "Fix flaky checkout test", now.AddDate(0, 0, -10), map[string]string{"tests/checkout_test.py": "timeout = 5\n"}},
		gitCommit{"Jane Doe", "Raise timeout", now.AddDate(0, 0, -3), map[string]string{"src/retry.py": "max_attempts = 3\ntimeout = 30\n"}},
	)
}

func TestHistorySearchPreviewAndOpen(t *testing.T) {
	// @covers screen:author-history
	root := checkoutRepo(t)
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	client.waitForHistory(t)
	batches := collectBatches(client)

	var result protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "author:jane timeout"}, &result); err != nil {
		t.Fatal(err)
	}
	items := batches.of("s1")
	if result.Total != 1 || len(items) != 1 || items[0].Kind != "commit" || items[0].Subject != "Raise timeout" {
		t.Fatalf("author:jane timeout = %d results %+v, want the Raise timeout commit", result.Total, items)
	}
	var preview protocol.Preview
	if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: items[0].Ref, ContextLines: 3}, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Kind != "commit" || len(preview.Hunks) != 1 || preview.Hunks[0].Path != "src/retry.py" {
		t.Errorf("preview = %+v, want the commit's diff of src/retry.py", preview)
	}
	var target protocol.OpenTarget
	if err := client.call(protocol.MethodOpenResolve, protocol.OpenResolveParams{Ref: items[0].Ref}, &target); err != nil {
		t.Fatal(err)
	}
	if target.SHA != items[0].SHA || target.RepoID != "r1" {
		t.Errorf("open target = %+v, want the commit %s", target, items[0].SHA)
	}
}

func TestAuthorAndMessageSuggestionsComeFromHistory(t *testing.T) {
	// @covers screen:author-values screen:value-suggestions
	root := checkoutRepo(t)
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	client.waitForHistory(t)
	var parsed protocol.ParseResult
	if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: "author:ja", Cursor: 9}, &parsed); err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, c := range parsed.Completions {
		labels = append(labels, c.Label)
	}
	if !reflect.DeepEqual(labels, []string{"Jane Doe", "Jason Kim"}) || !strings.HasPrefix(parsed.Completions[0].Detail, "2 commits · payments-api · last 3 days ago") {
		t.Errorf("author:ja suggestions = %+v, want Jane (2 commits) then Jason", parsed.Completions)
	}
	if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: "msg:fl", Cursor: 6}, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Completions) == 0 || parsed.Completions[0].Label != "flaky" {
		t.Errorf("msg:fl suggestions = %+v, want flaky", parsed.Completions)
	}
}

func TestSinceOnFilesUsesCommitDatesAndUncommittedEdits(t *testing.T) {
	// @covers screen:since-on-files
	root := checkoutRepo(t)
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	client.waitForHistory(t)
	files := func(text string) []string {
		t.Helper()
		batches := collectBatches(client)
		if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: text, Text: text}, nil); err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, item := range batches.of(text) {
			if item.Kind == "file" {
				paths = append(paths, item.Path)
			}
		}
		return paths
	}
	// Every file was written seconds ago, but only src/retry.py changed in a commit this week.
	if got := files("type:file since:1w f:."); !reflect.DeepEqual(got, []string{"src/retry.py"}) {
		t.Errorf("since:1w = %q, want only src/retry.py (committed 3 days ago)", got)
	}
	mustWriteFile(t, filepath.Join(root, "tests", "checkout_test.py"), "timeout = 9\n")
	client.server.index.FilesChanged([]protocol.FileChange{{Path: filepath.Join(root, "tests", "checkout_test.py"), Type: "changed"}})
	if got := files("type:file since:1w f:."); !reflect.DeepEqual(got, []string{"src/retry.py", "tests/checkout_test.py"}) {
		t.Errorf("since:1w after an uncommitted edit = %q, want both files", got)
	}
}

func TestSavedFilesAreSearchableWithoutARebuild(t *testing.T) {
	// @covers rpc:workspace/didChangeFiles
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.py"), "alpha = 1\n")
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "app"})
	lines := func(text string) []string {
		t.Helper()
		batches := collectBatches(client)
		if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: text, Text: text}, nil); err != nil {
			t.Fatal(err)
		}
		var found []string
		for _, item := range batches.of(text) {
			if item.Kind == "line" {
				found = append(found, item.Path+": "+item.Text)
			}
		}
		return found
	}
	mustWriteFile(t, filepath.Join(root, "a.py"), "beta = 2\n")
	mustWriteFile(t, filepath.Join(root, "b.py"), "beta = 3\n")
	if err := client.conn.Notify(protocol.MethodWorkspaceDidChangeFiles, protocol.DidChangeFilesParams{Changes: []protocol.FileChange{
		{Path: filepath.Join(root, "a.py"), Type: "changed"}, {Path: filepath.Join(root, "b.py"), Type: "created"},
	}}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the saved files to be searchable", func() bool { return len(lines("type:code beta")) == 2 })
	if got := lines("type:code alpha"); len(got) != 0 {
		t.Errorf("alpha after it was replaced = %q, want nothing", got)
	}
	if err := os.Remove(filepath.Join(root, "b.py")); err != nil {
		t.Fatal(err)
	}
	client.server.index.FilesChanged([]protocol.FileChange{{Path: filepath.Join(root, "b.py"), Type: "deleted"}})
	if got := lines("type:code beta"); !reflect.DeepEqual(got, []string{"a.py: beta = 2"}) {
		t.Errorf("beta after b.py was deleted = %q, want only a.py", got)
	}
}

func TestAFolderOutsideGitSaysHistoryIsOff(t *testing.T) {
	// @covers failure:no-git
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: t.TempDir(), Name: "plain"})
	client.waitForHistory(t)
	status := client.server.index.Status()[0]
	if status.History != protocol.IndexStateOff || !strings.Contains(status.Message, "Not a Git repository") {
		t.Errorf("status = %+v, want history off with a reason", status)
	}
}

// commitSearch runs a query and returns the subjects of the commits found,
// and the result summary.
func commitSearch(t *testing.T, client *testClient, text string) ([]string, protocol.SearchResult) {
	t.Helper()
	batches := collectBatches(client)
	var result protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: text, Text: text}, &result); err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, item := range batches.of(text) {
		subjects = append(subjects, item.RepoID+": "+item.Subject)
	}
	return subjects, result
}

func TestPlainWordsMatchCommitMessages(t *testing.T) {
	// @covers screen:words-in-messages
	root := checkoutRepo(t)
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	client.waitForHistory(t)
	batches := collectBatches(client)

	// Only Jason's message says flaky; his diff doesn't.
	var result protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: "s1", Text: "author:jason flaky"}, &result); err != nil {
		t.Fatal(err)
	}
	items := batches.of("s1")
	if len(items) != 1 || items[0].Subject != "Fix flaky checkout test" || !items[0].InMessage || items[0].DiffHits != 0 {
		t.Fatalf("author:jason flaky = %+v, want Fix flaky checkout test, matched in its message alone", items)
	}
	var preview protocol.Preview
	if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: items[0].Ref, ContextLines: 3}, &preview); err != nil {
		t.Fatal(err)
	}
	if want := []protocol.Range{{Start: 4, End: 9}}; !reflect.DeepEqual(preview.SubjectHits, want) {
		t.Errorf("preview marks %v in the subject, want %v (flaky)", preview.SubjectHits, want)
	}
}

func TestBooleanHistoryQueriesCountWhatTheyHide(t *testing.T) {
	// @covers screen:boolean-history
	root := checkoutRepo(t)
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	client.waitForHistory(t)

	got, result := commitSearch(t, client, "type:commit (timeout OR attempts) -f:tests/ since:2w")
	if !reflect.DeepEqual(got, []string{"r1: Raise timeout"}) {
		t.Errorf("commits = %q, want only Raise timeout: the test fix is excluded and Add retry policy is too old", got)
	}
	var notes []string
	for _, note := range result.Hidden {
		notes = append(notes, fmt.Sprintf("%d %s hidden by %s", note.Count, note.Unit, note.Filter))
	}
	if want := []string{"1 commits hidden by -f:tests/", "1 commits hidden by since:2w"}; !reflect.DeepEqual(notes, want) {
		t.Errorf("hidden = %q, want %q", notes, want)
	}
	got, _ = commitSearch(t, client, "type:commit (timeout OR attempts)")
	if len(got) != 3 {
		t.Errorf("timeout OR attempts = %q, want all three commits", got)
	}
}

func TestMessagePhraseRepoAndCount(t *testing.T) {
	// @covers screen:message-repo-count
	checkout := checkoutRepo(t)
	now := time.Now()
	web := gitRepo(t,
		gitCommit{"Ana Lima", "Fix flaky checkout button", now.AddDate(0, 0, -2), map[string]string{"app.ts": "click()\n"}},
		gitCommit{"Ana Lima", "Fix flaky checkout total", now.AddDate(0, 0, -1), map[string]string{"app.ts": "click()\ntotal()\n"}},
	)
	client := newTestClient(t)
	client.mustInitialize(t,
		protocol.Root{ID: "api", Path: checkout, Name: "payments-api"},
		protocol.Root{ID: "web", Path: web, Name: "web-checkout"},
	)
	client.waitForHistory(t)

	got, _ := commitSearch(t, client, `msg:"flaky checkout"`)
	if !reflect.DeepEqual(got, []string{"web: Fix flaky checkout total", "web: Fix flaky checkout button", "api: Fix flaky checkout test"}) {
		t.Errorf(`msg:"flaky checkout" = %q, want the three commits, newest first`, got)
	}
	got, result := commitSearch(t, client, `msg:"flaky checkout" repo:web-checkout count:1`)
	if !reflect.DeepEqual(got, []string{"web: Fix flaky checkout total"}) || result.Total != 2 || result.NextCursor == "" {
		t.Errorf("with repo: and count:1 = %q (total %d, next %q), want the newest of web-checkout's 2 and a next page", got, result.Total, result.NextCursor)
	}
}
