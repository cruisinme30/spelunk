package testutil

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsolateGitKeepsFixturesOutOfTheRepoAHookRunsIn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	parent := t.TempDir()
	git(parent, "init", "--quiet")
	config := filepath.Join(parent, ".git", "config")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	// What a hook in a linked worktree hands its commands.
	t.Setenv("GIT_DIR", filepath.Join(parent, ".git"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(parent, ".git", "index"))

	IsolateGit()
	fixture := t.TempDir()
	git(fixture, "init", "--quiet")
	git(fixture, "config", "user.name", "Fixture")
	if _, err := os.Stat(filepath.Join(fixture, ".git")); err != nil {
		t.Errorf("the fixture has no repo of its own: %v", err)
	}
	if after, _ := os.ReadFile(config); !bytes.Equal(after, before) {
		t.Errorf("the parent repo's config changed:\n%s\nwant\n%s", after, before)
	}
	if _, set := os.LookupEnv("GIT_DIR"); set {
		t.Error("GIT_DIR is still set")
	}
}
