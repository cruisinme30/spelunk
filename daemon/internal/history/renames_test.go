package history

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// rename moves a file in the working tree, creating the new folders.
func (r *testRepo) rename(from, to string) {
	r.t.Helper()
	dest := filepath.Join(r.root, filepath.FromSlash(to))
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(r.root, filepath.FromSlash(from)), dest); err != nil {
		r.t.Fatal(err)
	}
}

// policy is a file long enough that a small edit keeps it over Git's 50%
// rename similarity.
const policy = "class RetryPolicy:\n    max_attempts = 3\n    base_delay = 0.5\n\n    def next_delay(self, attempt):\n        return self.base_delay * 2**attempt\n\n    def delays(self):\n        return [self.next_delay(n) for n in range(self.max_attempts)]\n"

// renamedTwice makes backoff.py, edits it, renames it to policy.py with an
// edit, renames that to retry_policy.py unchanged, then edits it again.
func renamedTwice(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	jane := "Jane Doe <jane@example.com>"
	r.commit(jane, "Add exponential backoff", day(60), map[string]string{"src/backoff.py": policy, "README.md": "payments\n"})
	r.commit(jane, "Cap the delay", day(50), map[string]string{"src/backoff.py": policy + "    timeout = 30\n"})
	r.rename("src/backoff.py", "src/policy.py")
	r.commit("Bob Lee <bob@example.com>", "Rename backoff to policy", day(40), map[string]string{
		"src/policy.py": "# Backoff rules\n" + policy + "    timeout = 30\n"})
	r.rename("src/policy.py", "src/retry_policy.py")
	r.commit(jane, "Move policy into retry_policy", day(30), nil)
	r.commit(jane, "Build the policy from settings", day(20), map[string]string{
		"src/retry_policy.py": "# Backoff rules\n" + policy + "    timeout = 30\n    jitter = 0.1\n"})
	return r
}

func TestARenameIsOneChangeWithItsOldPath(t *testing.T) {
	// @covers op:f op:type
	r := renamedTwice(t)
	store := r.ingest()
	var moved *FileChange
	for _, seg := range store.segments {
		for i := range seg.commits {
			if seg.commits[i].Subject == "Move policy into retry_policy" {
				moved = &seg.commits[i].Files[0]
			}
		}
	}
	if moved == nil || moved.Path != "src/retry_policy.py" || moved.OldPath != "src/policy.py" || moved.Added+moved.Removed != 0 {
		t.Fatalf("the pure rename is %+v, want src/policy.py → src/retry_policy.py with no lines", moved)
	}
	repos := []Repo{{ID: "r", Name: "r", Root: r.root, Store: store}}
	// A rename re-adds no lines, so type:added finds only the commit that wrote one.
	got, _ := runSearch(t, `type:added "def next_delay"`, repos...)
	if want := []string{"Add exponential backoff"}; !slices.Equal(got, want) {
		t.Errorf("type:added found %q, want %q", got, want)
	}
}

func TestFFollowsAFileThroughItsRenames(t *testing.T) {
	// @covers op:f
	r := renamedTwice(t)
	repos := []Repo{{ID: "r", Name: "r", Root: r.root, Store: r.ingest()}}
	all := []string{"Build the policy from settings", "Move policy into retry_policy", "Rename backoff to policy", "Cap the delay", "Add exponential backoff"}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"f:retry_policy.py type:commit", all},
		// The commit that wrote a line, two names ago.
		{"f:retry_policy.py next_delay type:commit", []string{"Add exponential backoff"}},
		// An old name finds the file under that name and the rename away from it, not what came after.
		{"f:backoff.py type:commit", []string{"Rename backoff to policy", "Cap the delay", "Add exponential backoff"}},
		// -f: leaves the file out under every name.
		{"author:jane -f:retry_policy.py type:commit", []string{"Add exponential backoff"}},
	} {
		got, _ := runSearch(t, tc.query, repos...)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s found %q, want %q", tc.query, got, tc.want)
		}
	}
	_, items := runSearch(t, "f:backoff.py type:commit", repos...)
	if files := items[0].Files; len(files) != 1 || files[0].OldPath != "src/backoff.py" || files[0].Path != "src/policy.py" ||
		files[0].CurrentPath != "src/retry_policy.py" {
		t.Errorf("the rename's file is %+v, want src/backoff.py → src/policy.py, now src/retry_policy.py", files)
	}
	if files := items[2].Files; len(files) != 1 || files[0].CurrentPath != "src/retry_policy.py" {
		t.Errorf("the first commit's file is %+v, want it named src/retry_policy.py now", files)
	}
}

