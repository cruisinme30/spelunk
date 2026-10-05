package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// rawCommit writes a commit object exactly as given (header lines, a blank
// line, the message), on top of the index's tree and HEAD, and moves the
// branch to it. Git's own commands refuse most of what tests need here.
func (r *testRepo) rawCommit(header, message string) string {
	r.t.Helper()
	tree, parent := r.git("write-tree"), r.git("rev-parse", "HEAD")
	object := fmt.Sprintf("tree %s\nparent %s\n%s\n\n%s", tree, parent, header, message)
	cmd := exec.CommandContext(context.Background(), "git", "-C", r.root, "hash-object", "-t", "commit", "-w", "--stdin", "--literally")
	cmd.Stdin = strings.NewReader(object)
	out, err := cmd.Output()
	if err != nil {
		r.t.Fatalf("hash-object: %v", err)
	}
	sha := strings.TrimSpace(string(out))
	r.git("reset", "--quiet", "--hard", sha)
	return sha
}

// stage writes files and adds them to the index.
func (r *testRepo) stage(files map[string]string) {
	r.t.Helper()
	for path, content := range files {
		r.write(path, content)
	}
	r.git("add", "--all")
}

// sorted returns a sorted copy of list.
func sorted(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)
	return out
}

// filePaths lists the paths of a commit's changed files.
func filePaths(c *Commit) []string {
	var paths []string
	for _, f := range c.Files {
		paths = append(paths, f.Path)
	}
	return paths
}

func TestUnusualCommitsAreReadWhole(t *testing.T) {
	r := newTestRepo(t)
	shas := map[string]string{}
	shas["names"] = r.commit("Ada <ada@example.com>", "odd names", day(9), map[string]string{
		"sp ace.txt": "space\n", "tab\tname.txt": "tab\n", "quo\"te.bin": "bin\x00ary\n", "ünï/😀.txt": "emoji\n",
	})
	// A subject far longer than a line the parser keeps, then a long body line.
	long := strings.Repeat("squashed change ", 400)
	shas["long"] = r.commit("Ada <ada@example.com>", long+"\n\n"+strings.Repeat("b", 5000), day(8), map[string]string{"long.txt": "long\n"})
	// Dates Git stores but can't write as ISO 8601.
	r.stage(map[string]string{"zone.txt": "zone\n"})
	shas["zone"] = r.rawCommit("author Odd <odd@example.com> 1700000000 +9999\ncommitter Odd <odd@example.com> 1700000000 +0000", "bad zone\n")
	r.stage(map[string]string{"year.txt": "year\n"})
	shas["year"] = r.rawCommit("author Odd <odd@example.com> 253402300800 +0000\ncommitter Odd <odd@example.com> 1700000000 +0000", "year 10000\n")
	// Control characters in the name and message, in a Latin-1 commit.
	r.stage(map[string]string{"ctl.txt": "ctl\n"})
	shas["ctl"] = r.rawCommit("author Ctl\x1fName\x1e <ctl@example.com> 1700000000 +0000\ncommitter Ctl <ctl@example.com> 1700000000 +0000\nencoding ISO-8859-1",
		"Caf\xe9 \x1f subject\n\n\x1e"+"body line\n")
	shas["last"] = r.commit("Bea <bea@example.com>", "last", day(1), map[string]string{"last.txt": "last\n"})
	// Settings that change what git log and git show print.
	for _, setting := range [][]string{{"diff.noPrefix", "true"}, {"log.showRoot", "false"}, {"i18n.logOutputEncoding", "ISO-8859-1"}, {"diff.external", "false"}} {
		r.git("config", setting[0], setting[1])
	}

	store := r.ingest()
	if store.Commits() != 6 {
		t.Fatalf("store has %d commits, want 6", store.Commits())
	}
	tests := []struct {
		name, subject, author string
		at                    time.Time
		files                 []string
	}{
		{"names", "odd names", "Ada", day(9), []string{"quo\"te.bin", "sp ace.txt", "tab\tname.txt", "ünï/😀.txt"}},
		{"long", strings.TrimSpace(long), "Ada", day(8), []string{"long.txt"}},
		{"zone", "bad zone", "Odd", time.Unix(1700000000, 0), []string{"zone.txt"}},
		{"year", "year 10000", "Odd", time.Unix(253402300800, 0), []string{"year.txt"}},
		{"ctl", "Café \x1f subject", "Ctl\x1fName\x1e", time.Unix(1700000000, 0), []string{"ctl.txt"}},
		{"last", "last", "Bea", day(1), []string{"last.txt"}},
	}
	for _, tt := range tests {
		c, ok := store.Commit(shas[tt.name])
		if !ok {
			t.Errorf("%s: commit missing", tt.name)
			continue
		}
		if !strings.HasPrefix(tt.subject, c.Subject) || len(c.Subject) < 100 && c.Subject != tt.subject {
			t.Errorf("%s: subject = %.40q (%d bytes), want %.40q", tt.name, c.Subject, len(c.Subject), tt.subject)
		}
		if c.AuthorName != tt.author || !c.At.Equal(tt.at) {
			t.Errorf("%s: author %q at %v, want %q at %v", tt.name, c.AuthorName, c.At, tt.author, tt.at)
		}
		if got := filePaths(c); !slices.Equal(sorted(got), tt.files) {
			t.Errorf("%s: files = %q, want %q", tt.name, got, tt.files)
		}
	}
	if c, _ := store.Commit(shas["ctl"]); c.Body != "\x1e"+"body line" {
		t.Errorf("body = %q, want the body as written", c.Body)
	}

	// The preview, read with git show, sees the same paths.
	repo := &Repo{ID: "r", Name: "r", Root: r.root, Store: store}
	preview, err := Preview(context.Background(), repo, Ref{RepoID: "r", SHA: shas["names"]}, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	var previewed []string
	for _, f := range preview.Files {
		previewed = append(previewed, f.Path)
	}
	if want := tests[0].files; !reflect.DeepEqual(sorted(previewed), want) {
		t.Errorf("preview files = %q, want %q", previewed, want)
	}
}

func TestALongDiffLineIsClippedInThePreview(t *testing.T) {
	r := newTestRepo(t)
	sha := r.commit("Ada <ada@example.com>", "minified", day(1), map[string]string{
		"app.min.js": strings.Repeat("x", 300_000) + "needle" + strings.Repeat("y", 300_000) + "\n",
	})
	repo := &Repo{ID: "r", Name: "r", Root: r.root, Store: r.ingest()}
	preview, err := Preview(context.Background(), repo, Ref{RepoID: "r", SHA: sha}, plan(t, "type:commit needle"), 3)
	if err != nil {
		t.Fatal(err)
	}
	line := preview.Hunks[0].Lines[0]
	shown := []rune(line.Text)
	if len(shown) > 2_002 || len(line.Hits) != 1 || string(shown[line.Hits[0].Start:line.Hits[0].End]) != "needle" {
		t.Errorf("diff line = %d runes with hits %+v, want a clipped window with needle marked", len(shown), line.Hits)
	}
}

func TestSaveRemovesTempFilesACrashLeftBehind(t *testing.T) {
	r, _ := paymentsHistory(t)
	dir := t.TempDir()
	stale := filepath.Join(dir, ".history-111")
	if err := os.WriteFile(stale, []byte("half a store"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleTempAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := r.ingest().Save(filepath.Join(dir, "repo.history")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a temp file from an hour-old crashed save is still there (%v)", err)
	}
}
