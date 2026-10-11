package trigram

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// changedRepo has files changed at different times: two with uncommitted
// edits saved 10 minutes and an hour ago, a test committed 2 hours ago,
// older commits, and one with no commit in the history window.
func changedRepo() Repo {
	repo := repoOf("app", map[string]string{
		"a.py":                  "timeout = 1\n",
		"b.py":                  "timeout = 2\n",
		"c.py":                  "timeout = 3\n",
		"d.py":                  "timeout = 4\n",
		"e.py":                  "timeout = 5\n",
		"tests/timeout_test.py": "timeout = 6\n",
	})
	setModTime(repo.Shard, "c.py", fixedNow.Add(-10*time.Minute))
	setModTime(repo.Shard, "d.py", fixedNow.Add(-time.Hour))
	repo.History = fakeHistory{
		dirty: map[string]bool{"c.py": true, "d.py": true},
		changed: map[string]time.Time{
			"tests/timeout_test.py": fixedNow.Add(-2 * time.Hour),
			"b.py":                  fixedNow.Add(-3 * time.Hour),
			"a.py":                  fixedNow.Add(-90 * 24 * time.Hour),
		},
	}
	return repo
}

func setModTime(shard *Shard, path string, at time.Time) {
	for i := range shard.Docs {
		if shard.Docs[i].Path == path {
			shard.Docs[i].ModTime = at
		}
	}
}

func TestOrderRecentPutsTheFilesChangedMostRecentlyFirst(t *testing.T) {
	// @covers op:order setting:results.order
	want := []string{"line c.py", "line d.py", "line tests/timeout_test.py", "line b.py", "line a.py", "line e.py"}
	for _, text := range []string{"type:code order:recent timeout", "type:code order:recent:desc timeout"} {
		items, _ := runItems(t, text, defaultSettings, "", changedRepo())
		if got := files(items); !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %q\nwant %q (by when each changed: a save for uncommitted edits, otherwise the newest commit; the test not moved down, the undated file last)", text, got, want)
		}
		if got := reasons(items); len(got) != 0 {
			t.Errorf("%s: reasons = %v, want none: they explain best-match places", text, got)
		}
	}
	recent := defaultSettings
	recent.Order = protocol.ResultOrderRecent
	items, _ := runItems(t, "type:code timeout", recent, "", changedRepo())
	if got := files(items); !reflect.DeepEqual(got, want) {
		t.Errorf("order setting recent:\n got %q\nwant %q", got, want)
	}
}

func TestOrderRecentAscPutsTheOldestFirstAndUndatedFilesStillLast(t *testing.T) {
	items, _ := runItems(t, "type:code order:recent:asc timeout", defaultSettings, "", changedRepo())
	want := []string{"line a.py", "line b.py", "line tests/timeout_test.py", "line d.py", "line c.py", "line e.py"}
	if got := files(items); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
	asc := defaultSettings
	asc.Order = "recent:asc"
	bySetting, _ := runItems(t, "type:code timeout", asc, "", changedRepo())
	if got := files(bySetting); !reflect.DeepEqual(got, want) {
		t.Errorf("order setting recent:asc:\n got %q\nwant %q", got, want)
	}
}

