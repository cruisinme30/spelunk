package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/indexer"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/server"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

func runIndexCommand(ctx context.Context, t *testing.T, root, location string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	settings := server.DefaultSettings()
	settings.Location = location
	code = Main(ctx, Env{Args: append([]string{"index"}, args...), Cwd: root, Stdout: &out, Stderr: &errOut, Options: Options{Config: Config{Settings: settings}}})
	return code, out.String(), errOut.String()
}

func indexGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func savedIndexPath(t *testing.T, location, pattern string) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(location, pattern))
	if err != nil || len(paths) != 1 {
		t.Fatalf("saved files %q = %v, %v; want one", pattern, paths, err)
	}
	return paths[0]
}

func TestIndexBuildsRefreshesAndRebuildsWithoutEditor(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"src/a.go": "package src\nfunc Old() {}\n"})
	location := t.TempDir()
	indexGit(t, root, "init", "-q")
	indexGit(t, root, "config", "user.name", "Index Test")
	indexGit(t, root, "config", "user.email", "index@example.test")
	indexGit(t, root, "add", ".")
	indexGit(t, root, "commit", "-qm", "initial")
	if code, out, errOut := runIndexCommand(t.Context(), t, filepath.Join(root, "src"), location); code != 0 || !strings.Contains(out, root+" · files ready · history ready") {
		t.Fatalf("initial index: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	treePath := savedIndexPath(t, location, "tree/*.shard")
	historyPath := savedIndexPath(t, location, "history/*.history")
	before, err := os.Stat(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runIndexCommand(t.Context(), t, root, location); code != 0 {
		t.Fatalf("refresh: exit %d, stderr %q", code, errOut)
	}
	after, err := os.Stat(historyPath)
	if err != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("unchanged history rewritten: before %v, after %v, error %v", before, after, err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package src\nfunc New() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	indexGit(t, root, "add", ".")
	indexGit(t, root, "commit", "-qm", "new name")
	if code, _, errOut := runIndexCommand(t.Context(), t, root, location); code != 0 {
		t.Fatalf("updated index: exit %d, stderr %q", code, errOut)
	}
	assertSavedIndexes(t, treePath, historyPath)
	old := time.Unix(100000, 0)
	if err := os.Chtimes(historyPath, old, old); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runIndexCommand(t.Context(), t, root, location, "--rebuild"); code != 0 {
		t.Fatalf("forced history rebuild: exit %d, stderr %q", code, errOut)
	}
	if info, err := os.Stat(historyPath); err != nil || !info.ModTime().After(old) {
		t.Fatalf("forced rebuild did not save history: %v, %v", info, err)
	}
	if err := os.WriteFile(historyPath, []byte("broken history snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runIndexCommand(t.Context(), t, root, location, "--rebuild"); code != 0 {
		t.Fatalf("rebuild: exit %d, stderr %q", code, errOut)
	}
	assertSavedIndexes(t, treePath, historyPath)
}

func assertSavedIndexes(t *testing.T, treePath, historyPath string) {
	t.Helper()
	shard, err := trigram.Load(treePath)
	if err != nil || len(shard.Docs) != 1 || !bytes.Contains(shard.Docs[0].Content, []byte("func New()")) {
		t.Fatalf("saved tree does not include the latest source: %v, %v", shard, err)
	}
	store, err := history.Load(historyPath)
	if err != nil || store.Commits() != 2 {
		t.Fatalf("saved history = %v, %v; want both commits", store, err)
	}
}

func TestIndexRejectsAnotherWriter(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha"})
	settings := server.DefaultSettings()
	settings.Location = t.TempDir()
	writer := indexer.New(settings, nil)
	defer writer.Close(time.Second)
	writer.SetRoots([]protocol.Root{{ID: root, Name: "test", Path: root}})
	for _, args := range [][]string{nil, {"--rebuild"}} {
		code, out, errOut := runIndexCommand(t.Context(), t, root, settings.Location, args...)
		if code != exitError || out != "" || !strings.Contains(errOut, "another Spelunk process") {
			t.Errorf("locked index %v: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
}

func TestIndexPlainFolderAndFailures(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha"})
	location := t.TempDir()
	if code, out, errOut := runIndexCommand(t.Context(), t, filepath.Dir(root), location, "--in", filepath.Base(root)); code != 0 || !strings.Contains(out, "history off") {
		t.Fatalf("plain folder: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	for _, args := range [][]string{{"--in"}, {"--in", "missing"}, {"--unknown"}} {
		if code, out, errOut := runIndexCommand(t.Context(), t, root, location, args...); code != exitError || out != "" || errOut == "" {
			t.Errorf("invalid options %v: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code, out, errOut := runIndexCommand(ctx, t, root, location); code != exitError || out != "" || !strings.Contains(errOut, "context canceled") {
		t.Errorf("canceled index: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	badLocation := filepath.Join(root, "a.txt", "index")
	if code, out, errOut := runIndexCommand(t.Context(), t, root, badLocation); code != exitError || out != "" || !strings.Contains(errOut, "write lock") {
		t.Errorf("unwritable index: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestIndexReportsSaveFailure(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha"})
	location := t.TempDir()
	if err := os.WriteFile(filepath.Join(location, "tree"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runIndexCommand(t.Context(), t, root, location); code != exitError || out != "" || !strings.Contains(errOut, "Index not saved") {
		t.Errorf("failed save: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestIndexDoesNotWriteWithoutLock(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha"})
	location := t.TempDir()
	if err := os.WriteFile(filepath.Join(location, "locks"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--rebuild"}} {
		if code, _, errOut := runIndexCommand(t.Context(), t, root, location, args...); code != exitError || !strings.Contains(errOut, "write lock") {
			t.Errorf("missing lock %v: exit %d, stderr %q", args, code, errOut)
		}
	}
	if _, err := os.Stat(filepath.Join(location, "tree")); !os.IsNotExist(err) {
		t.Errorf("working-tree index written without lock: %v", err)
	}
}
