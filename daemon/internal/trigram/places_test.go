package trigram

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// retryFile is the mock's retry.ts: retry in code, in strings, in line and
// block comments, and on lines that have it in two places.
var retryFile = strings.Join([]string{
	"// TODO(retry): back off longer when the gateway answers 429", // 1
	`import { loadPolicy } from "./policy";`,
	"",
	"/* A retry budget per checkout. Once it is spent, the retry",
	"   gives up and the error reaches the shopper. */", // 5
	"export async function withRetry<T>(call: () => Promise<T>): Promise<T> {",
	`  const policy = loadPolicy("retry"); // "retry" is the policy's name`,
	"  for (let attempt = 0; attempt < policy.max; attempt++) {",
	"    try {",
	"      return await call();", // 10
	"    } catch (err) {",
	"      log(`retry ${attempt} in ${policy.delay(attempt)}ms`);",
	"      retryCount++; // retry again after the delay",
	"    }",
	"  }", // 15
	`  throw new Error("retry budget spent");`,
	"}",
	"",
	"let retryCount = 0;",
	"",
}, "\n")

var inRepo = repoOf("web-checkout", map[string]string{
	"src/api/retry.ts": retryFile,
	"src/payments/client.py": strings.Join([]string{
		"def charge(body):",
		`    """Charge a card, retrying on connection errors."""`,
		`    log.info("retrying charge")`,
		"    return retry(post, body)  # retry once more",
		"",
	}, "\n"),
	"docs/retry-notes.md":  "# Retries\n\nThe gateway asks us to retry after 429s.\n",
	"docs/retry-notes.txt": "Checkout retry notes\n",
	"config/retry.xml":     "<!-- retry --><policy name=\"retry\"/>\n",
})

// hitsOf renders each code result as "path:line" and the text of its hits,
// so a test can see which part of a line in: kept.
func hitsOf(items []protocol.ResultItem) []string {
	out := []string{}
	for _, item := range items {
		if item.Kind != "line" {
			out = append(out, "file "+item.Path)
			continue
		}
		var parts []string
		for _, hit := range item.Hits {
			parts = append(parts, item.Text[hit.Start:hit.End]+"@"+strconv.Itoa(hit.Start))
		}
		out = append(out, item.Path+":"+strconv.Itoa(item.Line)+" "+strings.Join(parts, ","))
	}
	return out
}

// Each match is judged by where it is, on its own: a line with retry in
// its code and in its comment keeps only the part in: asks for, and the
// file's other matches don't decide it.
func TestInKeepsEachMatchByWhereItIs(t *testing.T) {
	// @covers op:in
	tests := map[string][]string{
		"retry in:comments f:retry.ts": {
			"src/api/retry.ts:1 retry@8",
			"src/api/retry.ts:4 retry@5,retry@54",
			"src/api/retry.ts:7 retry@42",
			"src/api/retry.ts:13 retry@23",
		},
		"retry in:strings f:retry.ts": {
			"src/api/retry.ts:7 retry@29",
			"src/api/retry.ts:12 retry@11",
			"src/api/retry.ts:16 retry@19",
		},
		"retry in:code f:retry.ts": {
			"src/api/retry.ts:6 Retry@26",
			"src/api/retry.ts:13 retry@6",
			"src/api/retry.ts:19 retry@4",
		},
		"retry -in:comments f:retry.ts": {
			"src/api/retry.ts:6 Retry@26",
			"src/api/retry.ts:7 retry@29",
			"src/api/retry.ts:12 retry@11",
			"src/api/retry.ts:13 retry@6",
			"src/api/retry.ts:16 retry@19",
			"src/api/retry.ts:19 retry@4",
		},
	}
	for query, want := range tests {
		items, _ := runItems(t, query, defaultSettings, "", inRepo)
		if got := hitsOf(items); !slices.Equal(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", query, got, want)
		}
	}
}

