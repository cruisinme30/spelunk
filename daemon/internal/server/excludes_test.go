package server

import (
	"slices"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// excludeRoot sets VS Code's excludes for root r to globs and waits until
// a search sees them.
func excludeRoot(t *testing.T, client *testClient, patterns ...protocol.ExcludePattern) {
	t.Helper()
	updateSettings(t, client, func(s *protocol.Settings) {
		s.EditorExcludes = []protocol.RootExcludes{{RootID: "r", Patterns: patterns}}
	})
	waitUntil(t, "the excludes to apply", func() bool { return len(client.server.Settings().EditorExcludes) == 1 })
}

func globPatterns(globs ...string) []protocol.ExcludePattern {
	patterns := make([]protocol.ExcludePattern, len(globs))
	for i, g := range globs {
		patterns[i] = protocol.ExcludePattern{Glob: g}
	}
	return patterns
}

// excludesNote is the search's VS Code excludes note, if it has one.
func excludesNote(result protocol.SearchResult) (protocol.HiddenNote, bool) {
	for _, note := range result.Hidden {
		if note.Reason == "excludes" {
			return note, true
		}
	}
	return protocol.HiddenNote{}, false
}

func excludesWorkspace(t *testing.T) *testClient {
	t.Helper()
	root := testutil.WriteTree(t, map[string]string{
		"src/retry.py":             "retry = 1\n",
		"build/retry.py":           "retry = 1  # copied by the build\n",
		"http/build/steps.py":      "retry = 2\n",
		"coverage/retry.py.html":   "<td>retry</td>\n",
		"web/coverage/report.html": "<td>retry</td>\n",
	})
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r", Path: root, Name: "app"})
	return client
}

func TestVSCodeExcludesLeaveOutTheFilesTheyHide(t *testing.T) {
	// @covers op:excludes screen:editor-excludes
	client := excludesWorkspace(t)
	all, _ := paths(t, client, "type:code retry")
	if len(all) != 5 {
		t.Fatalf("before the excludes, retry = %q, want all 5 lines", all)
	}

	excludeRoot(t, client, globPatterns("build", "**/coverage", "**/never")...)
	got, result := paths(t, client, "type:code retry")
	// VS Code anchors build at the folder, so http/build stays.
	if want := []string{"r/http/build/steps.py:1", "r/src/retry.py:1"}; !slices.Equal(sorted(got), want) {
		t.Errorf("retry = %q, want %q", got, want)
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2: hidden results aren't counted", result.Total)
	}
	note, ok := excludesNote(result)
	if !ok {
		t.Fatalf("hidden = %+v, want a note for VS Code's excludes", result.Hidden)
	}
	if note.Count != 3 || note.Unit != "matches" || note.Filter != query.ExcludesText {
		t.Errorf("note = %+v, want 3 matches hidden by %s", note, query.ExcludesText)
	}
	// Only the patterns that hid something are named.
	if want := []string{"**/coverage", "build"}; !slices.Equal(note.Patterns, want) {
		t.Errorf("patterns = %q, want %q", note.Patterns, want)
	}
	if shown := query.ApplyFix("type:code retry", note.Undo); shown != "excludes:no type:code retry" {
		t.Errorf("Show them makes %q, want excludes:no first", shown)
	}
	for _, f := range result.Facets {
		for _, b := range f.Buckets {
			if b.Label == "build/" || b.Label == "coverage/" {
				t.Errorf("facet %s counts %s, which only hidden results are in", f.Field, b.Label)
			}
		}
	}
}

func TestExcludesNoShowsTheHiddenFilesLastAndMarked(t *testing.T) {
	// @covers op:excludes screen:editor-excludes
	client := excludesWorkspace(t)
	excludeRoot(t, client, globPatterns("build", "**/coverage")...)

	items, result := search(t, client, "excludes:no type:code retry")
	if len(items) != 5 {
		t.Fatalf("excludes:no retry = %d results, want all 5", len(items))
	}
	for i, item := range items {
		excluded := item.RankReason == protocol.RankReasonExcluded
		hidden := item.Path == "build/retry.py" || item.Path == "coverage/retry.py.html" || item.Path == "web/coverage/report.html"
		if excluded != hidden {
			t.Errorf("%s: rank reason %q", item.Path, item.RankReason)
		}
		if hidden && i < 2 {
			t.Errorf("%s is result %d, want the excluded files after the others", item.Path, i)
		}
	}
	note, ok := excludesNote(result)
	if !ok || !note.Shown || note.Count != 3 {
		t.Fatalf("hidden = %+v, want a shown note counting 3 matches", result.Hidden)
	}
	if hidden := query.ApplyFix("excludes:no type:code retry", note.Undo); hidden != "type:code retry" {
		t.Errorf("Hide them makes %q, want excludes:no taken out", hidden)
	}

	// excludes:yes is what no excludes: says.
	got, _ := paths(t, client, "excludes:yes type:code retry")
	if len(got) != 2 {
		t.Errorf("excludes:yes retry = %q, want the 2 shown lines", got)
	}
}

func TestExcludesWithoutSettingsChangeNothing(t *testing.T) {
	// @covers op:excludes
	client := excludesWorkspace(t)
	got, result := paths(t, client, "type:code retry")
	if len(got) != 5 {
		t.Errorf("retry = %q, want all 5 lines", got)
	}
	if _, ok := excludesNote(result); ok {
		t.Errorf("hidden = %+v, want no excludes note", result.Hidden)
	}
	if got, _ := paths(t, client, "excludes:no type:code retry"); len(got) != 5 {
		t.Errorf("excludes:no retry = %q, want all 5 lines", got)
	}
}

func TestExcludesHideAFileOnlyBesideItsWhenSibling(t *testing.T) {
	// @covers op:excludes
	root := testutil.WriteTree(t, map[string]string{
		"src/checkout.ts": "const retry = 1\n",
		"src/checkout.js": "const retry = 1\n",
		"src/legacy.js":   "var retry = 2\n",
	})
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r", Path: root, Name: "web"})
	excludeRoot(t, client, protocol.ExcludePattern{Glob: "**/*.js", When: "$(basename).ts"})
	got, _ := paths(t, client, "type:code retry")
	if want := []string{"r/src/checkout.ts:1", "r/src/legacy.js:1"}; !slices.Equal(sorted(got), want) {
		t.Errorf("retry = %q, want %q", got, want)
	}
}

func TestExcludesKeepFileNamesAndDefinitionsOut(t *testing.T) {
	// @covers op:excludes
	client := excludesWorkspace(t)
	excludeRoot(t, client, globPatterns("build", "**/coverage")...)
	items, _ := search(t, client, "type:file retry")
	for _, item := range items {
		if item.Path != "src/retry.py" {
			t.Errorf("type:file retry has %s, which the excludes hide", item.Path)
		}
	}
}

func TestExcludesHideCommitsThatOnlyChangeExcludedFiles(t *testing.T) {
	// @covers op:excludes
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	root := gitRepo(t,
		gitCommit{"Jane Doe", "Add retry", at, map[string]string{"src/a.ts": "retry = 1\n", "src/a.js": "retry = 1\n"}},
		gitCommit{"Jane Doe", "Rebuild dist for retry", at.Add(time.Hour), map[string]string{"dist/bundle.js": "retry = 3\n"}},
	)
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r", Path: root, Name: "web"})
	client.waitForHistory(t)
	excludeRoot(t, client, protocol.ExcludePattern{Glob: "dist"}, protocol.ExcludePattern{Glob: "**/*.js", When: "$(basename).ts"})

	items, result := search(t, client, "type:commit retry")
	if len(items) != 1 || items[0].Subject != "Add retry" {
		t.Fatalf("commits = %+v, want only Add retry", items)
	}
	if want := []protocol.FileStat{{Path: "src/a.ts", Added: 1}}; !slices.Equal(items[0].Files, want) {
		t.Errorf("Add retry lists %+v, want the excluded src/a.js left out", items[0].Files)
	}
	note, ok := excludesNote(result)
	if !ok || note.Count != 1 || note.Unit != "commits" || !slices.Equal(note.Patterns, []string{"dist"}) {
		t.Errorf("hidden = %+v, want 1 commit hidden by dist", result.Hidden)
	}
}

func TestReplaceLeavesExcludedFilesAlone(t *testing.T) {
	// @covers op:excludes rpc:replace/plan
	client := excludesWorkspace(t)
	excludeRoot(t, client, globPatterns("build", "**/coverage")...)
	plan := planReplace(t, client, "retry", "retries")
	var changed []string
	for _, file := range plan.Files {
		changed = append(changed, file.Path)
	}
	if want := []string{"http/build/steps.py", "src/retry.py"}; !slices.Equal(sorted(changed), want) {
		t.Errorf("replace changes %q, want %q", changed, want)
	}
	if plan.Matches != 2 || plan.Excluded != 3 {
		t.Errorf("matches = %d, excluded = %d; want 2 and 3", plan.Matches, plan.Excluded)
	}
	if all := planReplace(t, client, "excludes:no retry", "retries"); all.Matches != 5 || all.Excluded != 0 {
		t.Errorf("excludes:no: matches = %d, excluded = %d; want 5 and 0", all.Matches, all.Excluded)
	}
}

// sorted returns a sorted copy of lines.
func sorted(lines []string) []string {
	return slices.Sorted(slices.Values(lines))
}
