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

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
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
	root := writeFiles(t, map[string]string{"main.go": "package main\n"})
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

func TestSavedFilesGoIntoTheOverlayUntilTheNextBuild(t *testing.T) {
	root := writeFiles(t, map[string]string{"a.txt": "alpha\n", "old/b.txt": "beta\n"})
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
	root := writeFiles(t, map[string]string{"a.txt": "alpha\n"})
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
	root := writeFiles(t, map[string]string{"a.txt": "alpha\n"})
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
