package history

// Test helpers shared by this package's test files.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
)

// fixedNow is the clock for plans, so since: windows are stable.
var fixedNow = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)

// testRepo is a Git repo in a temporary folder, built commit by commit.
type testRepo struct {
	t    *testing.T
	root string
}

// newTestRepo creates an empty Git repo; it skips the test without git.
func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	r := &testRepo{t: t, root: t.TempDir()}
	r.git("init", "--quiet", "--initial-branch=main")
	r.git("config", "user.name", "Test")
	r.git("config", "user.email", "test@example.com")
	r.git("config", "commit.gpgsign", "false")
	return r
}

// git runs git in the repo and returns its output, failing the test on error.
func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	return r.gitWithEnv(nil, args...)
}

func (r *testRepo) gitWithEnv(env []string, args ...string) string {
	r.t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", r.root}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// write writes a file, creating its folders.
func (r *testRepo) write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// commit stages everything and commits it as author at the given time,
// returning the new sha. files maps paths to their new content.
func (r *testRepo) commit(author, message string, at time.Time, files map[string]string) string {
	r.t.Helper()
	for path, content := range files {
		r.write(path, content)
	}
	r.git("add", "--all")
	name, email, _ := strings.Cut(author, " <")
	date := at.Format(time.RFC3339)
	r.gitWithEnv([]string{
		"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + strings.TrimSuffix(email, ">"),
		"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date,
	}, "commit", "--quiet", "--allow-empty", "-m", message)
	return r.git("rev-parse", "HEAD")
}

// ingest reads the repo's whole history.
func (r *testRepo) ingest() *Store {
	r.t.Helper()
	store, err := Ingest(context.Background(), r.root, Options{}, nil, nil)
	if err != nil {
		r.t.Fatalf("Ingest: %v", err)
	}
	return store
}

// plan parses and plans text, failing the test on any error.
func plan(tb testing.TB, text string) *query.Plan {
	tb.Helper()
	parsed := query.Parse(text, nil)
	for _, d := range parsed.Diagnostics {
		if d.Severity == protocol.SeverityError {
			tb.Fatalf("Parse(%q): %s", text, d.Message)
		}
	}
	p, _, err := query.NewPlan(parsed, protocol.Settings{DefaultCount: 500, HistoryDepth: "2y"}, fixedNow, "")
	if err != nil {
		tb.Fatalf("NewPlan(%q): %v", text, err)
	}
	return p
}

// runSearch runs text over the repos and returns each result's subject.
func runSearch(t *testing.T, text string, repos ...Repo) ([]string, []protocol.ResultItem) {
	t.Helper()
	var items []protocol.ResultItem
	if _, err := Search(context.Background(), plan(t, text), repos, 1, func(item protocol.ResultItem) {
		items = append(items, item)
	}); err != nil {
		t.Fatalf("Search(%q): %v", text, err)
	}
	subjects := make([]string, len(items))
	for i, item := range items {
		subjects[i] = item.Subject
	}
	return subjects, items
}

// day returns a time n days before fixedNow.
func day(n int) time.Time { return fixedNow.AddDate(0, 0, -n) }
