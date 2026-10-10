package server

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// matchLog records every watch/matched notification the client receives.
type matchLog struct {
	mu      sync.Mutex
	matches []protocol.WatchMatchedParams
}

func collectMatches(client *testClient) *matchLog {
	log := &matchLog{}
	client.conn.OnNotify(protocol.MethodWatchMatched, func(raw json.RawMessage) {
		var params protocol.WatchMatchedParams
		_ = json.Unmarshal(raw, &params)
		log.mu.Lock()
		defer log.mu.Unlock()
		log.matches = append(log.matches, params)
	})
	return log
}

func (l *matchLog) all() []protocol.WatchMatchedParams {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.matches)
}

// commitAs commits files in root as author (whose email is made from the
// name, as gitRepo does), or as the repo's own user when author is "".
func commitAs(t *testing.T, root, author, subject string, files map[string]string) {
	t.Helper()
	testutil.WriteFiles(t, root, files)
	var env []string
	if author != "" {
		env = []string{"GIT_AUTHOR_NAME=" + author, "GIT_AUTHOR_EMAIL=" + strings.ToLower(strings.ReplaceAll(author, " ", ".")) + "@example.com"}
	}
	for _, args := range [][]string{{"add", "--all"}, {"commit", "--quiet", "-m", subject}} {
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// subjectsOf lists the subjects of a match's commits.
func subjectsOf(match protocol.WatchMatchedParams) []string {
	subjects := make([]string, len(match.Commits))
	for i, c := range match.Commits {
		subjects[i] = c.Subject
	}
	return subjects
}

func TestWatchedHistoryQueriesAnnounceNewCommitsByOthers(t *testing.T) {
	// @covers screen:query-watches rpc:watch/matched
	root := checkoutRepo(t)
	client := newTestClient(t)
	matches := collectMatches(client)
	settings := DefaultSettings()
	settings.Location = t.TempDir()
	t.Cleanup(func() { client.server.index.Close(shutdownGrace) })
	params := protocol.InitializeParams{
		Protocol: protocol.Version, Settings: settings,
		Roots: []protocol.Root{{ID: "r1", Path: root, Name: "payments-api"}},
		// A history query, the same one again, a working-tree query and one with an error.
		Watches: []string{"msg:revert", " msg:revert ", "timeout", "author:"},
	}
	if err := client.call(protocol.MethodInitialize, params, nil); err != nil {
		t.Fatal(err)
	}
	client.waitForHistory(t)
	if got := client.server.Watches(); !slices.Equal(got, []string{"msg:revert", "timeout", "author:"}) {
		t.Errorf("Watches() = %q, want them trimmed and without the repeat", got)
	}
	if got := matches.all(); len(got) != 0 {
		t.Fatalf("reading history the first time announced %+v; want nothing", got)
	}

	commitAs(t, root, "Jason Kim", "Revert ledger batching", map[string]string{"src/ledger.py": "write = 1\n"})
	commitAs(t, root, "", "Revert my own experiment", map[string]string{"src/mine.py": "x = 1\n"})
	commitAs(t, root, "Marta Ruiz", "Tune the timeout", map[string]string{"src/retry.py": "timeout = 9\n"})
	waitUntil(t, "a watch/matched notification", func() bool { return len(matches.all()) > 0 })
	time.Sleep(100 * time.Millisecond) // let any further (wrong) announcement arrive

	got := matches.all()
	if len(got) != 1 {
		t.Fatalf("announcements = %+v, want one, for msg:revert only", got)
	}
	match := got[0]
	if match.Query != "msg:revert" || match.RepoID != "r1" || match.RepoName != "payments-api" || match.Total != 1 {
		t.Errorf("announcement = %+v, want msg:revert in r1 (payments-api) with 1 commit", match)
	}
	if subjects := subjectsOf(match); !slices.Equal(subjects, []string{"Revert ledger batching"}) {
		t.Errorf("announced commits = %q, want Jason's revert only: not my own, nor older reverts", subjects)
	}
	if c := match.Commits[0]; c.Author != "Jason Kim" || len(c.SHA) != 40 {
		t.Errorf("announced commit = %+v, want Jason Kim's with its full sha", c)
	}
}

func TestWatchSetReplacesTheWatchedQueries(t *testing.T) {
	// @covers screen:query-watches rpc:watch/set
	client := newTestClient(t)
	client.mustInitialize(t)
	if err := client.conn.Notify(protocol.MethodWatchSet, protocol.WatchSetParams{Queries: []string{"msg:revert", ""}}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "watch/set to apply", func() bool { return slices.Equal(client.server.Watches(), []string{"msg:revert"}) })
	if err := client.conn.Notify(protocol.MethodWatchSet, protocol.WatchSetParams{Queries: []string{}}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "an empty watch/set to clear the watches", func() bool { return len(client.server.Watches()) == 0 })
}

func TestWatchMatchesCountsEveryMatchAndHonoursTheFilters(t *testing.T) {
	// @covers screen:query-watches
	root := gitRepo(t,
		gitCommit{"Jane Doe", "Revert payout batching", time.Now().AddDate(0, 0, -2), map[string]string{"payments/payout.py": "a = 1\n"}},
		gitCommit{"Jason Kim", "Revert cart badge", time.Now().AddDate(0, 0, -1), map[string]string{"web/cart.ts": "b = 1\n"}},
		gitCommit{"Marta Ruiz", "Revert ledger rounding", time.Now(), map[string]string{"payments/ledger.py": "c = 1\n"}},
	)
	store, err := history.Ingest(context.Background(), root, history.Options{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(t)
	repo := history.Repo{ID: "r1", Name: "payments-api", Root: root, Store: store}

	match, ok := client.server.watchMatches("msg:revert f:payments/ count:1", repo)
	if !ok || match.Total != 2 || !slices.Equal(subjectsOf(match), []string{"Revert ledger rounding"}) {
		t.Errorf("msg:revert f:payments/ count:1 = %+v, %v; want 2 in total, the newest listed", match, ok)
	}
	if _, ok := client.server.watchMatches("msg:revert repo:web-app", repo); ok {
		t.Error("a watch whose repo: leaves this repo out matched")
	}
	if _, ok := client.server.watchMatches("retry", repo); ok {
		t.Error("a query over current files matched as a watch")
	}
}

func TestOthersCommitsLeavesOutTheUsersOwn(t *testing.T) {
	commits := []history.Commit{{Subject: "mine", AuthorEmail: "Me@Example.com"}, {Subject: "theirs", AuthorEmail: "them@example.com"}}
	kept := othersCommits(commits, "me@example.com")
	if len(kept) != 1 || kept[0].Subject != "theirs" {
		t.Errorf("othersCommits = %+v, want only theirs (emails compared ignoring case)", kept)
	}
	if len(commits) != 2 || commits[0].Subject != "mine" {
		t.Error("othersCommits changed its argument")
	}
	if kept := othersCommits(commits, ""); len(kept) != 2 {
		t.Errorf("othersCommits without an email = %+v, want both", kept)
	}
}
