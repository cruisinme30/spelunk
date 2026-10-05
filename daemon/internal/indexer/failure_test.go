//go:build unix

package indexer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// importCommits makes root a Git repo with n commits, each changing one
// line, quickly (git fast-import).
func importCommits(t *testing.T, root string, n int) {
	t.Helper()
	var stream strings.Builder
	for i := range n {
		message, content := fmt.Sprintf("change %d\n", i), fmt.Sprintf("line %d\n", i)
		_, _ = fmt.Fprintf(&stream, "commit refs/heads/main\ncommitter Ada <ada@example.com> %d +0000\ndata %d\n%s", 1_700_000_000+i, len(message), message)
		_, _ = fmt.Fprintf(&stream, "M 644 inline file.txt\ndata %d\n%s\n", len(content), content)
	}
	for _, args := range [][]string{{"init", "--quiet", "--initial-branch=main"}, {"fast-import", "--quiet"}, {"reset", "--quiet", "--hard"}} {
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
		if args[0] == "fast-import" {
			cmd.Stdin = strings.NewReader(stream.String())
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// gitThatFailsLog puts a git on PATH whose log stops after some output
// and fails, as when the repo is deleted or rewritten mid-read.
func gitThatFailsLog(t *testing.T) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = log ]; then\n    %q \"$@\" | head -n 3000\n    exit 1\n  fi\ndone\nexec %q \"$@\"\n", gitPath, gitPath)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestAHistoryReadThatFailsPartWayPublishesNoPartialHistory(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	importCommits(t, root, 600)
	gitThatFailsLog(t)
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})

	status := waitFor(t, ix, protocol.IndexStateReady)[0]
	if status.History != protocol.IndexStateError {
		t.Fatalf("history state = %s (%q), want an error", status.History, status.Message)
	}
	// The newest 200 commits were published while the rest were read. Kept
	// under the HEAD the read meant to finish, they would pass for the whole
	// history, and polling would never read the rest.
	if repos := ix.HistoryRepos(); len(repos) != 0 {
		t.Errorf("HistoryRepos() after a failed read = %d commits up to %s, want no history", repos[0].Store.Commits(), repos[0].Store.Head)
	}
}
