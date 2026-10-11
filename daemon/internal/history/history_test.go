package history

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
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

// commitBySHA finds a commit in store by sha.
func commitBySHA(store *Store, sha string) (*Commit, bool) {
	for _, seg := range store.segments {
		for i := range seg.commits {
			if seg.commits[i].SHA == sha {
				return &seg.commits[i], true
			}
		}
	}
	return nil, false
}

func TestIngestReadsCommitsNewestFirstWithTheirChangedLines(t *testing.T) {
	r, shas := paymentsHistory(t)
	store := r.ingest()
	if store.Commits() != 4 || store.Head != shas["vendor"] {
		t.Fatalf("store has %d commits up to %s, want 4 up to %s", store.Commits(), store.Head, shas["vendor"])
	}
	raise, ok := commitBySHA(store, shas["raise"])
	if !ok {
		t.Fatal("Raise timeout is missing")
	}
	if raise.AuthorName != "Jane Doe" || !raise.At.Equal(day(3)) {
		t.Errorf("author and date = %s, %s; want Jane Doe, %s", raise.AuthorName, raise.At, day(3))
	}
	want := []FileChange{
		{Path: "src/retry.py", Added: 1, Text: []byte("    timeout = 30\n"), Signs: []byte("+")},
		{Path: "vendor/lib/retry.js", Added: 1, Text: []byte("var timeout = 1;\n"), Signs: []byte("+")},
	}
	if !reflect.DeepEqual(raise.Files, want) {
		t.Errorf("files = %+v, want %+v", raise.Files, want)
	}
	vendor, _ := commitBySHA(store, shas["vendor"])
	want = []FileChange{
		{Path: "vendor/lib/retry.js", Added: 1, Removed: 1, Text: []byte("var timeout = 1;\nvar timeout = 2;\n"), Signs: []byte("-+")},
	}
	if !reflect.DeepEqual(vendor.Files, want) {
		t.Errorf("files = %+v, want %+v: each line with its sign, in diff order", vendor.Files, want)
	}
	add, _ := commitBySHA(store, shas["add"])
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
	raise, _ := commitBySHA(store, shas["raise"])
	want := []FileChange{
		{Path: "src/retry.py", Added: 1, Text: []byte("    timeout = 30\n"), Signs: []byte("+")},
		{Path: "vendor/lib/retry.js", Added: 1},
	}
	if !reflect.DeepEqual(raise.Files, want) {
		t.Errorf("files = %+v, want %+v: the vendored file counted but its lines left out", raise.Files, want)
	}
}

func TestHistoryQueries(t *testing.T) {
	// @covers op:author op:msg op:is screen:test-history
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
		{"author:jason is:test", []string{"Fix flaky checkout test"}},
		{"type:commit timeout -is:test", []string{"Vendor update", "Raise timeout"}},
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

func TestTextTermsMatchTheMessageToo(t *testing.T) {
	r, _ := paymentsHistory(t)
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	tests := []struct {
		query string
		want  []string
	}{
		// Only the messages say flaky and update.
		{"type:commit flaky", []string{"Fix flaky checkout test"}},
		{"author:marta update", []string{"Vendor update"}},
		// A message match still needs a file that passes f:.
		{"type:commit f:vendor/ raise", []string{"Raise timeout"}},
		{"type:commit f:^tests/ raise", []string{}},
		// Leaving a word out leaves out the commits whose message has it.
		{"type:commit timeout -flaky", []string{"Vendor update", "Raise timeout"}},
		{"type:commit (flaky OR update)", []string{"Vendor update", "Fix flaky checkout test"}},
	}
	for _, tt := range tests {
		got, _ := runSearch(t, tt.query, repo)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s = %q, want %q", tt.query, got, tt.want)
		}
	}
}

