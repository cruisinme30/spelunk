package history

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// paymentsHistory builds a small repo whose history the tests search:
//
//	day 40  Jane (old email)  Add retry policy         src/retry.py
//	day 10  Jason             Fix flaky checkout test  tests/checkout_test.py
//	day  3  Jane              Raise timeout            src/retry.py, vendor/lib/retry.js
//	day  1  Marta             Vendor update            vendor/lib/retry.js
func paymentsHistory(t *testing.T) (*testRepo, map[string]string) {
	t.Helper()
	r := newTestRepo(t)
	shas := map[string]string{}
	shas["add"] = r.commit("Jane Doe <jdoe@old.example>", "Add retry policy", day(40), map[string]string{
		".mailmap":     "Jane Doe <jane@payments.example> <jdoe@old.example>\n",
		"src/retry.py": "class RetryPolicy:\n    max_attempts = 3\n",
	})
	shas["flaky"] = r.commit("Jason Kim <jason@checkout.example>", "Fix flaky checkout test", day(10), map[string]string{
		"tests/checkout_test.py": "def test_checkout():\n    timeout = 5\n",
	})
	shas["raise"] = r.commit("Jane Doe <jane@payments.example>", "Raise timeout", day(3), map[string]string{
		"src/retry.py":        "class RetryPolicy:\n    max_attempts = 3\n    timeout = 30\n",
		"vendor/lib/retry.js": "var timeout = 1;\n",
	})
	shas["vendor"] = r.commit("Marta Ruiz <marta@libs.example>", "Vendor update", day(1), map[string]string{
		"vendor/lib/retry.js": "var timeout = 2;\n",
	})
	return r, shas
}

func TestIngestReadsCommitsNewestFirstWithTheirChangedLines(t *testing.T) {
	r, shas := paymentsHistory(t)
	store := r.ingest()
	if store.Commits() != 4 || store.Head != shas["vendor"] {
		t.Fatalf("store has %d commits up to %s, want 4 up to %s", store.Commits(), store.Head, shas["vendor"])
	}
	raise, ok := store.Commit(shas["raise"])
	if !ok {
		t.Fatal("Raise timeout is missing")
	}
	if raise.AuthorName != "Jane Doe" || !raise.At.Equal(day(3)) {
		t.Errorf("author and date = %s, %s; want Jane Doe, %s", raise.AuthorName, raise.At, day(3))
	}
	want := []FileChange{
		{Path: "src/retry.py", Added: 1, Text: []byte("    timeout = 30\n")},
		{Path: "vendor/lib/retry.js", Added: 1, Text: []byte("var timeout = 1;\n")},
	}
	if !reflect.DeepEqual(raise.Files, want) {
		t.Errorf("files = %+v, want %+v", raise.Files, want)
	}
	add, _ := store.Commit(shas["add"])
	if add.AuthorName != "Jane Doe" || add.AuthorEmail != "jane@payments.example" {
		t.Errorf("old author = %s <%s>, want Jane Doe <jane@payments.example> after .mailmap", add.AuthorName, add.AuthorEmail)
	}
}

func TestSkippedFilesAreListedWithoutTheirLines(t *testing.T) {
	r, shas := paymentsHistory(t)
	skipVendor := func(path string) bool { return strings.HasPrefix(path, "vendor/") }
	store, err := Ingest(context.Background(), r.root, Options{Skip: skipVendor}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	raise, _ := store.Commit(shas["raise"])
	want := []FileChange{
		{Path: "src/retry.py", Added: 1, Text: []byte("    timeout = 30\n")},
		{Path: "vendor/lib/retry.js", Added: 1},
	}
	if !reflect.DeepEqual(raise.Files, want) {
		t.Errorf("files = %+v, want %+v: the vendored file counted but its lines left out", raise.Files, want)
	}
}

func TestHistoryQueries(t *testing.T) {
	// @covers op:author op:msg
	r, _ := paymentsHistory(t)
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	tests := []struct {
		query string
		want  []string
	}{
		{"author:jane", []string{"Raise timeout", "Add retry policy"}},
		{`author:"Jane Doe"`, []string{"Raise timeout", "Add retry policy"}},
		{"author:@libs.example", []string{"Vendor update"}},
		{"msg:flaky", []string{"Fix flaky checkout test"}},
		{"author:jane timeout", []string{"Raise timeout"}},
		{`author:jason f:.*test\.py$ timeout`, []string{"Fix flaky checkout test"}},
		{"type:commit timeout -f:vendor/", []string{"Raise timeout", "Fix flaky checkout test"}},
		{"type:commit timeout since:7d", []string{"Vendor update", "Raise timeout"}},
		{"type:commit max_attempts", []string{"Add retry policy"}},
		{"author:marta OR msg:flaky", []string{"Vendor update", "Fix flaky checkout test"}},
		{"type:commit lang:javascript", []string{"Vendor update", "Raise timeout"}},
		{"type:commit repo:other", []string{}},
	}
	for _, tt := range tests {
		got, _ := runSearch(t, tt.query, repo)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s = %q, want %q", tt.query, got, tt.want)
		}
	}
}

