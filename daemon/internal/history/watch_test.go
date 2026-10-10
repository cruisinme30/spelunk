package history

import (
	"context"
	"slices"
	"testing"
	"time"
)

// subjectsOf lists the commits' subjects, in order.
func subjectsOf(commits []Commit) []string {
	subjects := make([]string, len(commits))
	for i := range commits {
		subjects[i] = commits[i].Subject
	}
	return subjects
}

// update brings store up to date with the repo, failing the test on error.
func (r *testRepo) update(store *Store) *Store {
	r.t.Helper()
	next, err := Update(context.Background(), r.root, store, Options{})
	if err != nil {
		r.t.Fatalf("Update: %v", err)
	}
	return next
}

func TestNewCommitsAreTheCommitsAnUpdateReadOnTop(t *testing.T) {
	// @covers screen:query-watches
	r, _ := paymentsHistory(t)
	old := r.ingest()
	r.commit("Jason Kim <jason@checkout.example>", "Revert ledger batching", day(0), map[string]string{"src/ledger.py": "write = 1\n"})
	r.commit("Marta Ruiz <marta@libs.example>", "Revert refunds retry", day(0).Add(1), map[string]string{"src/refunds.py": "retry = 0\n"})
	next := r.update(old)
	got := subjectsOf(NewCommits(old, next))
	if want := []string{"Revert refunds retry", "Revert ledger batching"}; !slices.Equal(got, want) {
		t.Errorf("NewCommits = %q, want %q (newest first)", got, want)
	}
}

func TestNewCommitsAreNoneWithoutAnEarlierStoreOrAMove(t *testing.T) {
	// @covers screen:query-watches
	r, _ := paymentsHistory(t)
	store := r.ingest()
	if got := NewCommits(nil, store); got != nil {
		t.Errorf("NewCommits(nil, store) = %q, want none: the first read is not news", subjectsOf(got))
	}
	if got := NewCommits(store, r.update(store)); got != nil {
		t.Errorf("NewCommits after an update with HEAD unmoved = %q, want none", subjectsOf(got))
	}
}

func TestNewCommitsAfterARewriteAreOnlyTheOnesNewerThanAnyKnown(t *testing.T) {
	// @covers screen:query-watches
	r, _ := paymentsHistory(t)
	r.git("branch", "old-work", "HEAD~2") // a branch that stops at "Fix flaky checkout test"
	old := r.ingest()

	// Switching to the older branch brings no commit newer than the store's.
	r.git("checkout", "--quiet", "old-work")
	switched := r.update(old)
	if got := NewCommits(old, switched); len(got) != 0 {
		t.Errorf("NewCommits after switching to an older branch = %q, want none", subjectsOf(got))
	}

	// A commit on top of the switched branch is a rewrite from the old store's
	// view, and is newer than anything it held: it counts.
	r.commit("Jason Kim <jason@checkout.example>", "Revert checkout timeout", day(0), map[string]string{"tests/checkout_test.py": "timeout = 1\n"})
	next := r.update(old)
	if got := subjectsOf(NewCommits(old, next)); !slices.Equal(got, []string{"Revert checkout timeout"}) {
		t.Errorf("NewCommits after a rewrite = %q, want only the commit newer than the old store's", got)
	}
}

// A pull that rebases your commit onto upstream brings in upstream's
// commits, though they were authored before yours: they were committed
// here after it.
func TestNewCommitsAfterARebasingPullIncludeUpstreamsOlderWork(t *testing.T) {
	// @covers screen:query-watches
	r, _ := paymentsHistory(t)
	mine := r.commit("Test <test@example.com>", "My local work", day(0), map[string]string{"src/mine.py": "m = 1\n"})
	old := r.ingest()

	r.git("reset", "--quiet", "--hard", "HEAD~1")
	r.write("src/upstream.py", "u = 1\n")
	r.git("add", "--all")
	r.gitWithEnv([]string{
		"GIT_AUTHOR_NAME=Jason Kim", "GIT_AUTHOR_EMAIL=jason@checkout.example",
		"GIT_AUTHOR_DATE=" + day(1).Format(time.RFC3339), "GIT_COMMITTER_DATE=" + day(0).Add(time.Hour).Format(time.RFC3339),
	}, "commit", "--quiet", "-m", "Revert upstream thing")
	r.gitWithEnv([]string{"GIT_COMMITTER_DATE=" + day(0).Add(2*time.Hour).Format(time.RFC3339)}, "cherry-pick", mine)

	got := subjectsOf(NewCommits(old, r.update(old)))
	if !slices.Contains(got, "Revert upstream thing") {
		t.Errorf("NewCommits after a rebasing pull = %q, want upstream's commit among them", got)
	}
}

func TestStoreOfIsSearchableOnItsOwn(t *testing.T) {
	// @covers screen:query-watches
	r, _ := paymentsHistory(t)
	old := r.ingest()
	r.commit("Jason Kim <jason@checkout.example>", "Revert ledger batching", day(0), map[string]string{"src/ledger.py": "write = 1\n"})
	r.commit("Marta Ruiz <marta@libs.example>", "Tune the timeout", day(0).Add(1), map[string]string{"src/retry.py": "timeout = 9\n"})
	next := r.update(old)
	fresh := StoreOf(next.Head, NewCommits(old, next))
	if fresh.Commits() != 2 {
		t.Fatalf("StoreOf holds %d commits, want 2", fresh.Commits())
	}
	got, _ := runSearch(t, "msg:revert", Repo{ID: "r", Name: "payments", Root: r.root, Store: fresh})
	if !slices.Equal(got, []string{"Revert ledger batching"}) {
		t.Errorf("msg:revert over the new commits = %q, want only the new revert", got)
	}
	got, _ = runSearch(t, "type:commit timeout", Repo{ID: "r", Name: "payments", Root: r.root, Store: fresh})
	if !slices.Equal(got, []string{"Tune the timeout"}) {
		t.Errorf("type:commit timeout over the new commits = %q, want only the new commit, not the older ones that match", got)
	}
}

func TestUserEmailIsTheRepoConfiguredEmail(t *testing.T) {
	r := newTestRepo(t)
	if got := UserEmail(context.Background(), r.root); got != "test@example.com" {
		t.Errorf("UserEmail = %q, want test@example.com", got)
	}
}
