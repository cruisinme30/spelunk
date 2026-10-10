package indexer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// heard is one call to the OnNewCommits listener.
type heard struct {
	repo    history.Repo
	subject []string
}

// commitFile writes a file in root and commits it with message.
func commitFile(t *testing.T, root, path, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(message+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "--all"}, {"commit", "--quiet", "-m", message}} {
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestAHistoryUpdateAnnouncesTheCommitsItBroughtIn(t *testing.T) {
	// @covers screen:query-watches
	root := testutil.WriteTree(t, map[string]string{"main.go": "package main\n"})
	gitInit(t, root)
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	calls := make(chan heard, 4)
	ix.OnNewCommits(func(repo history.Repo, commits []history.Commit) {
		subjects := make([]string, len(commits))
		for i := range commits {
			subjects[i] = commits[i].Subject
		}
		calls <- heard{repo: repo, subject: subjects}
	})
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)
	select {
	case call := <-calls:
		t.Fatalf("the first read announced %v; it should announce nothing", call.subject)
	default:
	}

	commitFile(t, root, "ledger.go", "Revert ledger batching")
	commitFile(t, root, "refunds.go", "Revert refunds retry")
	ix.queueMovedHeads(context.Background())
	select {
	case call := <-calls:
		if call.repo.ID != "r1" || call.repo.Name != "app" || call.repo.Root != root {
			t.Errorf("announced repo = %+v, want r1 (app) at %s", call.repo, root)
		}
		if call.repo.Store == nil || call.repo.Store.Commits() != 3 {
			t.Errorf("announced store should be the updated one, with all 3 commits")
		}
		if want := []string{"Revert refunds retry", "Revert ledger batching"}; !slices.Equal(call.subject, want) {
			t.Errorf("announced commits = %q, want %q", call.subject, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the update after two new commits announced nothing")
	}
}