// A Python docstring is a comment; the log message beside it a string.
func TestInReadsDocstringsAsComments(t *testing.T) {
	// @covers op:in
	items, _ := runItems(t, "retry in:comments f:client.py", defaultSettings, "", inRepo)
	want := []string{"src/payments/client.py:2 retry@22", "src/payments/client.py:4 retry@32"}
	if got := hitsOf(items); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	items, _ = runItems(t, "retry in:strings f:client.py", defaultSettings, "", inRepo)
	if got := hitsOf(items); !slices.Equal(got, []string{"src/payments/client.py:3 retry@14"}) {
		t.Errorf("strings: got %q", got)
	}
}

// Plain text and Markdown have nothing to tell apart, so every in: keeps
// their matches; a file in a language without comment rules is all code.
func TestInLeavesProseAloneAndReadsUnknownLanguagesAsCode(t *testing.T) {
	// @covers op:in
	for _, in := range []string{"in:comments", "in:strings", "in:code", "-in:code"} {
		items, _ := runItems(t, "retry f:docs/ "+in, defaultSettings, "", inRepo)
		want := []string{"docs/retry-notes.md:3 retry@23", "docs/retry-notes.txt:1 retry@9"}
		if got := hitsOf(items); !slices.Equal(got, want) {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
	items, _ := runItems(t, "retry f:xml in:code", defaultSettings, "", inRepo)
	if got := hitsOf(items); !slices.Equal(got, []string{"config/retry.xml:1 retry@5,retry@28"}) {
		t.Errorf("xml in:code: got %q", got)
	}
	if items, _ := runItems(t, "retry f:xml in:comments", defaultSettings, "", inRepo); len(items) != 0 {
		t.Errorf("xml in:comments: got %q", hitsOf(items))
	}
}

// A match that runs from code into a comment is in neither: in: drops it,
// and -in: keeps it, as it isn't wholly in what -in: leaves out.
func TestInJudgesAMatchAcrossPlacesAsNeither(t *testing.T) {
	// @covers op:in
	query := `/retryCount\+\+; \/\/ retry/`
	for in, want := range map[string]int{"": 1, "in:comments": 0, "in:code": 0, "in:strings": 0, "-in:comments": 1, "-in:code": 1} {
		items, _ := runItems(t, query+" "+in, defaultSettings, "", inRepo)
		if len(items) != want {
			t.Errorf("%s: %d results, want %d", in, len(items), want)
		}
	}
}

// A regex across lines is kept when the whole match is in the place.
func TestInJudgesAMatchAcrossLinesWhole(t *testing.T) {
	// @covers op:in
	query := `/the retry\n\s*gives up/ f:retry.ts`
	for in, want := range map[string]int{"in:comments": 1, "in:code": 0, "-in:comments": 0} {
		items, _ := runItems(t, query+" "+in, defaultSettings, "", inRepo)
		if len(items) != want {
			t.Errorf("%s: %d results, want %d", in, len(items), want)
		}
	}
}

// A note counts the results in: hid (lines, 8 less 4 here), and its undo
// takes in: out; file names and,
// once code is left out, definitions aren't searched.
func TestInSaysWhatItHid(t *testing.T) {
	// @covers op:in
	items, stats := runItems(t, "retry in:comments f:retry.ts", defaultSettings, "", inRepo)
	for _, item := range items {
		if item.Kind != "line" {
			t.Errorf("in: returned a %s result: %+v", item.Kind, item)
		}
	}
	var note *protocol.HiddenNote
	for i := range stats.Hidden {
		if stats.Hidden[i].Reason == "in" {
			note = &stats.Hidden[i]
		}
	}
	if note == nil {
		t.Fatalf("no in: note in %+v", stats.Hidden)
	}
	if note.Count != 4 || note.Filter != "in:comments" || note.Unit != "matches" || note.Undo.Title != "Show them" {
		t.Errorf("note = %+v", note)
	}
	if _, stats := runItems(t, "retry in:comments f:docs/", defaultSettings, "", inRepo); len(stats.Hidden) != 0 {
		t.Errorf("prose only: notes %+v, want none", stats.Hidden)
	}
}

// struct: matches code, so in: keeps its matches only when it keeps code.
func TestInKeepsStructuralMatchesWithCode(t *testing.T) {
	// @covers op:in op:struct
	query := `struct:"loadPolicy(:[x])" f:retry.ts`
	for in, want := range map[string]int{"": 1, "in:code": 1, "-in:comments": 1, "in:comments": 0, "in:strings": 0} {
		items, _ := runItems(t, query+" "+in, defaultSettings, "", inRepo)
		if len(items) != want {
			t.Errorf("%s: %d results, want %d", in, len(items), want)
		}
	}
}

// Replace changes only the matches in: keeps, never the code beside a kept
// comment on the same line.
func TestInReplacesOnlyTheMatchesItKeeps(t *testing.T) {
	// @covers op:in rpc:replace/plan
	plan := mustPlan(t, "retry in:comments f:retry.ts", defaultSettings, "")
	out, err := Replacements(context.Background(), plan, []Repo{inRepo}, plan.Terms[0], "retries", 100)
	if err != nil {
		t.Fatal(err)
	}
	if out.Matches != 5 {
		t.Errorf("matches = %d, want 5", out.Matches)
	}
	var lines []string
	for _, file := range out.Files {
		for _, line := range file.Lines {
			for _, edit := range line.Edits {
				lines = append(lines, strconv.Itoa(line.Line)+":"+strconv.Itoa(edit.Start))
			}
		}
	}
	if want := []string{"1:8", "4:5", "4:54", "7:42", "13:23"}; !slices.Equal(lines, want) {
		t.Errorf("edits at %q, want %q", lines, want)
	}
}

// The preview marks only what in: keeps, on the result's line and around it.
func TestInMarksOnlyKeptMatchesInThePreview(t *testing.T) {
	// @covers op:in rpc:preview/get
	root := testutil.WriteTree(t, map[string]string{"src/api/retry.ts": retryFile})
	repo := &Repo{ID: "r", Name: "web-checkout", Root: root}
	plan := mustPlan(t, "retry in:comments", defaultSettings, "")
	got, err := Preview(repo, Ref{PlanID: 1, RepoID: "r", Path: "src/api/retry.ts", Line: 13, Column: 23, Length: 5}, plan, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.LineHits{{Line: 13, Ranges: []protocol.Hit{{Start: 23, End: 28}}}}
	if !reflect.DeepEqual(got.Hits, want) {
		t.Errorf("hits = %+v, want %+v", got.Hits, want)
	}
}

// A notebook is read cell by cell: a markdown cell is prose, which in:
// leaves alone, and a code cell is read by its language.
func TestInReadsANotebookCellByCell(t *testing.T) {
	// @covers op:in
	repo := notebookRepo(t, map[string]string{"churn.ipynb": churnNotebook}, BuildOptions{})
	items, _ := runItems(t, "retry_policy in:code", defaultSettings, "", repo)
	var got []string
	for _, item := range items {
		if item.Kind == "line" && item.Cell != nil {
			got = append(got, item.Cell.Kind+":"+strconv.Itoa(item.Cell.Number))
		}
	}
	// The markdown cell (1) stays; cell 4's print() string doesn't.
	if want := []string{"markdown:1", "code:2", "code:2", "code:3"}; !slices.Equal(got, want) {
		t.Errorf("cells = %q, want %q", got, want)
	}
}

// kind: with a term that matches bodies finds lines inside definitions, and
// in: judges those lines like any other: the comments inside a function.
func TestInFindsCommentsInsideDefinitions(t *testing.T) {
	// @covers op:in op:kind
	repo := notebookRepo(t, map[string]string{"client.py": strings.Join([]string{
		"def charge(body):",
		`    """Charge a card, retrying on connection errors."""`,
		`    log.info("retrying charge")`,
		"    return retry(post, body)  # retry once more",
		"",
		"# retry is imported below",
	}, "\n")}, BuildOptions{})
	items, _ := runItems(t, "kind:function content:retry in:comments", defaultSettings, "", repo)
	// Not the comment outside the function, nor the string and code inside it.
	want := []string{"client.py:2 retry@22", "client.py:4 retry@32"}
	if got := hitsOf(items); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
