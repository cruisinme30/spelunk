package permalink

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.IsolateGit()
	os.Exit(m.Run())
}

// clientPy is the file the tests link to; line 5 has the match.
const clientPy = "import time\n\nclass Client:\n    def charge(self):\n        for attempt in range(3):\n            pass\n"

// repo is a Git repo with one commit of clientPy, its main branch pushed
// to origin (git@github.com:acme/payments-api.git) as far as the
// remote-tracking branch knows. run runs git in it.
func repo(t *testing.T) (root string, run func(args ...string) string) {
	t.Helper()
	root = t.TempDir()
	run = func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Dana", "GIT_AUTHOR_EMAIL=dana@acme.dev",
			"GIT_COMMITTER_NAME=Dana", "GIT_COMMITTER_EMAIL=dana@acme.dev")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "--quiet", "-b", "main")
	testutil.WriteFiles(t, root, map[string]string{"src/client.py": clientPy})
	run("add", "-A")
	run("commit", "--quiet", "-m", "Add the client")
	run("remote", "add", "origin", "git@github.com:acme/payments-api.git")
	push(run)
	return root, run
}

// push moves origin/main to HEAD, as a push would, without a network.
func push(run func(args ...string) string) {
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	run("branch", "--quiet", "--set-upstream-to=origin/main", "main")
}

func write(t *testing.T, root, path, text string) {
	t.Helper()
	testutil.WriteFiles(t, root, map[string]string{path: text})
}

func linkTo(t *testing.T, file File) protocol.Link {
	t.Helper()
	link, err := ForFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ForFile: %v", err)
	}
	return link
}

// A pushed, unedited file links to HEAD at the same line, on the
// preferred remote.
func TestLinksAPushedLineToHEAD(t *testing.T) {
	root, run := repo(t)
	link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 5})
	if link.Problem != "" || link.Commit != run("rev-parse", "HEAD") || link.Path != "src/client.py" || link.Line != 5 {
		t.Fatalf("link = %+v", link)
	}
	if link.Remote == nil || link.Remote.Name != "origin" || link.Remote.URL != "git@github.com:acme/payments-api.git" {
		t.Fatalf("remote = %+v", link.Remote)
	}
}

// Lines added above the match (not committed) move it down on disk; the
// link points at the line it is on in HEAD. A file-name result links to
// the file with no line.
func TestMapsALineMovedByEditsAboveIt(t *testing.T) {
	root, _ := repo(t)
	write(t, root, "src/client.py", "# Charges retry.\n# See RetryPolicy.\n"+clientPy)
	if link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 7}); link.Problem != "" || link.Line != 5 {
		t.Fatalf("moved line: %+v", link)
	}
	if link := linkTo(t, File{Root: root, Path: "src/client.py"}); link.Problem != "" || link.Line != 0 {
		t.Fatalf("file name: %+v", link)
	}
}

// A match whose own line isn't on the remote yet has no link; neither
// does one spanning lines, when any of them changed.
func TestRefusesALineTheRemoteLacks(t *testing.T) {
	root, _ := repo(t)
	write(t, root, "src/client.py", strings.Replace(clientPy, "range(3)", "range(5)", 1))
	if link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 5}); link.Problem != protocol.LinkProblemLineChanged {
		t.Fatalf("changed line: %+v", link)
	}
	if link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 4, EndLine: 5}); link.Problem != protocol.LinkProblemLineChanged {
		t.Fatalf("span over a changed line: %+v", link)
	}
	if link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 3, EndLine: 4}); link.Problem != "" || link.EndLine != 4 {
		t.Fatalf("span above it: %+v", link)
	}
}

// Commits that aren't pushed: the link goes to the newest commit origin
// has, with the lines mapped through the unpushed commits too.
func TestLinksPastUnpushedCommitsToTheRemotesCommit(t *testing.T) {
	root, run := repo(t)
	pushed := run("rev-parse", "HEAD")
	write(t, root, "src/client.py", "# Charges retry.\n"+clientPy)
	run("commit", "--quiet", "-am", "Explain")
	link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 6})
	if link.Problem != "" || link.Commit != pushed || link.Line != 5 {
		t.Fatalf("link = %+v, want line 5 at %s", link, pushed)
	}
}

// A repo the remote has none of has no links.
func TestRefusesARepoNeverPushed(t *testing.T) {
	root, run := repo(t)
	run("update-ref", "-d", "refs/remotes/origin/main")
	if link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 5}); link.Problem != protocol.LinkProblemNotPushed {
		t.Fatalf("link = %+v", link)
	}
}

// A file added since (or never committed) isn't on the remote; one renamed
// since links to its old path.
func TestFollowsARenameAndRefusesANewFile(t *testing.T) {
	root, run := repo(t)
	write(t, root, "src/new.py", "x = 1\n")
	if link := linkTo(t, File{Root: root, Path: "src/new.py", Line: 1}); link.Problem != protocol.LinkProblemNotOnRemote {
		t.Fatalf("new file: %+v", link)
	}
	run("mv", "src/client.py", "src/payments_client.py")
	link := linkTo(t, File{Root: root, Path: "src/payments_client.py", Line: 5})
	if link.Problem != "" || link.Path != "src/client.py" || link.Line != 5 {
		t.Fatalf("renamed: %+v", link)
	}
}

