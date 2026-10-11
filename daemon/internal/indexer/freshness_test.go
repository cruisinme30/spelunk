package indexer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// gitInit makes root a Git repo with one commit of everything in it.
func gitInit(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "ada@example.com"},
		{"config", "user.name", "Ada"},
		{"config", "commit.gpgsign", "false"},
		{"add", "--all"},
		{"commit", "--quiet", "-m", "first commit"},
	} {
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// overlayPaths lists the paths the repo's overlay holds, and the shard
// paths it masks.
func overlayPaths(t *testing.T, ix *Indexer) (docs, masked []string) {
	t.Helper()
	repos := ix.Repos()
	if len(repos) != 1 {
		t.Fatalf("Repos() = %+v, want one", repos)
	}
	if repos[0].Overlay != nil {
		for _, d := range repos[0].Overlay.Docs {
			docs = append(docs, d.Path)
		}
	}
	for path := range repos[0].Masked {
		masked = append(masked, path)
	}
	slices.Sort(masked)
	return docs, masked
}

func TestHistoryIsReadForGitRepos(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"main.go": "package main\n"})
	gitInit(t, root)
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})

	statuses := waitFor(t, ix, protocol.IndexStateReady)
	if statuses[0].History != protocol.IndexStateReady {
		t.Fatalf("history state = %s (%q), want ready", statuses[0].History, statuses[0].Message)
	}
	repos := ix.HistoryRepos()
	if len(repos) != 1 || repos[0].Store.Commits() != 1 {
		t.Fatalf("HistoryRepos() = %+v, want app with its one commit", repos)
	}
	commit, _, ok := ix.Repos()[0].History.LastCommit("main.go")
	if !ok || commit.Author != "Ada" {
		t.Errorf("LastCommit(main.go) = %+v, %v; want Ada's commit", commit, ok)
	}
}

// is:changed follows each save: undoing an edit, as a stash or a
// checkout of the file does, makes the file unchanged again.
func TestASaveThatUndoesAnEditLeavesTheFileUnchanged(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"main.go": "package main\n"})
	gitInit(t, root)
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)

	save := func(content string) bool {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		ix.FilesChanged([]protocol.FileChange{{Path: filepath.Join(root, "main.go")}})
		return ix.HistoryRepos()[0].Store.Dirty("main.go")
	}
	if !save("package main // edited\n") {
		t.Error("after an edit, main.go isn't changed")
	}
	if save("package main\n") {
		t.Error("after undoing the edit, main.go is still changed")
	}
}

func TestSavedFilesGoIntoTheOverlayUntilTheNextBuild(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n", "old/b.txt": "beta\n"})
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "c.txt"), []byte("gamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	ix.FilesChanged([]protocol.FileChange{
		{Path: filepath.Join(root, "a.txt")},
		{Path: filepath.Join(root, "c.txt")},
		{Path: filepath.Join(root, "old")},
		{Path: filepath.Join(t.TempDir(), "elsewhere.txt")}, // outside every root: ignored
	})
	docs, masked := overlayPaths(t, ix)
	slices.Sort(docs)
	if !slices.Equal(docs, []string{"a.txt", "c.txt"}) {
		t.Errorf("overlay docs = %q, want the saved a.txt and the new c.txt", docs)
	}
	if !slices.Equal(masked, []string{"a.txt", "c.txt", "old", "old/b.txt"}) {
		t.Errorf("masked = %q, want the changed paths and the deleted folder's file", masked)
	}

	if err := ix.Rebuild("r1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ix, protocol.IndexStateReady)
	docs, masked = overlayPaths(t, ix)
	if len(docs) != 0 || len(masked) != 0 {
		t.Errorf("overlay after a rebuild = %q masking %q, want it folded into the shard", docs, masked)
	}
	if got := docPaths(ix.Repos()[0]); !slices.Equal(got, []string{"a.txt", "c.txt"}) {
		t.Errorf("rebuilt docs = %q, want [a.txt c.txt]", got)
	}
}

func TestACorruptSavedIndexIsRebuilt(t *testing.T) {
	// @covers failure:index-corrupt
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := testSettings(t)
	path := shardPath(settings.Location, protocol.Root{Path: root})
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a shard"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	ix := New(settings, rec.notify)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	if status := ix.Status()[0]; status.Tree == protocol.IndexStateReady || !strings.Contains(status.Message, "couldn't be read") {
		t.Errorf("status with a corrupt index = %+v, want queued or indexing, saying it is rebuilt", status)
	}
	waitFor(t, ix, protocol.IndexStateReady)
	if !rec.sawState("r1", protocol.IndexStateIndexing) {
		t.Errorf("a corrupt index was never shown as indexing")
	}
	if status := ix.Status()[0]; strings.Contains(status.Message, "couldn't be read") {
		t.Errorf("message after the rebuild = %q, want the notice gone", status.Message)
	}
	if got := docPaths(ix.Repos()[0]); !slices.Equal(got, []string{"a.txt"}) {
		t.Errorf("rebuilt docs = %q, want [a.txt]", got)
	}
}

