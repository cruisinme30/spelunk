package history

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// client is client.py with its retry log line.
func client(log string) string {
	return "def charge():\n    for attempt in range(3):\n        try:\n            return post()\n        except TransientError:\n            " +
		log + "\n            sleep(delay)\n"
}

// mergedRepo is main with a branch by Bob, written three weeks ago and
// merged two days ago over a conflicting edit by Jane, then three
// one-commit branches by Ana merged in one octopus merge a day ago.
func mergedRepo(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	jane, bob, ana := "Jane Doe <jane@example.com>", "Bob Lee <bob@example.com>", "Ana Ruiz <ana@example.com>"
	r.commit(jane, "Add the client", day(40), map[string]string{"client.py": client(`log.info("retry %d", attempt)`), "README.md": "payments\n"})
	r.git("checkout", "--quiet", "-b", "jitter")
	r.commit(bob, "Log the delay", day(21), map[string]string{"client.py": client(`log.info("retry in %.1fs", delay)`)})
	r.commit(bob, "Add jitter", day(20), map[string]string{"jitter.py": "JITTER = 0.1\n"})
	r.git("checkout", "--quiet", "main")
	r.commit(jane, "Say which attempt", day(14), map[string]string{"client.py": client(`log.info("retrying charge, attempt %d", attempt)`)})
	r.merge(jane, "Merge branch 'jitter'", day(2), map[string]string{"client.py": client(`log.info("retrying charge in %.1fs", delay)`)}, "jitter")
	base := r.git("rev-parse", "HEAD")
	for i, name := range []string{"docs", "owners", "scripts"} {
		r.git("checkout", "--quiet", "-b", name, base)
		r.commit(ana, "Change "+name, day(4-i), map[string]string{name + ".txt": name + "\n"})
	}
	r.git("checkout", "--quiet", "main")
	r.merge(ana, "Merge docs, owners and scripts", day(1), nil, "docs", "owners", "scripts")
	return r
}

// merge merges branches into the checked-out branch at the given time,
// resolving a conflict with resolved (the files' content) when there is one.
func (r *testRepo) merge(author, message string, at time.Time, resolved map[string]string, branches ...string) {
	r.t.Helper()
	name, email, _ := strings.Cut(author, " <")
	env := []string{
		"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + strings.TrimSuffix(email, ">"),
		"GIT_AUTHOR_DATE=" + at.Format(time.RFC3339), "GIT_COMMITTER_DATE=" + at.Format(time.RFC3339),
	}
	args := append([]string{"-c", "merge.conflictStyle=merge", "merge", "--quiet", "--no-ff", "--no-edit", "-m", message}, branches...)
	cmd := r.gitCommand(env, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		if resolved == nil {
			r.t.Fatalf("git merge: %v\n%s", err, out)
		}
		for path, content := range resolved {
			r.write(path, content)
		}
		r.git("add", "--all")
		r.gitWithEnv(env, "commit", "--quiet", "--no-edit")
	}
}

// gitCommand is git in the repo with env added.
func (r *testRepo) gitCommand(env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", r.root}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	return cmd
}

