package trigram

import (
	"context"
	"os/exec"
	"reflect"
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