func TestOrderRecentSaysWhenEachFileChanged(t *testing.T) {
	git := changedRepo()
	plain := repoOf("notes", map[string]string{"todo.md": "timeout\n"})
	setModTime(plain.Shard, "todo.md", fixedNow.Add(-30*time.Minute))
	items, _ := runItems(t, "type:code order:recent timeout", defaultSettings, "", git, plain)
	byPath := map[string]protocol.ResultItem{}
	for _, item := range items {
		byPath[item.Path] = item
	}
	saved := func(at time.Time) string { return at.UTC().Format(time.RFC3339) }
	cases := []struct {
		path       string
		dirty      bool
		modifiedAt string
		committed  bool
	}{
		{"c.py", true, saved(fixedNow.Add(-10 * time.Minute)), false},
		{"b.py", false, "", true},
		{"e.py", false, "", false},
		{"todo.md", false, saved(fixedNow.Add(-30 * time.Minute)), false},
	}
	for _, c := range cases {
		item := byPath[c.path]
		if item.Dirty != c.dirty || item.ModifiedAt != c.modifiedAt || (item.LastCommit != nil) != c.committed {
			t.Errorf("%s: dirty %v, modifiedAt %q, last commit %v; want %v, %q, %v",
				c.path, item.Dirty, item.ModifiedAt, item.LastCommit != nil, c.dirty, c.modifiedAt, c.committed)
		}
	}
	// One timeline across repos: the Markdown file saved 30 minutes ago
	// outside Git sorts between d.py's edit an hour ago and c.py's.
	want := []string{"line c.py", "line todo.md", "line d.py", "line tests/timeout_test.py", "line b.py", "line a.py", "line e.py"}
	if got := files(items); !reflect.DeepEqual(got, want) {
		t.Errorf("order across repos:\n got %q\nwant %q", got, want)
	}

	best, _ := runItems(t, "timeout", bestMatchSettings, "", git, plain)
	for _, item := range best {
		if item.Kind == "line" && (item.ModifiedAt != "" || item.LastCommit != nil || item.Dirty) {
			t.Errorf("best match: %s carries when it changed, want that only in order:recent", item.Path)
		}
	}
}

func TestOrderRecentBreaksTiesByBestMatch(t *testing.T) {
	repo := repoOf("app", map[string]string{
		"a.py":     "retry(call)\n",
		"retry.py": "def retry(call):\n    return call()\n",
	})
	withSymbols(repo.Shard)
	sameCommit := fixedNow.Add(-3 * 24 * time.Hour)
	repo.History = fakeHistory{changed: map[string]time.Time{"a.py": sameCommit, "retry.py": sameCommit}}
	items, _ := runItems(t, "type:code order:recent retry", defaultSettings, "", repo)
	if got, want := files(items), []string{"line retry.py", "line a.py"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q: one commit changed both, and retry.py defines retry", got, want)
	}
}

func TestOrderRecentPagesNeverRepeatOrSkipAResult(t *testing.T) {
	all, stats := runItems(t, "order:recent timeout", defaultSettings, "", changedRepo())
	var paged []protocol.ResultItem
	for cursor := ""; ; {
		page, pageStats := runItems(t, "count:2 order:recent timeout", defaultSettings, cursor, changedRepo())
		paged = append(paged, page...)
		if pageStats.NextOffset == 0 {
			break
		}
		cursor = strconv.Itoa(pageStats.NextOffset)
	}
	if got, want := files(paged), files(all); len(paged) != stats.Total || !reflect.DeepEqual(got, want) {
		t.Errorf("pages hold %q (%d results)\nwant %q (%d)", got, len(paged), want, stats.Total)
	}
}

func TestOrderPathDescRunsEachReposPathsFromZToA(t *testing.T) {
	first := rankedRepo()
	second := repoOf("lib", map[string]string{"a/retry.go": "retry\n", "z/retry.go": "retry\n"})
	items, _ := runItems(t, "order:path:desc retry", bestMatchSettings, "", first, second)
	want := []string{
		"file vendor/lib/retry.js", "file tests/retry_test.py", "file src/retry_policy.py", "file src/retry.py",
		"file z/retry.go", "file a/retry.go",
		"line vendor/lib/retry.js", "line tests/retry_test.py", "line src/retry_policy.py", "line src/retry.py",
		"line src/client.py", "line src/backoff.py", "line gen/api.pb.go", "line z/retry.go", "line a/retry.go",
	}
	if got := files(items); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	var client []int
	for _, item := range items {
		if item.Path == "src/client.py" && item.Kind == "line" {
			client = append(client, item.Line)
		}
	}
	if !reflect.DeepEqual(client, []int{1, 3}) {
		t.Errorf("src/client.py lines = %v, want [1 3]: lines stay top to bottom", client)
	}
	asc, _ := runItems(t, "order:path:asc retry", bestMatchSettings, "", first, second)
	plain, _ := runItems(t, "order:path retry", bestMatchSettings, "", first, second)
	if !reflect.DeepEqual(files(asc), files(plain)) {
		t.Errorf("order:path:asc = %q, want the same as order:path, %q", files(asc), files(plain))
	}
}
