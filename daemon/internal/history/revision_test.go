package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// revisionRepo is a repo with three commits, the first tagged v1.0.0:
// backoff.py is added, changed, then deleted.
func revisionRepo(t *testing.T) (r *testRepo, first, second, third string) {
	t.Helper()
	r = newTestRepo(t)
	first = r.commit("Ann <ann@example.com>", "add backoff", day(40), map[string]string{
		"src/backoff.py": "def parse_retry_after(header):\n    return int(header)\n",
		"src/client.py":  "import backoff\n",
		"README.md":      "payments\n",
	})
	r.git("tag", "v1.0.0")
	second = r.commit("Ann <ann@example.com>", "cap retry_after", day(20), map[string]string{
		"src/backoff.py": "def parse_retry_after(header):\n    return min(int(header), 30)\n",
	})
	r.git("rm", "--quiet", "src/backoff.py")
	third = r.commit("Bo <bo@example.com>", "drop backoff", day(2), nil)
	return r, first, second, third
}

func TestResolveRevisionFindsTagsBranchesShasAndAncestors(t *testing.T) {
	r, first, second, third := revisionRepo(t)
	ctx := context.Background()
	for name, want := range map[string]string{
		"v1.0.0": first, "main": third, "HEAD": third, "HEAD~1": second, first[:10]: first, second: second,
	} {
		got, err := ResolveRevision(ctx, r.root, name)
		if err != nil || got.SHA != want {
			t.Errorf("ResolveRevision(%q) = %v, %v; want %s", name, got, err, want)
		}
	}
	got, _ := ResolveRevision(ctx, r.root, "v1.0.0")
	if !got.At.Equal(day(40)) {
		t.Errorf("v1.0.0 is dated %v, want its commit's date %v", got.At, day(40))
	}
}

func TestResolveRevisionRefusesWhatNamesNoCommit(t *testing.T) {
	r, _, _, _ := revisionRepo(t)
	blob := r.git("rev-parse", "v1.0.0:README.md")
	for _, name := range []string{"v9", "HEAD~10", "-v", "--output=/tmp/x", "", blob, "v1.0.0:README.md"} {
		if _, err := ResolveRevision(context.Background(), r.root, name); !errors.Is(err, ErrNoRevision) {
			t.Errorf("ResolveRevision(%q) = %v, want ErrNoRevision", name, err)
		}
	}
}

func TestResolveRevisionOutsideGitIsNotGit(t *testing.T) {
	if _, err := ResolveRevision(context.Background(), t.TempDir(), "HEAD"); !errors.Is(err, ErrNotGit) {
		t.Errorf("ResolveRevision outside Git = %v, want ErrNotGit", err)
	}
}

func TestRevisionBeforeFindsTheNewestCommitThatOld(t *testing.T) {
	r, first, second, third := revisionRepo(t)
	ctx := context.Background()
	for _, tc := range []struct {
		before time.Time
		want   string
	}{
		{day(0), third},
		{day(2), third},
		{day(3), second},
		{day(30), first},
		{day(40), first},
	} {
		got, err := RevisionBefore(ctx, r.root, tc.before)
		if err != nil || got.SHA != tc.want {
			t.Errorf("RevisionBefore(%v) = %v, %v; want %s", tc.before, got, err, tc.want)
		}
	}
	if _, err := RevisionBefore(ctx, r.root, day(41)); !errors.Is(err, ErrNoRevision) {
		t.Errorf("RevisionBefore the first commit = %v, want ErrNoRevision", err)
	}
	if _, err := RevisionBefore(ctx, newTestRepo(t).root, day(0)); !errors.Is(err, ErrNoRevision) {
		t.Errorf("RevisionBefore in a repo without commits = %v, want ErrNoRevision", err)
	}
}

func TestListTreeListsRegularFilesUnderTheRoot(t *testing.T) {
	r, first, _, third := revisionRepo(t)
	ctx := context.Background()
	paths := func(files []TreeFile) []string {
		var names []string
		for _, f := range files {
			names = append(names, f.Path)
		}
		return names
	}
	files, err := ListTree(ctx, r.root, first)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(files), []string{"README.md", "src/backoff.py", "src/client.py"}; !slices.Equal(got, want) {
		t.Errorf("ListTree(v1.0.0) = %v, want %v", got, want)
	}
	if files[1].Size != int64(len("def parse_retry_after(header):\n    return int(header)\n")) {
		t.Errorf("backoff.py size = %d", files[1].Size)
	}
	files, _ = ListTree(ctx, r.root, third)
	if got, want := paths(files), []string{"README.md", "src/client.py"}; !slices.Equal(got, want) {
		t.Errorf("ListTree(HEAD) = %v, want %v", got, want)
	}
	// A root inside the repo sees its own folder, with paths relative to it.
	files, _ = ListTree(ctx, filepath.Join(r.root, "src"), first)
	if got, want := paths(files), []string{"backoff.py", "client.py"}; !slices.Equal(got, want) {
		t.Errorf("ListTree from src/ = %v, want %v", got, want)
	}
	if _, err := ListTree(ctx, r.root, "not-a-sha"); !errors.Is(err, ErrNoRevision) {
		t.Errorf("ListTree(not-a-sha) = %v, want ErrNoRevision", err)
	}
}