// A workspace folder inside a bigger repo links with the folder's path in
// the repo in front of the file's.
func TestPrefixesTheFolderInsideABiggerRepo(t *testing.T) {
	root, run := repo(t)
	write(t, root, "services/api/main.py", "print(1)\n")
	run("add", "-A")
	run("commit", "--quiet", "-m", "Add a service")
	push(run)
	link := linkTo(t, File{Root: filepath.Join(root, "services", "api"), Path: "main.py", Line: 1})
	if link.Problem != "" || link.Path != "services/api/main.py" {
		t.Fatalf("link = %+v", link)
	}
}

// An at: result's file is the revision's: it links there, unmapped, when
// the remote has the revision.
func TestLinksAResultAtARevisionToThatRevision(t *testing.T) {
	root, run := repo(t)
	old := run("rev-parse", "HEAD")
	write(t, root, "src/client.py", "# new\n"+clientPy)
	run("commit", "--quiet", "-am", "Later")
	push(run)
	link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 5, Revision: old})
	if link.Problem != "" || link.Commit != old || link.Line != 5 {
		t.Fatalf("link = %+v", link)
	}
	local := run("commit-tree", "-p", "HEAD", "-m", "dangling", run("rev-parse", "HEAD^{tree}"))
	if link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 5, Revision: local}); link.Problem != protocol.LinkProblemNotPushed {
		t.Fatalf("unpushed revision: %+v", link)
	}
}

// A commit result links to itself once the remote has it.
func TestLinksACommitOnlyOnceItIsPushed(t *testing.T) {
	root, run := repo(t)
	pushed := run("rev-parse", "HEAD")
	write(t, root, "src/client.py", "# local\n"+clientPy)
	run("commit", "--quiet", "-am", "Local")
	local := run("rev-parse", "HEAD")
	if link, err := ForCommit(context.Background(), root, pushed, ""); err != nil || link.Commit != pushed {
		t.Fatalf("pushed: %+v %v", link, err)
	}
	if link, _ := ForCommit(context.Background(), root, local, ""); link.Problem != protocol.LinkProblemNotPushed {
		t.Fatalf("local: %+v", link)
	}
}

// Which remote links go to: the tracked one, else origin, else the only
// one; with several and none of those, the user picks.
func TestPicksTheRemote(t *testing.T) {
	ctx := context.Background()
	root, run := repo(t)
	run("remote", "add", "fork", "git@github.com:dana-fork/payments-api.git")
	if remotes, _ := Remotes(ctx, root); remotes.Preferred != "origin" || len(remotes.Remotes) != 2 {
		t.Fatalf("tracked origin: %+v", remotes)
	}
	run("update-ref", "refs/remotes/fork/main", "HEAD")
	run("branch", "--quiet", "--set-upstream-to=fork/main", "main")
	if remotes, _ := Remotes(ctx, root); remotes.Preferred != "fork" {
		t.Fatalf("tracked fork: %+v", remotes)
	}
	run("branch", "--quiet", "--unset-upstream")
	run("remote", "rename", "origin", "upstream")
	remotes, _ := Remotes(ctx, root)
	if remotes.Preferred != "" {
		t.Fatalf("two remotes, neither tracked: %+v", remotes)
	}
	link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 5})
	if link.Problem != protocol.LinkProblemPickRemote || len(link.Remotes) != 2 {
		t.Fatalf("link = %+v", link)
	}
	if link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 5, Remote: "upstream"}); link.Problem != "" || link.Remote.Name != "upstream" {
		t.Fatalf("picked upstream: %+v", link)
	}
	run("remote", "remove", "fork")
	if remotes, _ := Remotes(ctx, root); remotes.Preferred != "upstream" {
		t.Fatalf("the only remote: %+v", remotes)
	}
}

// A folder outside Git, and a repo without remotes, have no links.
func TestRefusesNoGitAndNoRemote(t *testing.T) {
	plain := t.TempDir()
	write(t, plain, "a.txt", "a\n")
	if link := linkTo(t, File{Root: plain, Path: "a.txt", Line: 1}); link.Problem != protocol.LinkProblemNotGit {
		t.Fatalf("no git: %+v", link)
	}
	root, run := repo(t)
	run("remote", "remove", "origin")
	if link := linkTo(t, File{Root: root, Path: "src/client.py", Line: 5}); link.Problem != protocol.LinkProblemNoRemote {
		t.Fatalf("no remote: %+v", link)
	}
}

func TestMapLine(t *testing.T) {
	// Lines 3–4 replaced by one line at 3; two lines inserted after line 8 (now 8–9 → 9–10).
	hunks := []hunk{{3, 2, 3, 1}, {8, 0, 9, 2}}
	for now, want := range map[int]int{1: 1, 2: 2, 4: 5, 8: 9, 11: 10} {
		if got, ok := mapLine(hunks, now); !ok || got != want {
			t.Errorf("mapLine(%d) = %d, %v; want %d", now, got, ok, want)
		}
	}
	for _, changed := range []int{3, 9, 10} {
		if _, ok := mapLine(hunks, changed); ok {
			t.Errorf("mapLine(%d) mapped a changed line", changed)
		}
	}
	// Lines removed after line 4 (no new lines): 5 onward move up.
	if got, ok := mapLine([]hunk{{5, 3, 4, 0}}, 5); !ok || got != 8 {
		t.Errorf("after a removal: %d, %v", got, ok)
	}
}
