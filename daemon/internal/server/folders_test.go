package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// explainFolder asks folder/explain about one folder of r1.
func explainFolder(t *testing.T, client *testClient, path string) protocol.FolderExplanation {
	t.Helper()
	var explanation protocol.FolderExplanation
	if err := client.call(protocol.MethodFolderExplain, protocol.FolderExplainParams{RepoID: "r1", Path: path}, &explanation); err != nil {
		t.Fatalf("folder/explain %q: %v", path, err)
	}
	return explanation
}

// Each reason a folder's files go unsearched, and a Git repo cloned inside
// the open one, as Search in Folder's notes name them.
func TestFolderExplainSaysWhyAFolderIsSkipped(t *testing.T) {
	// @covers rpc:folder/explain screen:search-in-folder
	root := gitRepo(t, gitCommit{author: "Jane", subject: "Initial", at: time.Now(), files: map[string]string{
		".gitignore":          "dist/\n",
		"src/api/checkout.ts": "const retry = 1\n",
		"README.md":           "retry\n",
	}})
	testutil.WriteFiles(t, root, map[string]string{
		"dist/bundle.js":                  "retry()\n",
		"node_modules/left-pad/index.js":  "retry()\n",
		"plugins/stripe-adapter/retry.py": "def retry(): pass\n",
	})
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(root, "plugins", "stripe-adapter")
	for _, args := range [][]string{
		{"init", "--quiet"}, {"add", "--all"},
		{"-c", "user.name=Ana", "-c", "user.email=ana@example.com", "commit", "--quiet", "-m", "Add retry"},
	} {
		if out, err := exec.CommandContext(context.Background(), "git", append([]string{"-C", inner}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "web-checkout"})

	for _, tc := range []struct {
		path string
		want protocol.FolderExplanation
	}{
		{"src/api", protocol.FolderExplanation{}},
		{"src/api/", protocol.FolderExplanation{}},
		{"", protocol.FolderExplanation{}},
		{"dist", protocol.FolderExplanation{Skipped: "ignored"}},
		{"node_modules", protocol.FolderExplanation{Skipped: "excluded"}},
		{"node_modules/left-pad", protocol.FolderExplanation{Skipped: "excluded"}},
		{"linked", protocol.FolderExplanation{Skipped: "link"}},
		{"empty", protocol.FolderExplanation{Skipped: "empty"}},
		{"plugins/stripe-adapter", protocol.FolderExplanation{GitRepo: "plugins/stripe-adapter"}},
		{"plugins", protocol.FolderExplanation{}},
	} {
		if got := explainFolder(t, client, tc.path); got != tc.want {
			t.Errorf("folder/explain %q = %+v, want %+v", tc.path, got, tc.want)
		}
	}
}

// A path that leaves the repo is refused, and a closed repo is stale.
func TestFolderExplainRefusesPathsOutsideTheRepo(t *testing.T) {
	// @covers rpc:folder/explain failure:ref-stale
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "retry\n")
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "a"})
	for _, path := range []string{"../elsewhere", "/etc"} {
		err := client.call(protocol.MethodFolderExplain, protocol.FolderExplainParams{RepoID: "r1", Path: path}, nil)
		wantRPCCode(t, "folder/explain "+path, err, rpc.CodeInvalidParams)
	}
	err := client.call(protocol.MethodFolderExplain, protocol.FolderExplainParams{RepoID: "gone", Path: "src"}, nil)
	wantRPCCode(t, "folder/explain on a closed repo", err, protocol.CodeRefStale)
}
