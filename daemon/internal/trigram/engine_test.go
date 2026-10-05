package trigram

import (
	"context"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

var webRepo = repoOf("web", map[string]string{
	"src/retry.ts":        "export class RetryPolicy {\n  maxAttempts = 3;\n}\n",
	"src/client.ts":       "import { RetryPolicy } from './retry';\nconst timeout = 30;\nnew RetryPolicy();\n",
	"src/retry_test.py":   "def test_timeout():\n    assert retry(timeout=1)\n",
	"vendor/lib/retry.js": "function retry() { return timeout; }\n",
	"README.md":           "Retries use a RetryPolicy.\r\nTimeouts are separate.\r\n",
})

func TestSearchFindsFileNamesThenLines(t *testing.T) {
	got, stats := run(t, "retry", webRepo)
	want := []string{
		"file src/retry.ts",
		"file src/retry_test.py",
		"file vendor/lib/retry.js",
		"README.md:1 Retries use a RetryPolicy.",
		"src/client.ts:1 import { RetryPolicy } from './retry';",
		"src/client.ts:3 new RetryPolicy();",
		"src/retry.ts:1 export class RetryPolicy {",
		"src/retry_test.py:2     assert retry(timeout=1)",
		"vendor/lib/retry.js:1 function retry() { return timeout; }",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results:\n got %q\nwant %q", got, want)
	}
	if stats.Total != len(want) || stats.Truncated || stats.NextOffset != 0 {
		t.Errorf("stats = %+v, want Total %d, not truncated, last page", stats, len(want))
	}
}

func TestSearchSemantics(t *testing.T) {
	tests := []struct {
		name, query string
		want        []string
	}{
		{
			// @covers syntax:and
			name:  "AND needs every term in the same file; lines of each are shown",
			query: "RetryPolicy timeout",
			want: []string{
				"README.md:1 Retries use a RetryPolicy.",
				"README.md:2 Timeouts are separate.",
				"src/client.ts:1 import { RetryPolicy } from './retry';",
				"src/client.ts:2 const timeout = 30;",
				"src/client.ts:3 new RetryPolicy();",
			},
		},
		{
			// @covers syntax:or
			name:  "OR is the union of each side's results",
			query: "maxAttempts OR separate",
			want: []string{
				"README.md:2 Timeouts are separate.",
				"src/retry.ts:2   maxAttempts = 3;",
			},
		},
		{
			// @covers syntax:not
			name:  "NOT removes files containing the term",
			query: "timeout -RetryPolicy",
			want: []string{
				"src/retry_test.py:1 def test_timeout():",
				"src/retry_test.py:2     assert retry(timeout=1)",
				"vendor/lib/retry.js:1 function retry() { return timeout; }",
			},
		},
		{
			// @covers op:f
			name:  "f: limits files by path",
			query: "f:vendor/ timeout",
			want:  []string{"vendor/lib/retry.js:1 function retry() { return timeout; }"},
		},
		{
			// @covers op:case
			name:  "case:yes matches case exactly",
			query: "case:yes Timeout",
			want:  []string{"README.md:2 Timeouts are separate."},
		},
		{
			// @covers syntax:regex
			name:  "regex terms match per line",
			query: `/^def \w+/`,
			want:  []string{"src/retry_test.py:1 def test_timeout():"},
		},
		{
			name:  "^ anchors each line, not the file",
			query: `/^  maxAttempts/`,
			want:  []string{"src/retry.ts:2   maxAttempts = 3;"},
		},
		{
			// @covers op:lang
			name:  "lang: limits files by language",
			query: "lang:python timeout",
			want: []string{
				"src/retry_test.py:1 def test_timeout():",
				"src/retry_test.py:2     assert retry(timeout=1)",
			},
		},
		{
			name:  "a quoted phrase is one literal",
			query: `"new RetryPolicy"`,
			want:  []string{"src/client.ts:3 new RetryPolicy();"},
		},
		{
			name:  "a term found nowhere returns nothing",
			query: "zzz_nothing_matches",
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := run(t, tt.query, webRepo)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s:\n got %q\nwant %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestTypeFileReturnsOnlyFileNames(t *testing.T) {
	// @covers op:type
	got, stats := run(t, "type:file retry", webRepo)
	want := []string{"file src/retry.ts", "file src/retry_test.py", "file vendor/lib/retry.js"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results:\n got %q\nwant %q", got, want)
	}
	wantHidden := []protocol.HiddenNote{{Reason: "type", Filter: "type:file", Count: 6, Unit: "matches"}}
	if len(stats.Hidden) != 1 || stats.Hidden[0].Count != 6 || stats.Hidden[0].Reason != "type" || stats.Hidden[0].Unit != "matches" {
		t.Errorf("hidden = %+v, want %+v", stats.Hidden, wantHidden)
	}
}

func TestFiltersReportWhatTheyHid(t *testing.T) {
	_, stats := run(t, "timeout -f:vendor/", webRepo)
	if len(stats.Hidden) != 1 {
		t.Fatalf("hidden = %+v, want one note", stats.Hidden)
	}
	note := stats.Hidden[0]
	if note.Reason != "pathFilter" || note.Filter != "-f:vendor/" || note.Count != 1 || note.Unit != "matches" {
		t.Errorf("note = %+v, want 1 match hidden by -f:vendor/", note)
	}
	if note.Undo.Title != "Remove -f:vendor/" || len(note.Undo.Edits) != 1 {
		t.Errorf("undo = %+v, want one edit removing the filter", note.Undo)
	}
}

func TestCaseYesReportsMatchesThatDifferOnlyInCase(t *testing.T) {
	// @covers screen:case-regex-language
	repo := repoOf("r", map[string]string{
		"policy.py": "class RetryPolicy:\n    pass\nretry_policy = RetryPolicy()\n# retrypolicy and RETRYPOLICY\n",
	})
	got, stats := run(t, "case:yes RetryPolicy", repo)
	if want := []string{"policy.py:1 class RetryPolicy:", "policy.py:3 retry_policy = RetryPolicy()"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %q, want %q", got, want)
	}
	want := []protocol.HiddenNote{{Reason: "case", Filter: "case:yes", Count: 1, Unit: "matches"}}
	if len(stats.Hidden) != 1 || stats.Hidden[0].Count != 1 || stats.Hidden[0].Reason != "case" || stats.Hidden[0].Undo.Title != "Ignore case" {
		t.Errorf("hidden = %+v, want %+v with an Ignore case undo (line 4 differs only in case)", stats.Hidden, want)
	}
}

func TestResultsFailingTwoFiltersAreNotCreditedToEither(t *testing.T) {
	_, stats := run(t, "timeout -f:vendor/ -f:lib/", webRepo)
	if len(stats.Hidden) != 0 {
		t.Errorf("hidden = %+v, want none (the vendor file fails both filters)", stats.Hidden)
	}
}

func TestCountPagesResults(t *testing.T) {
	// @covers op:count
	first, stats := runItems(t, "count:2 retry", defaultSettings, "", webRepo)
	if len(first) != 2 || stats.Total != 9 || stats.NextOffset != 2 {
		t.Fatalf("first page: %d items, stats %+v; want 2 items of 9, next at 2", len(first), stats)
	}
	second, stats := runItems(t, "count:2 retry", defaultSettings, "2", webRepo)
	if got, want := summarize(second), []string{"file vendor/lib/retry.js", "README.md:1 Retries use a RetryPolicy."}; !reflect.DeepEqual(got, want) {
		t.Errorf("second page = %q, want %q", got, want)
	}
	if stats.NextOffset != 4 {
		t.Errorf("second page NextOffset = %d, want 4", stats.NextOffset)
	}
	last, stats := runItems(t, "count:2 retry", defaultSettings, "8", webRepo)
	if len(last) != 1 || stats.NextOffset != 0 {
		t.Errorf("last page: %d items, NextOffset %d; want 1 item and no next page", len(last), stats.NextOffset)
	}
}

func TestResultsCarryHitsInUTF16(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.txt": "😀 needle and Needle\n"})
	items, _ := runItems(t, "needle", defaultSettings, "", repo)
	if len(items) != 1 {
		t.Fatalf("got %d results, want 1", len(items))
	}
	want := []protocol.Hit{{Start: 3, End: 9, TermIndex: 0}, {Start: 14, End: 20, TermIndex: 0}}
	if !reflect.DeepEqual(items[0].Hits, want) {
		t.Errorf("hits = %+v, want %+v (the emoji is two UTF-16 units)", items[0].Hits, want)
	}
	ref, ok := ParseRef(items[0].Ref)
	if !ok || ref.Line != 1 || ref.Column != 3 || ref.Length != 6 || ref.PlanID != 7 {
		t.Errorf("ref = %+v (ok %v), want line 1, column 3, length 6, plan 7", ref, ok)
	}
}

func TestHitsAreColoredByTerm(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.go": "alpha beta alpha\n"})
	items, _ := runItems(t, "beta alpha", defaultSettings, "", repo)
	want := []protocol.Hit{{Start: 0, End: 5, TermIndex: 1}, {Start: 6, End: 10, TermIndex: 0}, {Start: 11, End: 16, TermIndex: 1}}
	if len(items) != 1 || !reflect.DeepEqual(items[0].Hits, want) {
		t.Errorf("items = %+v, want one line with hits %+v", items, want)
	}
}

func TestLongLinesAreClippedAroundTheMatch(t *testing.T) {
	line := strings.Repeat("x", 1000) + "needle" + strings.Repeat("y", 1000)
	repo := repoOf("r", map[string]string{"min.js": line + "\n"})
	items, _ := runItems(t, "needle", defaultSettings, "", repo)
	if len(items) != 1 {
		t.Fatalf("got %d results, want 1", len(items))
	}
	text := items[0].Text
	if !strings.HasPrefix(text, "…") || !strings.HasSuffix(text, "…") {
		t.Errorf("clipped text = %.20q…, want it to start and end with an ellipsis", text)
	}
	if n := len([]rune(text)); n != maxResultLineRunes+2 {
		t.Errorf("clipped text has %d runes, want %d", n, maxResultLineRunes+2)
	}
	hit := items[0].Hits[0]
	if got := string([]rune(text)[hit.Start:hit.End]); got != "needle" {
		t.Errorf("hit covers %q, want needle", got)
	}
}

func TestRepoFilterSkipsOtherRepos(t *testing.T) {
	// @covers op:repo
	api := repoOf("api", map[string]string{"main.go": "timeout := 5\n"})
	got, _ := run(t, "repo:api timeout", webRepo, api)
	if want := []string{"main.go:1 timeout := 5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
}

func TestSearchStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Search(ctx, mustPlan(t, "retry", defaultSettings, ""), []Repo{webRepo}, 1, func(protocol.ResultItem) {})
	if err == nil {
		t.Errorf("Search with a cancelled context = nil error, want an error")
	}
}

func TestSearchReturnsWhatItHasWhenOverBudget(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	stats, err := Search(ctx, mustPlan(t, "retry", defaultSettings, ""), []Repo{webRepo}, 1, func(protocol.ResultItem) {})
	if err != nil || !stats.Truncated {
		t.Errorf("Search past its deadline = %+v, %v; want truncated results and no error", stats, err)
	}
}

func TestNarrowUsesTrigramsOnlyWhereSound(t *testing.T) {
	shard := webRepo.Shard
	tests := []struct {
		query   string
		narrows bool
	}{
		{"RetryPolicy", true},
		{"RetryPolicy OR maxAttempts", true},
		{"ab", false},                  // too short for a trigram
		{"x OR RetryPolicy", false},    // one side can't narrow, so the OR can't
		{"-RetryPolicy timeout", true}, // the NOT is skipped, timeout narrows
		{"/[a-z]+/", false},
	}
	for _, tt := range tests {
		plan := mustPlan(t, tt.query, defaultSettings, "")
		ids := narrowShard(shard, plan.Pred, plan.CaseSensitive)
		if (ids != nil) != tt.narrows {
			t.Errorf("narrowShard(%q) = %v, want narrowing %v", tt.query, ids, tt.narrows)
		}
	}
}

func TestGlobPathFilterListsMatchingFilesAndHighlightsTheirNames(t *testing.T) {
	items, _ := runItems(t, "f:*.ts$", defaultSettings, "", webRepo)
	var paths []string
	for _, item := range items {
		paths = append(paths, item.Path)
	}
	if want := []string{"src/client.ts", "src/retry.ts"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("f:*.ts$ lists %v, want %v", paths, want)
	}
	// "src/client.ts": the highlight covers client.ts, not the slash before it.
	if want := []protocol.Range{{Start: 4, End: 13}}; !reflect.DeepEqual(items[0].NameHits, want) {
		t.Errorf("highlight on %s = %v, want %v", items[0].Path, items[0].NameHits, want)
	}
}

// shownAsJS is text as the webview gets it: through JSON (which writes
// each invalid UTF-8 byte as U+FFFD) into a JavaScript string, in UTF-16.
func shownAsJS(t *testing.T, text string) []uint16 {
	t.Helper()
	encoded, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	var decoded string
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return utf16.Encode([]rune(decoded))
}

func TestHitsAreUTF16OffsetsIntoTheShownText(t *testing.T) {
	lines := []string{
		"😀 astral needle",
		"é combining: Needle",
		"caf\xe9 \xff\xfe invalid bytes needle",
		"\xe2\x82 truncated sequence needle",
		"𝒳𝒴 two astral 😀 needle 😀 needle",
	}
	repo := repoOf("r", map[string]string{"u.txt": strings.Join(lines, "\n") + "\n"})
	items, _ := runItems(t, "needle", defaultSettings, "", repo)
	if len(items) != len(lines) {
		t.Fatalf("results = %q, want one per line", summarize(items))
	}
	for _, item := range items {
		shown := shownAsJS(t, item.Text)
		if len(item.Hits) == 0 {
			t.Errorf("line %d: no hits", item.Line)
		}
		for _, hit := range item.Hits {
			if hit.End > len(shown) {
				t.Fatalf("line %d: hit %+v past the end of %q", item.Line, hit, item.Text)
			}
			if got := string(utf16.Decode(shown[hit.Start:hit.End])); !strings.EqualFold(got, "needle") {
				t.Errorf("line %d: hit %+v covers %q in the shown text, want needle", item.Line, hit, got)
			}
		}
		ref, _ := ParseRef(item.Ref)
		if got := string(utf16.Decode(shown[ref.Column : ref.Column+ref.Length])); !strings.EqualFold(got, "needle") {
			t.Errorf("line %d: ref column %d length %d covers %q, want needle", item.Line, ref.Column, ref.Length, got)
		}
	}
}

func TestAHugeLineIsSearchedInBoundedTimeAndMemory(t *testing.T) {
	// A minified bundle: one 2 MB line with a match every few bytes. Every
	// match used to be collected (and converted) before the line was cut
	// to the 400 runes a result shows.
	line := strings.Repeat("aaa retry ", 200_000)
	repo := repoOf("r", map[string]string{"bundle.min.js": line, "run.txt": strings.Repeat("b", 1_000) + strings.Repeat("a", 200_000)})
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	items, _ := runItems(t, "aaa", defaultSettings, "", repo)
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 32<<20 {
		t.Errorf("searching a 2 MB line allocated %d MB, want it bounded by the line, not its matches", allocated>>20)
	}
	if len(items) != 2 {
		t.Fatalf("results = %d, want one per file", len(items))
	}
	for _, item := range items {
		if n := len([]rune(item.Text)); n > maxResultLineRunes+2 || len(item.Hits) == 0 || len(item.Hits) > maxResultLineRunes {
			t.Errorf("%s: %d runes with %d hits, want a clipped window with its hits", item.Path, n, len(item.Hits))
		}
	}
	// A match longer than the window is marked up to the window's edge.
	items, _ = runItems(t, "/a+/ f:run", defaultSettings, "", repo)
	if len(items) != 1 || !reflect.DeepEqual(items[0].Hits, []protocol.Hit{{Start: 81, End: maxResultLineRunes + 1}}) {
		t.Errorf("hits of a match past the window = %+v, want it marked from where it starts to the window's end", items)
	}
}