func TestACopyAndABigRewriteStartTheirOwnHistory(t *testing.T) {
	// @covers op:f
	r := newTestRepo(t)
	r.commit("Jane <j@example.com>", "Add the policy", day(10), map[string]string{"policy.py": policy, "errors.py": "class A(Exception):\n    pass\n"})
	r.commit("Jane <j@example.com>", "Copy the policy", day(8), map[string]string{"policy_copy.py": policy})
	r.git("rm", "--quiet", "errors.py")
	r.commit("Jane <j@example.com>", "Rewrite errors as exceptions", day(6), map[string]string{"exceptions.py": "class TransientError(RuntimeError):\n    \"\"\"Worth retrying.\"\"\"\n"})
	repos := []Repo{{ID: "r", Name: "r", Root: r.root, Store: r.ingest()}}
	for query, want := range map[string][]string{
		"f:policy_copy.py type:commit": {"Copy the policy"},
		"f:exceptions.py type:commit":  {"Rewrite errors as exceptions"},
	} {
		if got, _ := runSearch(t, query, repos...); !slices.Equal(got, want) {
			t.Errorf("%s found %q, want %q", query, got, want)
		}
	}
}

func TestAPathTakenOverAfterARenameKeepsItsOwnHistory(t *testing.T) {
	// @covers op:f
	r := newTestRepo(t)
	r.commit("Jane <j@example.com>", "Add a", day(10), map[string]string{"a.py": policy})
	r.rename("a.py", "b.py")
	r.commit("Jane <j@example.com>", "Rename a to b", day(8), nil)
	r.commit("Jane <j@example.com>", "Add a new a", day(6), map[string]string{"a.py": "print('a new file')\n"})
	repos := []Repo{{ID: "r", Name: "r", Root: r.root, Store: r.ingest()}}
	if got, want := first(runSearch(t, "f:^b.py type:commit", repos...)), []string{"Rename a to b", "Add a"}; !slices.Equal(got, want) {
		t.Errorf("f:^b.py found %q, want %q", got, want)
	}
	if got, want := first(runSearch(t, "f:^a.py$ new type:commit", repos...)), []string{"Add a new a"}; !slices.Equal(got, want) {
		t.Errorf("f:^a.py$ new found %q, want %q", got, want)
	}
}

func TestUpdateNamesOlderCommitsAfterANewRename(t *testing.T) {
	// @covers op:f
	r := newTestRepo(t)
	r.commit("Jane <j@example.com>", "Add the policy", day(10), map[string]string{"policy.py": policy})
	r.commit("Jane <j@example.com>", "Tune the policy", day(9), map[string]string{"policy.py": policy + "# tuned\n"})
	store := r.ingest()
	r.rename("policy.py", "retry_policy.py")
	r.commit("Jane <j@example.com>", "Rename the policy", day(8), nil)
	updated, err := Update(context.Background(), r.root, store, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.fresh != 1 {
		t.Fatalf("the update read %d commits, want only the new one", updated.fresh)
	}
	repos := []Repo{{ID: "r", Name: "r", Root: r.root, Store: updated}}
	want := []string{"Rename the policy", "Tune the policy", "Add the policy"}
	if got := first(runSearch(t, "f:retry_policy type:commit", repos...)); !slices.Equal(got, want) {
		t.Errorf("after the update f:retry_policy found %q, want %q", got, want)
	}
	// The store the update started from is unchanged: a search on it may still be running.
	old := []Repo{{ID: "r", Name: "r", Root: r.root, Store: store}}
	if got := first(runSearch(t, "f:retry_policy type:commit", old...)); len(got) != 0 {
		t.Errorf("the old store found %q, want nothing", got)
	}
}

// first returns runSearch's subjects.
func first(subjects []string, _ any) []string { return subjects }

func TestPreviewShowsARenameAndEachFilesNameNow(t *testing.T) {
	// @covers rpc:preview/get
	r := renamedTwice(t)
	repo := &Repo{ID: "r", Name: "r", Root: r.root, Store: r.ingest()}
	_, items := runSearch(t, "f:retry_policy.py type:commit", *repo)
	byPreview := map[string]protocolPreview{}
	for _, item := range items {
		ref, ok := ParseRef(item.Ref)
		if !ok {
			t.Fatalf("ParseRef(%q) failed", item.Ref)
		}
		preview, err := Preview(context.Background(), repo, ref, plan(t, "f:retry_policy.py type:commit"), 0)
		if err != nil {
			t.Fatal(err)
		}
		byPreview[item.Subject] = protocolPreview{files: preview.Files, hunks: len(preview.Hunks)}
	}
	moved := byPreview["Move policy into retry_policy"]
	if len(moved.files) != 1 || moved.files[0].OldPath != "src/policy.py" || moved.hunks != 0 {
		t.Errorf("the pure rename's preview has files %+v and %d hunks, want src/policy.py → src/retry_policy.py and none", moved.files, moved.hunks)
	}
	created := byPreview["Add exponential backoff"]
	for _, file := range created.files {
		if file.Path == "src/backoff.py" && (file.CurrentPath != "src/retry_policy.py" || file.HiddenByFilter) {
			t.Errorf("backoff.py in the first commit's preview is %+v, want it shown and named src/retry_policy.py now", file)
		}
	}
}

// protocolPreview is what TestPreviewShowsARenameAndEachFilesNameNow checks of a preview.
type protocolPreview struct {
	files []protocol.FileStat
	hunks int
}
