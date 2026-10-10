package indexer

// Tests that each repo's CODEOWNERS file is read with its index, and again
// when the editor changes it, for owner:.

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// ownersOf returns who the indexer's one repo says owns path, and whether
// it has a CODEOWNERS file at all.
func ownersOf(t *testing.T, ix *Indexer, path string) (owners []string, hasFile bool) {
	t.Helper()
	repos := ix.Repos()
	if len(repos) != 1 {
		t.Fatalf("Repos() = %+v, want one", repos)
	}
	if repos[0].Owners == nil {
		return nil, false
	}
	return repos[0].Owners.Owners(path), true
}

func TestCodeOwnersIsReadWithTheIndexAndWhenSaved(t *testing.T) {
	// @covers op:owner
	root := testutil.WriteTree(t, map[string]string{
		".github/CODEOWNERS": "* @acme/platform\n",
		"src/retry.py":       "retry = 3\n",
	})
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)
	if got, _ := ownersOf(t, ix, "src/retry.py"); !slices.Equal(got, []string{"@acme/platform"}) {
		t.Fatalf("owners after the build = %q, want @acme/platform", got)
	}

	codeOwners := filepath.Join(root, ".github", "CODEOWNERS")
	if err := os.WriteFile(codeOwners, []byte("* @acme/platform\n/src/ @jane\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix.FilesChanged([]protocol.FileChange{{Path: codeOwners}})
	if got, _ := ownersOf(t, ix, "src/retry.py"); !slices.Equal(got, []string{"@jane"}) {
		t.Errorf("owners after saving CODEOWNERS = %q, want @jane", got)
	}

	// Saving another file doesn't read CODEOWNERS again.
	if err := os.WriteFile(codeOwners, []byte("* @someone-else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix.FilesChanged([]protocol.FileChange{{Path: filepath.Join(root, "src", "retry.py")}})
	if got, _ := ownersOf(t, ix, "src/retry.py"); !slices.Equal(got, []string{"@jane"}) {
		t.Errorf("owners after saving another file = %q, want @jane still", got)
	}

	// Deleting the folder that holds it leaves the repo without owners.
	if err := os.RemoveAll(filepath.Join(root, ".github")); err != nil {
		t.Fatal(err)
	}
	ix.FilesChanged([]protocol.FileChange{{Path: filepath.Join(root, ".github")}})
	if _, hasFile := ownersOf(t, ix, "src/retry.py"); hasFile {
		t.Error("after deleting .github the repo still has a CODEOWNERS file")
	}

	// A CODEOWNERS file at the root, the next place GitHub looks, counts.
	if err := os.WriteFile(filepath.Join(root, "CODEOWNERS"), []byte("*.py @py-team\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix.FilesChanged([]protocol.FileChange{{Path: filepath.Join(root, "CODEOWNERS")}})
	if got, _ := ownersOf(t, ix, "src/retry.py"); !slices.Equal(got, []string{"@py-team"}) {
		t.Errorf("owners from the root CODEOWNERS = %q, want @py-team", got)
	}
}

func TestASavedIndexComesWithItsCodeOwners(t *testing.T) {
	// @covers op:owner
	root := testutil.WriteTree(t, map[string]string{"docs/CODEOWNERS": "* @writers\n", "a.md": "alpha\n"})
	settings := testSettings(t)
	first := New(settings, nil)
	first.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, first, protocol.IndexStateReady)
	first.Close(time.Second)

	second := New(settings, nil)
	defer second.Close(time.Second)
	second.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	// The saved shard serves at once, before any build, and so do its owners.
	if got, _ := ownersOf(t, second, "a.md"); !slices.Equal(got, []string{"@writers"}) {
		t.Errorf("owners right after SetRoots = %q, want @writers", got)
	}
}

func TestARepoWithoutCodeOwnersHasNoOwners(t *testing.T) {
	// @covers op:owner
	root := testutil.WriteTree(t, map[string]string{"src/CODEOWNERS": "* @nobody\n", "a.go": "package a\n"})
	ix := New(testSettings(t), nil)
	defer ix.Close(time.Second)
	ix.SetRoots([]protocol.Root{{ID: "r1", Path: root, Name: "app"}})
	waitFor(t, ix, protocol.IndexStateReady)
	if _, hasFile := ownersOf(t, ix, "a.go"); hasFile {
		t.Error("a CODEOWNERS file in src/ counted; GitHub only looks in .github/, the root and docs/")
	}
}