func TestContentMatchesDiffsButNeverMessages(t *testing.T) {
	// @covers op:content
	r, _ := paymentsHistory(t)
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	// Only the messages say flaky and update, so content: finds neither.
	for _, query := range []string{"type:commit content:flaky", "author:marta content:update"} {
		if got, _ := runSearch(t, query, repo); len(got) != 0 {
			t.Errorf("%s = %q, want nothing", query, got)
		}
	}
	got, items := runSearch(t, "type:commit content:timeout", repo)
	plain, _ := runSearch(t, "type:commit timeout", repo)
	if len(got) == 0 || !reflect.DeepEqual(got, plain) {
		t.Errorf("content:timeout = %q, want the commits whose diffs say timeout, %q", got, plain)
	}
	for _, item := range items {
		if item.InMessage || len(item.SubjectHits) != 0 {
			t.Errorf("%s: marked in its message (%v, %v), want only its diff", item.Subject, item.InMessage, item.SubjectHits)
		}
	}
}

func TestCommitResultsSayWhetherTheMessageMatched(t *testing.T) {
	r := newTestRepo(t)
	shas := map[string]string{}
	shas["cap"] = r.commit("Jane Doe <jane@payments.example>", "Cap total retry timeout", day(9), map[string]string{
		"src/retry.py": "retry_timeout = 30\n",
	})
	shas["backoff"] = r.commit("Jane Doe <jane@payments.example>", "Back off between charge attempts\n\nWaits grow each time.\nSo a retry storm can't hammer the processor.", day(4), map[string]string{
		"src/charge.py": "sleep(delay)\n",
	})
	shas["rename"] = r.commit("Jane Doe <jane@payments.example>", "Rename attempt counter", day(2), map[string]string{
		"src/retry.py": "retry_timeout = 30\nretry_count = 0\n",
	})
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	_, items := runSearch(t, "author:jane retry", repo)
	byShas := map[string]protocol.ResultItem{}
	for _, item := range items {
		byShas[item.SHA] = item
	}
	if capped := byShas[shas["cap"]]; !capped.InMessage || capped.BodyLine != nil || capped.DiffHits != 1 {
		t.Errorf("Cap total retry timeout: in message %v, body line %+v, %d diff hits; want the subject and 1 diff hit", capped.InMessage, capped.BodyLine, capped.DiffHits)
	}
	backoff := byShas[shas["backoff"]]
	want := &protocol.MessageLine{Text: "So a retry storm can't hammer the processor.", Hits: []protocol.Range{{Start: 5, End: 10}}}
	if !backoff.InMessage || !reflect.DeepEqual(backoff.BodyLine, want) || backoff.DiffHits != 0 {
		t.Errorf("Back off: in message %v, body line %+v, %d diff hits; want the body line %+v alone", backoff.InMessage, backoff.BodyLine, backoff.DiffHits, want)
	}
	if rename := byShas[shas["rename"]]; rename.InMessage || rename.DiffHits != 1 {
		t.Errorf("Rename attempt counter: in message %v, %d diff hits; want the diff alone", rename.InMessage, rename.DiffHits)
	}

	preview, err := Preview(context.Background(), &repo, Ref{RepoID: "r1", SHA: shas["backoff"]}, plan(t, "author:jane retry"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []protocol.Range{{Start: 27, End: 32}}; !reflect.DeepEqual(preview.BodyHits, want) || len(preview.SubjectHits) != 0 {
		t.Errorf("preview marks %v in the subject and %v in the body, want nothing and %v", preview.SubjectHits, preview.BodyHits, want)
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

func TestWordYesMatchesWholeWordsInDiffs(t *testing.T) {
	// @covers op:word
	r, _ := paymentsHistory(t)
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	all, err := Search(context.Background(), plan(t, "type:commit time"), []Repo{repo}, 1, func(protocol.ResultItem) {})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := Search(context.Background(), plan(t, "type:commit word:yes time"), []Repo{repo}, 1, func(protocol.ResultItem) {})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total == 0 || stats.Total != 0 || len(stats.Hidden) != 1 || stats.Hidden[0].Reason != "word" || stats.Hidden[0].Count != all.Total {
		t.Errorf("word:yes time: %d results, hidden %+v; want none, and all %d commits hidden by word:yes", stats.Total, stats.Hidden, all.Total)
	}
	timeout, err := Search(context.Background(), plan(t, "type:commit timeout"), []Repo{repo}, 1, func(protocol.ResultItem) {})
	if err != nil {
		t.Fatal(err)
	}
	stats, err = Search(context.Background(), plan(t, "type:commit word:yes timeout"), []Repo{repo}, 1, func(protocol.ResultItem) {})
	if err != nil {
		t.Fatal(err)
	}
	if timeout.Total == 0 || stats.Total != timeout.Total {
		t.Errorf("word:yes timeout found %d commits, want the %d that timeout finds", stats.Total, timeout.Total)
	}
}

func TestTypeAddedAndRemovedSearchOneSideOfTheDiff(t *testing.T) {
	// @covers op:type
	r, shas := paymentsHistory(t)
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	tests := []struct {
		query string
		want  []string
	}{
		// Raise timeout added "var timeout = 1;", and Vendor update replaced it.
		{`type:added "timeout = 1"`, []string{"Raise timeout"}},
		{`type:removed "timeout = 1"`, []string{"Vendor update"}},
		{`type:commit "timeout = 1"`, []string{"Vendor update", "Raise timeout"}},
		{"type:removed timeout", []string{"Vendor update"}},
		{"type:added timeout -f:vendor/", []string{"Raise timeout", "Fix flaky checkout test"}},
		// Text terms skip the message; msg: still reads it.
		{"type:added flaky", []string{}},
		{"type:added msg:flaky timeout", []string{"Fix flaky checkout test"}},
	}
	for _, tt := range tests {
		got, _ := runSearch(t, tt.query, repo)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s = %q, want %q", tt.query, got, tt.want)
		}
	}

	_, items := runSearch(t, "type:removed timeout", repo)
	if vendor := items[0]; vendor.DiffHits != 1 || vendor.InMessage || len(vendor.SubjectHits) != 0 {
		t.Errorf("Vendor update: %d diff hits, in message %v, subject hits %v; want 1 removed line and nothing in the message",
			vendor.DiffHits, vendor.InMessage, vendor.SubjectHits)
	}
	stats, err := Search(context.Background(), plan(t, "type:added flaky"), []Repo{repo}, 1, func(protocol.ResultItem) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Hidden) != 1 || stats.Hidden[0].Count != 1 || stats.Hidden[0].Reason != "type" || stats.Hidden[0].Filter != "type:added" {
		t.Errorf("type:added flaky hidden = %+v, want 1 commit hidden by type:added", stats.Hidden)
	}

	preview, err := Preview(context.Background(), &repo, Ref{RepoID: "r1", SHA: shas["vendor"]}, plan(t, "type:removed timeout"), 3)
	if err != nil {
		t.Fatal(err)
	}
	lines := preview.Hunks[0].Lines
	if len(lines) != 2 || lines[0].Kind != "del" || len(lines[0].Hits) != 1 || lines[1].Kind != "add" || len(lines[1].Hits) != 0 {
		t.Errorf("type:removed preview lines = %+v, want the removed line marked and the added line not", lines)
	}
	if len(preview.Files) != 1 || preview.Files[0].HiddenByFilter {
		t.Errorf("type:removed preview files = %+v, want the file shown", preview.Files)
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

// Checking out main after it merged your branch moves HEAD forward, but
// the branch's commits aren't on main's first-parent line: the store is
// read again rather than mixing the two.
func TestUpdateAfterAMergeOfYourBranchReadsMainsLine(t *testing.T) {
	r, _ := paymentsHistory(t)
	r.git("checkout", "--quiet", "-b", "feature")
	r.commit("Jane Doe <jane@payments.example>", "Feature work", day(1), map[string]string{"src/feature.py": "x = 1\n"})
	onFeature, err := Update(context.Background(), r.root, r.ingest(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	r.git("checkout", "--quiet", "-")
	r.commit("Jason Kim <jason@checkout.example>", "Main work", day(0), map[string]string{"src/main.py": "y = 1\n"})
	r.gitWithEnv([]string{"GIT_AUTHOR_NAME=Jason Kim", "GIT_AUTHOR_EMAIL=jason@checkout.example"},
		"merge", "--quiet", "--no-ff", "-m", "Merge feature", "feature")
	updated, err := Update(context.Background(), r.root, onFeature, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if want := r.ingest(); updated.Commits() != want.Commits() || updated.Head != want.Head {
		t.Errorf("after the merge: %d commits up to %s, want %d up to %s as a fresh read has",
			updated.Commits(), updated.Head, want.Commits(), want.Head)
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
	if !store.WithDirty(map[string]bool{"tests/checkout_test.py": true}).Dirty("tests/checkout_test.py") || store.Dirty("tests/checkout_test.py") {
		t.Error("WithDirty should mark a copy, not the store")
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

func TestAStoreSavedInAnOlderFormatIsReadAgain(t *testing.T) {
	// A store saved before lines kept their signs can't answer type:added,
	// so Load refuses it and the indexer reads the history once more.
	r, _ := paymentsHistory(t)
	path := filepath.Join(t.TempDir(), "history")
	old := savedStore{Version: storeFormatVersion - 1, Head: r.git("rev-parse", "HEAD")}
	if err := trigram.SaveGob(path, ".history-*", "history", old); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, errStaleFormat) {
		t.Errorf("Load of an older format = %v, want errStaleFormat", err)
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
	for _, text := range []string{"hist|1|r1|--upload-pack=x", "hist|1|r1|abc", "tree|1|r1|0|0|0|0|0|a.go", "hist|x|r1|abcdef0"} {
		if _, ok := ParseRef(text); ok {
			t.Errorf("ParseRef(%q) accepted it", text)
		}
	}
}

func TestSearchCountsCommitsByRepoAuthorAndMonth(t *testing.T) {
	r, _ := paymentsHistory(t)
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	stats, err := Search(context.Background(), plan(t, "type:commit timeout count:1"), []Repo{repo}, 1, func(protocol.ResultItem) {})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, facet := range stats.Facets {
		for _, bucket := range facet.Buckets {
			got[facet.Field] = append(got[facet.Field], fmt.Sprintf("%s %d %s", bucket.Label, bucket.Count, bucket.Filter))
		}
	}
	want := map[string][]string{
		// every counted commit, not just the page count:1 shows
		engine.FacetRepo:   {"payments-api 3 repo:^payments-api$"},
		engine.FacetAuthor: {`Jane Doe 1 author:"Jane Doe"`, `Jason Kim 1 author:"Jason Kim"`, `Marta Ruiz 1 author:"Marta Ruiz"`},
		// in local time: these dates are September and October in every zone from UTC-12 to UTC+13
		engine.FacetMonth: {"Oct 2026 1 since:2026-10 until:2026-10", "Sep 2026 2 since:2026-09 until:2026-09"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("facets:\n got %q\nwant %q", got, want)
	}
}

func TestIsGeneratedAndIsVendoredInHistory(t *testing.T) {
	// @covers op:is screen:generated-vendored
	r := newTestRepo(t)
	r.commit("Jane Doe <jane@payments.example>", "Rename the retry policy", day(5), map[string]string{
		"src/retry.py":           "class BackoffPolicy:\n",
		"src/proto/retry_pb2.py": "BackoffPolicy = None\n",
	})
	r.commit("Marta Ruiz <marta@libs.example>", "Regenerate the protos", day(3), map[string]string{
		"src/proto/retry_pb2.py": "BackoffPolicy = object\n",
	})
	r.commit("Marta Ruiz <marta@libs.example>", "Update stripe_compat", day(2), map[string]string{
		"third_party/stripe_compat/retry.py": "class BackoffPolicy(object):\n",
	})
	// A commit's file has no header to read, so a file marked generated only
	// by its header counts as code people wrote.
	r.commit("Jason Kim <jason@checkout.example>", "Add the schema", day(1), map[string]string{
		"web/schema.ts": "// @generated by codegen\nexport type BackoffPolicy = {};\n",
	})
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: r.ingest()}
	tests := []struct {
		query string
		want  []string
	}{
		{"type:commit BackoffPolicy -is:generated -is:vendored", []string{"Add the schema", "Rename the retry policy"}},
		{"type:commit BackoffPolicy is:generated", []string{"Regenerate the protos", "Rename the retry policy"}},
		{"type:commit BackoffPolicy is:vendored", []string{"Update stripe_compat"}},
		{"author:marta -is:vendored", []string{"Regenerate the protos"}},
	}
	for _, tt := range tests {
		got, _ := runSearch(t, tt.query, repo)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s = %q, want %q", tt.query, got, tt.want)
		}
	}
}

// Each update adds a segment of the commits it read, often one; the small
// ones merge, so a day of commits leaves a few segments, not one apiece,
// and every commit stays searchable and announced once.
func TestUpdatesMergeTheirSmallSegments(t *testing.T) {
	r, _ := paymentsHistory(t)
	store := r.ingest()
	const commits = 64
	for i := range commits {
		subject := fmt.Sprintf("Tune retry %d", i)
		r.commit("Jason Kim <jason@checkout.example>", subject, day(0).Add(time.Duration(i)*time.Minute), map[string]string{"src/retry.py": subject + "\n"})
		next := r.update(store)
		if got := subjectsOf(NewCommits(store, next)); !slices.Equal(got, []string{subject}) {
			t.Fatalf("NewCommits after %q = %q, want just it", subject, got)
		}
		store = next
	}
	if store.Commits() != 4+commits {
		t.Errorf("%d commits, want %d", store.Commits(), 4+commits)
	}
	if len(store.segments) > 8 {
		t.Errorf("%d segments after %d one-commit updates, want a few", len(store.segments), commits)
	}
	repo := Repo{ID: "r1", Name: "payments-api", Root: r.root, Store: store}
	if got, _ := runSearch(t, `msg:"Tune retry 7" count:100`, repo); !slices.Equal(got, []string{"Tune retry 7"}) {
		t.Errorf(`msg:"Tune retry 7" = %q`, got)
	}
	if got, _ := runSearch(t, "msg:flaky", repo); !slices.Equal(got, []string{"Fix flaky checkout test"}) {
		t.Errorf("msg:flaky = %q, want the ingested commit still found", got)
	}
}

// The history window moves on with time: an update drops the commits made
// before it, and the newest-commit facts only they gave, as a fresh read
// under the same window would.
func TestUpdateDropsCommitsOlderThanTheWindow(t *testing.T) {
	r, _ := paymentsHistory(t)
	store, err := Ingest(context.Background(), r.root, Options{Since: day(50)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	later := Options{Since: day(5)}
	unmoved, err := Update(context.Background(), r.root, store, later)
	if err != nil {
		t.Fatal(err)
	}
	r.commit("Jason Kim <jason@checkout.example>", "Retry on 503", day(0), map[string]string{"src/retry.py": "retry_on = [503]\n"})
	moved, err := Update(context.Background(), r.root, store, later)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Ingest(context.Background(), r.root, later, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]*Store{"with HEAD unmoved": unmoved, "with a new commit": moved} {
		if _, ok := got.LastCommit("tests/checkout_test.py"); ok {
			t.Errorf("%s: tests/checkout_test.py keeps the touch of a commit before the window", name)
		}
		if _, ok := got.LastCommit("src/retry.py"); !ok {
			t.Errorf("%s: src/retry.py lost its touch, made in the window", name)
		}
	}
	if got, want := subjectsOf(moved.newest(moved.Commits())), subjectsOf(want.newest(want.Commits())); !slices.Equal(got, want) {
		t.Errorf("after the window moved: %q, want %q as a fresh read has", got, want)
	}
	if got := subjectsOf(NewCommits(store, moved)); !slices.Equal(got, []string{"Retry on 503"}) {
		t.Errorf("NewCommits = %q, want the new commit alone", got)
	}
	if unmoved.Commits() != 2 {
		t.Errorf("with HEAD unmoved: %d commits, want the 2 made in the window", unmoved.Commits())
	}
}