func TestAnIndexThatCannotBeSavedStillServes(t *testing.T) {
	// A file where the index folder should be makes every save fail, as a
	// full disk does: the repo keeps serving this session and says so.
	// @covers failure:disk-full
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n"})
	settings := testSettings(t)
	blocked := filepath.Join(settings.Location, "blocked")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	settings.Location = blocked
	ix := New(settings, nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})

	status := waitFor(t, ix, protocol.IndexStateReady)[0]
	if !strings.HasPrefix(status.Message, "Index not saved: ") {
		t.Errorf("status message = %q, want a one-line warning that the index wasn't saved", status.Message)
	}
	if got := docPaths(ix.Repos()[0]); !slices.Equal(got, []string{"a.txt"}) {
		t.Errorf("docs = %q, want [a.txt] served from memory", got)
	}
}

func TestDepthStart(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	tests := map[string]time.Time{
		"6m":  now.AddDate(0, -6, 0),
		"2y":  now.AddDate(-2, 0, 0),
		"all": {},
		"":    {},
	}
	for depth, want := range tests {
		if got := depthStart(depth, now); !got.Equal(want) {
			t.Errorf("depthStart(%q) = %v, want %v", depth, got, want)
		}
	}
}

// A history saved under another index.historyDepth serves the next session
// at once but is read again under the new window, though HEAD hasn't moved:
// a wider window brings in commits the saved one never read.
func TestAHistorySavedUnderAnotherDepthIsReadAgain(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"old.go": "package old\n"})
	gitInit(t, root)
	old := time.Now().AddDate(-3, 0, 0).Format(time.RFC3339)
	cmd := exec.CommandContext(context.Background(), "git", "-C", root, "commit", "--quiet", "--amend", "--no-edit", "--date="+old)
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE="+old)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit --amend: %v\n%s", err, out)
	}
	testutil.WriteFiles(t, root, map[string]string{"new.go": "package new\n"})
	for _, args := range [][]string{{"add", "--all"}, {"commit", "--quiet", "-m", "second commit"}} {
		if out, err := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	settings := testSettings(t)
	settings.HistoryDepth = "2y"
	first := New(settings, nil)
	first.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, first, protocol.IndexStateReady)
	if got := first.HistoryRepos()[0].Store.Commits(); got != 1 {
		t.Fatalf("with 2y of history: %d commits, want the one from the last 2 years", got)
	}
	first.Close(time.Second)

	settings.HistoryDepth = "all"
	second := New(settings, nil)
	defer second.Close(time.Second)
	second.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	deadline := time.Now().Add(5 * time.Second)
	for second.HistoryRepos()[0].Store.Commits() != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("with all history: %d commits, want both", second.HistoryRepos()[0].Store.Commits())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A folder opened outside Git that becomes a repo later (git init, or a
// clone into it) gets its history and .gitignore without a restart, and
// one that stops being a repo turns history off.
func TestAFolderThatBecomesAGitRepoIsNoticed(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"main.go": "package main\n", ".gitignore": "build/\n", "build/out.go": "package out\n"})
	settings := testSettings(t)
	settings.CommitCheckSeconds = 1
	ix := New(settings, nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	if statuses := waitFor(t, ix, protocol.IndexStateReady); statuses[0].History != protocol.IndexStateOff {
		t.Fatalf("history outside Git = %s, want off", statuses[0].History)
	}
	gitInit(t, root)
	eventually(t, "the new repo's history is read and build/ is ignored", func() bool {
		repos := ix.HistoryRepos()
		return len(repos) == 1 && repos[0].Store.Commits() == 1 && !slices.Contains(docPaths(ix.Repos()[0]), "build/out.go")
	})
	if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "history is off once the folder is no repo", func() bool {
		return len(ix.HistoryRepos()) == 0 && ix.Status()[0].History == protocol.IndexStateOff
	})
}

// eventually polls done for a few seconds, failing the test with what it
// waited for when it never holds.
func eventually(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestASavedHistoryKnowsTheUncommittedFilesAtOnce(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"main.go": "package main\n"})
	gitInit(t, root)
	location := t.TempDir()
	rootInfo := protocol.Root{ID: "r1", Path: root, Name: "r"}
	store, err := history.Ingest(context.Background(), root, history.Options{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(historyPath(location, rootInfo)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(historyPath(location, rootInfo)); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFiles(t, root, map[string]string{"main.go": "package main // edited\n"})
	saved := loadSaved(context.Background(), location, nil, rootInfo, false, true)
	if saved.history == nil || !saved.history.Dirty("main.go") {
		t.Fatalf("a loaded history says main.go has no uncommitted change; is:changed would find nothing until the next update")
	}
}

func TestAWindowCatchesUpOnFilesChangedWhereTheEditorIsNotLooking(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n", "old/b.txt": "beta\n"})
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)

	// Changed on disk with no word from the editor, as in a folder
	// files.watcherExclude leaves out, or while VS Code was closed.
	later := time.Now().Add(time.Minute)
	testutil.WriteFiles(t, root, map[string]string{"a.txt": "alpha two\n", "c.txt": "gamma\n"})
	for _, path := range []string{"a.txt", "c.txt"} {
		if err := os.Chtimes(filepath.Join(root, path), later, later); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	ix.sweepWriters(context.Background())
	docs, masked := overlayPaths(t, ix)
	slices.Sort(docs)
	slices.Sort(masked)
	if !slices.Equal(docs, []string{"a.txt", "c.txt"}) || !slices.Equal(masked, []string{"a.txt", "c.txt", "old/b.txt"}) {
		t.Errorf("after a sweep, overlay = %q masking %q, want a.txt and c.txt re-read and old/b.txt gone", docs, masked)
	}
}
