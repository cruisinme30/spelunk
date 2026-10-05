package trigram

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// paths lists the paths of files, in order.
func paths(files []File) []string {
	out := []string{}
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

var treeFiles = map[string]string{
	"main.go":           "package main\n",
	"big.txt":           "0123456789abcdef0123456789abcdef", // 32 bytes
	"logo.png":          "\x89PNG\x00\x00",
	"build/out.txt":     "generated\n",
	"src/lib/util.go":   "package lib\n",
	"vendor/dep/dep.go": "package dep\n",
	".gitignore":        "build/\n",
}

func TestListFilesOutsideGit(t *testing.T) {
	root := writeTree(t, treeFiles)
	files, err := ListFiles(context.Background(), root, WalkOptions{
		Exclude: NewExcluder([]string{"**/vendor/**"}), MaxFileBytes: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	// No Git: .gitignore is not applied. big.txt is over the size limit,
	// logo.png is binary, vendor/ is excluded.
	want := []string{".gitignore", "build/out.txt", "main.go", "src/lib/util.go"}
	if got := paths(files); !reflect.DeepEqual(got, want) {
		t.Errorf("ListFiles = %q, want %q", got, want)
	}
}

func TestListFilesInGitRespectsGitignore(t *testing.T) {
	// @covers setting:index.includeIgnored
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := writeTree(t, treeFiles)
	if out, err := exec.CommandContext(context.Background(), "git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	opts := WalkOptions{Exclude: NewExcluder([]string{"**/vendor/**"})}
	files, err := ListFiles(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "big.txt", "main.go", "src/lib/util.go"}
	if got := paths(files); !reflect.DeepEqual(got, want) {
		t.Errorf("ListFiles = %q, want %q (build/ is ignored)", got, want)
	}

	opts.IncludeIgnored = true
	files, err = ListFiles(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{".gitignore", "big.txt", "build/out.txt", "main.go", "src/lib/util.go"}
	if got := paths(files); !reflect.DeepEqual(got, want) {
		t.Errorf("ListFiles with IncludeIgnored = %q, want %q", got, want)
	}
}

// gitIn runs git in dir, failing the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestListFilesSkipsWhatIsNotARegularFile(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "alpha\n", "empty.txt": "", "locked/c.txt": "c\n", "unreadable.txt": "u\n"})
	outside := writeTree(t, map[string]string{"secret.txt": "secret\n"})
	for name, target := range map[string]string{"loop": root, "outside": outside, "outside.txt": filepath.Join(outside, "secret.txt"), "dangling": "missing"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"locked", "unreadable.txt"} {
		full := filepath.Join(root, path)
		if err := os.Chmod(full, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(full, 0o700) }) //nolint:gosec // G302: "locked" is a folder, which needs x so the temp dir can be removed
	}
	files, err := ListFiles(context.Background(), root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Unreadable entries are skipped (as root they are readable, so allow them).
	got := slices.DeleteFunc(paths(files), func(p string) bool { return p == "locked/c.txt" || p == "unreadable.txt" })
	if want := []string{"a.txt", "empty.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListFiles = %q, want %q: symlinks (to folders, files, loops or nothing) are never followed", got, want)
	}
}

func TestListFilesFollowsASymlinkedRoot(t *testing.T) {
	// /tmp on macOS is a symlink: a root reached through one is still walked.
	root := writeTree(t, map[string]string{"a.txt": "alpha\n", "src/b.txt": "beta\n"})
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	files, err := ListFiles(context.Background(), link, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.txt", "src/b.txt"}; !reflect.DeepEqual(paths(files), want) {
		t.Errorf("ListFiles(symlink to root) = %q, want %q", paths(files), want)
	}
}

func TestListFilesIncludesNestedReposAndSubmodules(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	library := writeTree(t, map[string]string{"lib.go": "package lib\n"})
	gitIn(t, library, "init", "-q")
	gitIn(t, library, "add", "-A")
	gitIn(t, library, "commit", "-q", "-m", "lib")

	root := writeTree(t, map[string]string{
		"main.go": "package main\n", ".gitignore": "*.log\n",
		"clone/.gitignore": "build/\n", "clone/x.go": "package x\n", "clone/build/out.go": "package out\n", "clone/notes.log": "log\n",
	})
	gitIn(t, root, "init", "-q")
	gitIn(t, filepath.Join(root, "clone"), "init", "-q")
	gitIn(t, root, "submodule", "add", "-q", library, "deps/lib")
	gitIn(t, root, "commit", "-q", "-m", "main")

	files, err := ListFiles(context.Background(), root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Each repo's own .gitignore applies inside it: clone/ ignores build/,
	// and *.log is ignored only by the outer repo.
	want := []string{".gitignore", ".gitmodules", "clone/.gitignore", "clone/notes.log", "clone/x.go", "deps/lib/lib.go", "main.go"}
	if got := paths(files); !reflect.DeepEqual(got, want) {
		t.Errorf("ListFiles = %q, want %q", got, want)
	}

	// Re-reading saved files asks the repo that holds each one.
	selected := SelectFiles(context.Background(), root, []string{"deps/lib/lib.go", "clone/build/out.go", "clone/x.go", "main.go", "debug.log"}, WalkOptions{})
	if want := []string{"clone/x.go", "deps/lib/lib.go", "main.go"}; !reflect.DeepEqual(paths(selected), want) {
		t.Errorf("SelectFiles = %q, want %q: a path in a submodule must not stop Git answering for the rest", paths(selected), want)
	}
}