func TestCommitResultsSayWhatMatched(t *testing.T) {
	r, shas := paymentsHistory(t)
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	_, items := runSearch(t, "type:commit (timeout OR max_attempts) -f:vendor/", repo)
	byShas := map[string]protocol.ResultItem{}
	for _, item := range items {
		byShas[item.SHA] = item
	}
	raise := byShas[shas["raise"]]
	if !reflect.DeepEqual(raise.MatchedTerms, []int{0}) || raise.DiffHits != 1 {
		t.Errorf("Raise timeout matched terms %v with %d diff hits, want [0] and 1", raise.MatchedTerms, raise.DiffHits)
	}
	if want := []protocol.FileStat{{Path: "src/retry.py", Added: 1}}; !reflect.DeepEqual(raise.Files, want) {
		t.Errorf("Raise timeout lists files %+v, want only %+v: the vendor file failed -f:vendor/", raise.Files, want)
	}
	if want := []protocol.Range{{Start: 6, End: 13}}; !reflect.DeepEqual(raise.SubjectHits, want) {
		t.Errorf("subject hits = %v, want %v (timeout)", raise.SubjectHits, want)
	}
	if add := byShas[shas["add"]]; !reflect.DeepEqual(add.MatchedTerms, []int{1}) {
		t.Errorf("Add retry policy matched terms %v, want [1] (max_attempts)", add.MatchedTerms)
	}
	if ref, ok := ParseRef(raise.Ref); !ok || ref.SHA != shas["raise"] || ref.RepoID != "r1" {
		t.Errorf("ref %q decodes to %+v, %v", raise.Ref, ref, ok)
	}
}

func TestFiltersReportTheCommitsTheyHid(t *testing.T) {
	r, _ := paymentsHistory(t)
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	stats, err := Search(context.Background(), plan(t, "type:commit timeout -f:vendor/"), []Repo{repo}, 1, func(protocol.ResultItem) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Hidden) != 1 || stats.Hidden[0].Count != 1 || stats.Hidden[0].Unit != "commits" || stats.Hidden[0].Filter != "-f:vendor/" {
		t.Errorf("hidden = %+v, want 1 commit hidden by -f:vendor/", stats.Hidden)
	}
	stats, err = Search(context.Background(), plan(t, "type:commit case:yes TIMEOUT"), []Repo{repo}, 1, func(protocol.ResultItem) {})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 0 || len(stats.Hidden) != 1 || stats.Hidden[0].Count != 3 || stats.Hidden[0].Reason != "case" {
		t.Errorf("case:yes TIMEOUT: %d results, hidden %+v; want none, and 3 commits hidden by case:yes", stats.Total, stats.Hidden)
	}
}