func TestListTreeLeavesOutSymlinksAndSubmodules(t *testing.T) {
	r := newTestRepo(t)
	r.write("real.txt", "text\n")
	if err := os.Symlink("real.txt", filepath.Join(r.root, "link.txt")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	r.git("add", "--all")
	// A submodule is a gitlink entry in the index; no other repo is needed.
	r.git("update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("ab", 20)+",vendor/lib")
	sha := r.commit("Ann <ann@example.com>", "files of every kind", day(1), nil)
	files, err := ListTree(context.Background(), r.root, sha)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "real.txt" {
		t.Errorf("ListTree = %+v, want only real.txt", files)
	}
}

func TestReadBlobsReadsEveryFileInOrder(t *testing.T) {
	r, first, _, _ := revisionRepo(t)
	ctx := context.Background()
	files, _ := ListTree(ctx, r.root, first)
	var got []string
	err := ReadBlobs(ctx, r.root, files, func(file TreeFile, content []byte) error {
		got = append(got, file.Path+"="+string(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"README.md=payments\n",
		"src/backoff.py=def parse_retry_after(header):\n    return int(header)\n",
		"src/client.py=import backoff\n",
	}
	if !slices.Equal(got, want) {
		t.Errorf("ReadBlobs = %q, want %q", got, want)
	}
}

func TestReadBlobsStopsAtTheFirstError(t *testing.T) {
	r, first, _, _ := revisionRepo(t)
	ctx := context.Background()
	files, _ := ListTree(ctx, r.root, first)
	stop := errors.New("stop")
	calls := 0
	err := ReadBlobs(ctx, r.root, files, func(TreeFile, []byte) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("ReadBlobs = %v after %d calls, want stop after 1", err, calls)
	}
}

func TestReadBlobsSkipsAMissingObject(t *testing.T) {
	r, first, _, _ := revisionRepo(t)
	ctx := context.Background()
	files, _ := ListTree(ctx, r.root, first)
	files = append([]TreeFile{{Path: "gone", OID: strings.Repeat("0", 40)}}, files...)
	var got []string
	if err := ReadBlobs(ctx, r.root, files, func(file TreeFile, _ []byte) error {
		got = append(got, file.Path)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"README.md", "src/backoff.py", "src/client.py"}; !slices.Equal(got, want) {
		t.Errorf("ReadBlobs read %v, want %v", got, want)
	}
}

func TestReadFileAtReadsOneFileAsItWas(t *testing.T) {
	r, first, second, third := revisionRepo(t)
	ctx := context.Background()
	for _, tc := range []struct{ sha, want string }{
		{first, "def parse_retry_after(header):\n    return int(header)\n"},
		{second, "def parse_retry_after(header):\n    return min(int(header), 30)\n"},
	} {
		got, err := ReadFileAt(ctx, r.root, tc.sha, "src/backoff.py")
		if err != nil || string(got) != tc.want {
			t.Errorf("ReadFileAt(%s) = %q, %v; want %q", tc.sha[:7], got, err, tc.want)
		}
	}
	got, err := ReadFileAt(ctx, filepath.Join(r.root, "src"), first, "client.py")
	if err != nil || string(got) != "import backoff\n" {
		t.Errorf("ReadFileAt from src/ = %q, %v", got, err)
	}
	for _, tc := range []struct{ sha, path string }{
		{third, "src/backoff.py"}, // deleted by then
		{first, "src"},            // a folder
		{first, "missing.txt"},
		{"HEAD", "README.md"}, // only shas are read
	} {
		if _, err := ReadFileAt(ctx, r.root, tc.sha, tc.path); !errors.Is(err, ErrStale) {
			t.Errorf("ReadFileAt(%s, %s) = %v, want ErrStale", tc.sha, tc.path, err)
		}
	}
}

func TestTagsListsTheNewestFirst(t *testing.T) {
	r, _, second, _ := revisionRepo(t)
	r.gitWithEnv([]string{"GIT_COMMITTER_DATE=" + day(19).Format(time.RFC3339)}, "tag", "-a", "-m", "release", "v1.1.0", second)
	tags, err := Tags(context.Background(), r.root, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0].Name != "v1.1.0" || tags[1].Name != "v1.0.0" {
		t.Fatalf("Tags = %+v, want v1.1.0 then v1.0.0", tags)
	}
	if !tags[0].At.Equal(day(19)) || !tags[1].At.Equal(day(40)) {
		t.Errorf("tag dates = %v, %v; want the annotated tag's date and the lightweight tag's commit date", tags[0].At, tags[1].At)
	}
	if tags, _ := Tags(context.Background(), r.root, 1); len(tags) != 1 {
		t.Errorf("Tags with limit 1 = %+v", tags)
	}
	if tags, err := Tags(context.Background(), newTestRepo(t).root, 10); err != nil || len(tags) != 0 {
		t.Errorf("Tags without tags = %+v, %v", tags, err)
	}
}