func ingestWith(t *testing.T, r *testRepo, opts Options) *Store {
	t.Helper()
	store, err := Ingest(context.Background(), r.root, opts, nil, nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return store
}

func subjects(store *Store) []string {
	var out []string
	for _, seg := range store.segments {
		for i := range seg.commits {
			out = append(out, seg.commits[i].Subject)
		}
	}
	return out
}

func TestMergedBranchesAreReadEachAfterItsMerge(t *testing.T) {
	// @covers setting:index.mergedBranches
	r := mergedRepo(t)
	if !canReadMergedBranches(context.Background()) {
		t.Skip("git is older than 2.36")
	}
	store := ingestWith(t, r, Options{MergedBranches: true})
	want := []string{
		"Merge docs, owners and scripts", "Change scripts", "Change owners", "Change docs",
		"Merge branch 'jitter'", "Add jitter", "Log the delay",
		"Say which attempt", "Add the client",
	}
	if got := subjects(store); !slices.Equal(got, want) {
		t.Fatalf("commits = %q, want %q", got, want)
	}
	merge := r.git("rev-parse", "HEAD~1")
	for _, seg := range store.segments {
		for i := range seg.commits {
			c := &seg.commits[i]
			if c.Subject == "Add jitter" && (c.MergedIn != merge || !c.MergedAt.Equal(day(2))) {
				t.Errorf("Add jitter came in by %s at %v, want %s at %v", c.MergedIn, c.MergedAt, merge, day(2))
			}
			if c.Subject == "Say which attempt" && c.MergedIn != "" {
				t.Errorf("a first-parent commit says it was merged in by %s", c.MergedIn)
			}
		}
	}
	// The default reads the first-parent line only, as before.
	if got, want := subjects(ingestWith(t, r, Options{})), []string{
		"Merge docs, owners and scripts", "Merge branch 'jitter'", "Say which attempt", "Add the client",
	}; !slices.Equal(got, want) {
		t.Errorf("first parents = %q, want %q", got, want)
	}
}

func TestMergedBranchesFindBranchWorkByItsAuthorAndWhenItLanded(t *testing.T) {
	// @covers setting:index.mergedBranches op:author op:since op:until
	r := mergedRepo(t)
	if !canReadMergedBranches(context.Background()) {
		t.Skip("git is older than 2.36")
	}
	merged := []Repo{{ID: "r", Name: "r", Root: r.root, Store: ingestWith(t, r, Options{MergedBranches: true})}}
	firstParents := []Repo{{ID: "r", Name: "r", Root: r.root, Store: ingestWith(t, r, Options{})}}
	for _, tc := range []struct {
		query               string
		merged, firstParent []string
	}{
		// Bob's commits keep their author.
		{"author:bob type:commit", []string{"Add jitter", "Log the delay"}, nil},
		// A line that only ever existed on the branch.
		{`type:commit "retry in %"`, []string{"Log the delay"}, nil},
		// Branch work landed this week, two days ago, though written three weeks ago.
		{"since:1w f:jitter.py type:commit", []string{"Add jitter"}, []string{"Merge branch 'jitter'"}},
		{"until:1w author:bob type:commit", nil, nil},
		// The merge shows only how it resolved the conflict.
		{`type:added "retrying charge in"`, []string{"Merge branch 'jitter'"}, []string{"Merge branch 'jitter'"}},
		{`type:removed "retrying charge, attempt"`, nil, []string{"Merge branch 'jitter'"}},
		// An octopus merge changed nothing of its own: its branches did.
		{"f:owners.txt type:commit", []string{"Change owners"}, []string{"Merge docs, owners and scripts"}},
		{"msg:merge type:commit", []string{"Merge docs, owners and scripts", "Merge branch 'jitter'"}, []string{"Merge docs, owners and scripts", "Merge branch 'jitter'"}},
	} {
		if got := first(runSearch(t, tc.query, merged...)); !slices.Equal(got, tc.merged) {
			t.Errorf("merged branches: %s found %q, want %q", tc.query, got, tc.merged)
		}
		if got := first(runSearch(t, tc.query, firstParents...)); !slices.Equal(got, tc.firstParent) {
			t.Errorf("first parents: %s found %q, want %q", tc.query, got, tc.firstParent)
		}
	}
	_, items := runSearch(t, "author:bob f:jitter.py type:commit", merged...)
	if len(items) != 1 || items[0].MergedIn != r.git("rev-parse", "HEAD~1") || items[0].MergedAt != day(2).UTC().Format(time.RFC3339) {
		t.Errorf("Add jitter's result = %+v, want it merged in by HEAD~1 two days ago", items)
	}
	_, items = runSearch(t, `type:commit "retrying charge in"`, merged...)
	if len(items) != 1 || items[0].Parents != 2 || len(items[0].Files) != 1 || !items[0].Files[0].Resolution ||
		items[0].Files[0].Added != 1 || items[0].Files[0].Removed != 0 {
		t.Errorf("the merge's result = %+v, want a merge with one resolved line in client.py", items)
	}
}

func TestPreviewOfAMergeWithMergedBranchesShowsItsResolution(t *testing.T) {
	// @covers setting:index.mergedBranches rpc:preview/get
	r := mergedRepo(t)
	if !canReadMergedBranches(context.Background()) {
		t.Skip("git is older than 2.36")
	}
	repo := &Repo{ID: "r", Name: "r", Root: r.root, Store: ingestWith(t, r, Options{MergedBranches: true})}
	preview, err := Preview(context.Background(), repo, Ref{RepoID: "r", SHA: r.git("rev-parse", "HEAD~1")}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, hunk := range preview.Hunks {
		for _, line := range hunk.Lines {
			lines = append(lines, line.Kind+" "+strings.TrimSpace(line.Text))
		}
	}
	want := []string{`add log.info("retrying charge in %.1fs", delay)`}
	if len(preview.Files) != 1 || !preview.Files[0].Resolution || !slices.Equal(lines, want) {
		t.Errorf("the merge's preview = files %+v, lines %q; want client.py resolved with %q", preview.Files, lines, want)
	}
	octopus, err := Preview(context.Background(), repo, Ref{RepoID: "r", SHA: r.git("rev-parse", "HEAD")}, nil, 0)
	if err != nil || len(octopus.Files) != 0 {
		t.Errorf("the octopus merge's preview = %+v, %v; want no files", octopus.Files, err)
	}
}

func TestUpdateWithMergedBranchesReadsANewMergesBranch(t *testing.T) {
	// @covers setting:index.mergedBranches
	r := mergedRepo(t)
	if !canReadMergedBranches(context.Background()) {
		t.Skip("git is older than 2.36")
	}
	store := ingestWith(t, r, Options{MergedBranches: true})
	r.git("checkout", "--quiet", "-b", "late")
	r.commit("Bob Lee <bob@example.com>", "Late fix", day(1), map[string]string{"late.txt": "late\n"})
	r.git("checkout", "--quiet", "main")
	r.merge("Jane Doe <jane@example.com>", "Merge late", fixedNow, nil, "late")
	updated, err := Update(context.Background(), r.root, store, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.fresh != 2 || !updated.MergedBranches {
		t.Errorf("the update read %d new commits (merged branches %v), want the merge and its branch's commit", updated.fresh, updated.MergedBranches)
	}
	if got := subjects(updated)[:3]; !slices.Equal(got, []string{"Merge late", "Late fix", "Merge docs, owners and scripts"}) {
		t.Errorf("newest commits = %q", got)
	}
}

func TestGitVersionsThatCanReadMergedBranches(t *testing.T) {
	for version, want := range map[string]bool{
		"git version 2.56.0 (Apple Git-155)": true, "git version 2.36.0": true, "git version 2.35.8": false,
		"git version 3.0.1": true, "nonsense": false,
	} {
		if got := gitAtLeast(version, 2, 36); got != want {
			t.Errorf("gitAtLeast(%q) = %v, want %v", version, got, want)
		}
	}
}
