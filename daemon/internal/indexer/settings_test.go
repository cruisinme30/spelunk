package indexer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// waitUntil polls condition for up to 5 seconds.
func waitUntil(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// searchPaths runs text over the published working-tree indexes and
// returns the path of each result.
func searchPaths(t *testing.T, ix *Indexer, text string) []string {
	t.Helper()
	plan, _, err := query.NewPlan(query.Parse(text, nil), protocol.Settings{DefaultCount: 500}, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	if _, err := trigram.Search(context.Background(), plan, ix.Repos(), 0, func(item protocol.ResultItem) {
		paths = append(paths, item.RepoID+"/"+item.Path)
	}); err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestReadyReposServeSearchesWhileAnotherIsIndexing(t *testing.T) {
	// @covers failure:repo-indexing
	ready := writeFiles(t, map[string]string{"a.txt": "needle\n"})
	slow := writeFiles(t, map[string]string{"b.txt": "needle\n"})
	release := make(chan struct{})
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.mu.Lock()
	ix.beforeBuild = func(id string) {
		if id == "slow" {
			<-release
		}
	}
	ix.mu.Unlock()
	ix.SetRoots([]protocol.Root{{ID: "ready", Path: ready, Name: "ready"}, {ID: "slow", Path: slow, Name: "slow"}})

	waitUntil(t, "slow to be indexing after ready is done", func() bool {
		statuses := ix.Status()
		return statuses[0].Tree == protocol.IndexStateReady && statuses[1].Tree == protocol.IndexStateIndexing
	})
	if got := searchPaths(t, ix, "type:code needle"); len(got) != 1 || got[0] != "ready/a.txt" {
		t.Errorf("results while slow indexes = %q, want ready/a.txt alone", got)
	}
	close(release)
	waitFor(t, ix, protocol.IndexStateReady)
	if got := searchPaths(t, ix, "type:code needle"); len(got) != 2 {
		t.Errorf("results once both are ready = %q, want both files", got)
	}
}

func TestSymbolsAreIndexedOnlyWhenTheSettingIsOn(t *testing.T) {
	// @covers setting:index.symbols
	root := writeFiles(t, map[string]string{"a.go": "package a\n\nfunc Retry() {}\n"})
	settings := testSettings(t)
	settings.Symbols = false
	ix := New(settings, nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)
	if got := ix.Repos()[0].Shard.Docs[0].Symbols; len(got) != 0 {
		t.Errorf("symbols with index.symbols off = %+v, want none", got)
	}

	settings.Symbols = true
	ix.SetSettings(settings)
	waitUntil(t, "a rebuild that finds Retry", func() bool {
		symbols := ix.Repos()[0].Shard.Docs[0].Symbols
		return len(symbols) == 1 && symbols[0].Name == "Retry"
	})
}

func TestIndexesAreSavedUnderTheIndexLocation(t *testing.T) {
	// @covers setting:index.location
	root := writeFiles(t, map[string]string{"a.txt": "alpha\n"})
	settings := testSettings(t)
	ix := New(settings, nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)
	saved, err := filepath.Glob(filepath.Join(settings.Location, "tree", "*.shard"))
	if err != nil || len(saved) != 1 {
		t.Errorf("shards under %s = %q, want one", settings.Location, saved)
	}
}

func TestHistoryDepthDecidesHowFarBackHistoryIsRead(t *testing.T) {
	// @covers setting:index.historyDepth
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := writeFiles(t, map[string]string{"a.txt": "old\n"})
	git := func(at time.Time, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
		date := at.Format(time.RFC3339)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date,
			"GIT_AUTHOR_NAME=Ada", "GIT_AUTHOR_EMAIL=ada@example.com", "GIT_COMMITTER_NAME=Ada", "GIT_COMMITTER_EMAIL=ada@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	now := time.Now()
	git(now, "init", "--quiet")
	git(now, "add", "--all")
	git(now.AddDate(-3, 0, 0), "commit", "--quiet", "-m", "old change")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(now.AddDate(0, -1, 0), "commit", "--quiet", "-am", "recent change")

	settings := testSettings(t)
	settings.HistoryDepth = "2y"
	ix := New(settings, nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)
	if got := ix.HistoryRepos()[0].Store.Commits(); got != 1 {
		t.Errorf("commits of the last 2 years = %d, want the recent one only", got)
	}

	settings.HistoryDepth = "all"
	ix.SetSettings(settings)
	waitUntil(t, "all history to be read", func() bool {
		repos := ix.HistoryRepos()
		return len(repos) == 1 && repos[0].Store.Commits() == 2
	})
}