func TestPreviewShowsTheDiffWithContextAndMarksTheTerms(t *testing.T) {
	r, shas := paymentsHistory(t)
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	preview, err := Preview(context.Background(), &repo, Ref{RepoID: "r1", SHA: shas["raise"]}, plan(t, "type:commit timeout -f:vendor/"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Subject != "Raise timeout" || preview.Author != "Jane Doe <jane@payments.example>" {
		t.Errorf("preview header = %q by %q", preview.Subject, preview.Author)
	}
	wantFiles := []protocol.FileStat{
		{Path: "src/retry.py", Added: 1},
		{Path: "vendor/lib/retry.js", Added: 1, HiddenByFilter: true},
	}
	if !reflect.DeepEqual(preview.Files, wantFiles) {
		t.Errorf("files = %+v, want %+v", preview.Files, wantFiles)
	}
	if len(preview.Hunks) != 2 || preview.Hunks[0].Path != "src/retry.py" {
		t.Fatalf("hunks = %+v, want one per file, src/retry.py first", preview.Hunks)
	}
	lines := preview.Hunks[0].Lines
	if len(lines) != 3 || lines[0].Kind != "ctx" || lines[0].Text != "class RetryPolicy:" || lines[2].Kind != "add" || lines[2].NewNo != 3 {
		t.Fatalf("src/retry.py hunk = %+v, want two context lines then the added line 3", lines)
	}
	if want := []protocol.Hit{{Start: 4, End: 11, TermIndex: 0}}; !reflect.DeepEqual(lines[2].Hits, want) {
		t.Errorf("added line hits = %v, want %v", lines[2].Hits, want)
	}
	target, err := OpenTarget(context.Background(), &repo, Ref{RepoID: "r1", SHA: shas["raise"]})
	if err != nil || target.SHA != shas["raise"] || target.RepoID != "r1" {
		t.Errorf("OpenTarget = %+v, %v; want the commit", target, err)
	}
	if _, err := Preview(context.Background(), &repo, Ref{RepoID: "r1", SHA: strings.Repeat("0", 40)}, nil, 3); !errors.Is(err, ErrStale) {
		t.Errorf("Preview of a missing commit: %v, want ErrStale", err)
	}
}

func TestUpdateReadsNewCommitsOrRereadsRewrittenHistory(t *testing.T) {
	r, shas := paymentsHistory(t)
	store := r.ingest()
	newest := r.commit("Jason Kim <jason@checkout.example>", "Retry on 503", day(0), map[string]string{"src/retry.py": "retry_on = [503]\n"})
	updated, err := Update(context.Background(), r.root, store, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Commits() != 5 || updated.Head != newest || store.Commits() != 4 {
		t.Errorf("after a commit: %d commits up to %s (old store %d); want 5 up to %s, old store unchanged", updated.Commits(), updated.Head, store.Commits(), newest)
	}
	if touch, _ := updated.LastCommit("src/retry.py"); touch.SHA != newest {
		t.Errorf("src/retry.py last changed in %s, want %s", touch.SHA, newest)
	}
	r.git("reset", "--quiet", "--hard", shas["flaky"])
	rewound, err := Update(context.Background(), r.root, updated, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rewound.Commits() != 2 || rewound.Head != shas["flaky"] {
		t.Errorf("after a reset: %d commits up to %s, want 2 up to %s", rewound.Commits(), rewound.Head, shas["flaky"])
	}
}

func TestStoreKnowsEachFilesNewestCommitAndUncommittedEdits(t *testing.T) {
	r, shas := paymentsHistory(t)
	r.write("src/retry.py", "edited\n")
	r.write("notes.txt", "new\n")
	store := r.ingest()
	if touch, ok := store.LastCommit("vendor/lib/retry.js"); !ok || touch.SHA != shas["vendor"] || touch.Author != "Marta Ruiz" {
		t.Errorf("vendor/lib/retry.js last commit = %+v, want Marta's %s", touch, shas["vendor"])
	}
	for path, want := range map[string]bool{"src/retry.py": true, "notes.txt": true, "tests/checkout_test.py": false} {
		if got := store.Dirty(path); got != want {
			t.Errorf("Dirty(%s) = %v, want %v", path, got, want)
		}
	}
	if !store.MarkDirty([]string{"tests/checkout_test.py"}).Dirty("tests/checkout_test.py") || store.Dirty("tests/checkout_test.py") {
		t.Error("MarkDirty should mark a copy, not the store")
	}
}

func TestSaveAndLoadKeepTheCommits(t *testing.T) {
	r, shas := paymentsHistory(t)
	store := r.ingest()
	path := filepath.Join(t.TempDir(), "history")
	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: loaded}
	if got, _ := runSearch(t, "msg:flaky", repo); loaded.Head != shas["vendor"] || !reflect.DeepEqual(got, []string{"Fix flaky checkout test"}) {
		t.Errorf("loaded store: head %s, msg:flaky = %q", loaded.Head, got)
	}
}

func TestAFolderOutsideGitHasNoHistory(t *testing.T) {
	if _, err := Ingest(context.Background(), t.TempDir(), Options{Since: fixedNow}, nil, nil); !errors.Is(err, ErrNotGit) {
		t.Errorf("Ingest outside Git: %v, want ErrNotGit", err)
	}
}

func TestAuthorsAndWordsSummarizeTheHistory(t *testing.T) {
	r, _ := paymentsHistory(t)
	store := r.ingest()
	authors := store.Authors()
	if len(authors) != 3 || authors[0].Name != "Marta Ruiz" || authors[1].Name != "Jane Doe" || authors[1].Commits != 2 {
		t.Fatalf("authors = %+v, want Marta, Jane (2 commits), Jason", authors)
	}
	words := store.Words(day(30))
	if words["timeout"].Commits != 1 || words["fix flaky"].Commits != 1 || words["retry"].Commits != 0 {
		t.Errorf("words since day 30 = %+v, want timeout and \"fix flaky\" once, not retry (day 40)", words)
	}
}

func TestParseRefRejectsWhatItDidNotWrite(t *testing.T) {
	for _, text := range []string{"hist|1|r1|--upload-pack=x", "hist|1|r1|abc", "tree|1|r1|0|0|0|a.go", "hist|x|r1|abcdef0"} {
		if _, ok := ParseRef(text); ok {
			t.Errorf("ParseRef(%q) accepted it", text)
		}
	}
}
