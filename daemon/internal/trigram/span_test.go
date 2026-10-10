package trigram

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// spanFile is a Python client whose two retry loops each have an except
// clause followed, on the next line, by the delay it waits.
var spanFile = strings.Join([]string{
	"class PaymentsClient:", // 1
	"    def charge(self):",
	"        try:",
	"            return self._post()",
	"        except TransientError:", // 5
	"            delay = self.retry_policy.next_delay()",
	"        raise TransientError()",
	"",
	"    def refund(self):",
	"        try:", // 10
	"            return self._delete()",
	"        except TransientError:",
	"            delay = 0",
	"",
}, "\n")

var spanRepo = repoOf("r", map[string]string{"client.py": spanFile})

// spanResult renders a code result as its first line, then each line it
// spans, with the matched parts in [brackets].
func spanResult(item protocol.ResultItem) string {
	lines := []string{fmt.Sprintf("%d %s", item.Line, bracket(item.Text, item.Hits))}
	for i, later := range item.SpanLines {
		lines = append(lines, fmt.Sprintf("%d %s", item.Line+1+i, bracket(later.Text, later.Hits)))
	}
	if item.EndLine != 0 {
		lines = append(lines, fmt.Sprintf("ends on %d", item.EndLine))
	}
	return strings.Join(lines, " | ")
}

// bracket marks hits (UTF-16 offsets, ASCII text here) in text. A hit
// overlapping the one before shows only what that one didn't, so two
// terms' hits on the same text read "[b][]".
func bracket(text string, hits []protocol.Hit) string {
	var b strings.Builder
	at := 0
	for _, hit := range hits {
		start := max(hit.Start, at)
		b.WriteString(text[at:start] + "[" + text[start:max(hit.End, start)] + "]")
		at = max(hit.End, start)
	}
	b.WriteString(text[at:])
	return b.String()
}

func spanResults(t *testing.T, text string, settings protocol.Settings, repos ...Repo) []string {
	t.Helper()
	items, _ := runItems(t, text, settings, "", repos...)
	out := []string{}
	for _, item := range items {
		if item.Kind == query.KindLine {
			out = append(out, spanResult(item))
		}
	}
	return out
}

// A regex with \n finds each match across lines once, on the line it
// starts, with the lines it runs onto and each line's part of the match.
func TestARegexWithANewlineMatchesAcrossLines(t *testing.T) {
	// @covers syntax:regex
	got := spanResults(t, `type:code /except TransientError:\n\s*delay/`, defaultSettings, spanRepo)
	want := []string{
		"5         [except TransientError:] | 6 [            delay] = self.retry_policy.next_delay() | ends on 6",
		"12         [except TransientError:] | 13 [            delay] = 0 | ends on 13",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Without \n a regex still matches one line at a time: \s doesn't run onto
// the next line.
func TestARegexWithoutANewlineStaysOnOneLine(t *testing.T) {
	if got := spanResults(t, `type:code /TransientError:\s+delay/`, defaultSettings, spanRepo); len(got) != 0 {
		t.Errorf("results = %q, want none: \\s stays on one line", got)
	}
}

// A match that ends with the newline itself shows on one line.
func TestAMatchEndingAtANewlineIsOneLine(t *testing.T) {
	items, _ := runItems(t, `type:code /self\._post\(\)\n/`, defaultSettings, "", spanRepo)
	if len(items) != 1 || items[0].Line != 4 || items[0].SpanLines != nil || items[0].EndLine != 0 {
		t.Fatalf("results = %+v, want line 4 alone", items)
	}
	if got := bracket(items[0].Text, items[0].Hits); got != "            return [self._post()]" {
		t.Errorf("line 4 = %q", got)
	}
}

// ^ and $ still mean the start and end of a line, as they do line by line.
func TestAnchorsMatchLinesInARegexThatSpansLines(t *testing.T) {
	got := spanResults(t, `type:code /^        try:\n.*_delete\(\)$/`, defaultSettings, spanRepo)
	want := []string{"10 [        try:] | 11 [            return self._delete()] | ends on 11"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
}

// Lines that end in "\r\n" match the same as ones that end in "\n", and
// the "\r" is never shown or marked. The trigram prefilter keeps such a
// file a candidate.
func TestCRLFLinesMatchAcrossLines(t *testing.T) {
	crlf := repoOf("r", map[string]string{"client.py": strings.ReplaceAll(spanFile, "\n", "\r\n")})
	if got, want := spanResults(t, `type:code /TransientError:\n\s*delay/`, defaultSettings, crlf),
		spanResults(t, `type:code /TransientError:\n\s*delay/`, defaultSettings, spanRepo); !reflect.DeepEqual(got, want) || len(got) != 2 {
		t.Errorf("CRLF results = %q, want the LF file's %q", got, want)
	}
	// Each piece of the regex's literal is long enough to narrow by.
	if got := spanResults(t, `type:code /retry_policy\.next_delay\(\)\n        raise/`, defaultSettings, crlf); len(got) != 1 {
		t.Errorf("results = %q, want the one match in the CRLF file", got)
	}
}

// Matches whose lines overlap are one result, and so is another term's
// match on a line the span covers: no line is shown twice.
func TestMatchesThatShareALineAreOneResult(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.txt": "a\nb\nc\nd\n"})
	// a\nb and b\nc share line 2; d is a line of its own.
	got := spanResults(t, `type:code ((/a\nb/ /b\nc/) OR d)`, defaultSettings, repo)
	want := []string{"1 [a] | 2 [b][] | 3 [c] | ends on 3", "4 [d]"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
	// Another term's match on a spanned line joins the span, in its own color.
	items, stats := runItems(t, `type:code /except TransientError:\n\s*delay/ next_delay`, defaultSettings, "", spanRepo)
	if len(items) != 2 || stats.Total != 2 {
		t.Fatalf("results = %q (total %d), want the two spans: next_delay is on a line the first spans", spanResultsOf(items), stats.Total)
	}
	hits := items[0].SpanLines[0].Hits
	if len(hits) != 2 || hits[0].TermIndex != 0 || hits[1].TermIndex != 1 {
		t.Errorf("line 6 hits = %+v, want the span's then next_delay's", hits)
	}
}

func spanResultsOf(items []protocol.ResultItem) []string {
	out := []string{}
	for _, item := range items {
		out = append(out, spanResult(item))
	}
	return out
}

// A match that starts on a line another term matched joins it, starting
// where the earlier of the two does.
func TestASpanStartingOnAMatchedLineJoinsIt(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.txt": "x retry y\nz\n"})
	items, _ := runItems(t, `type:code retry /y\nz/`, defaultSettings, "", repo)
	if len(items) != 1 {
		t.Fatalf("results = %q, want one", spanResultsOf(items))
	}
	if got := spanResult(items[0]); got != "1 x [retry] [y] | 2 [z] | ends on 2" {
		t.Errorf("result = %q", got)
	}
	ref, _ := ParseRef(items[0].Ref)
	if ref.Line != 1 || ref.Column != 2 || ref.EndLine != 2 || ref.EndColumn != 1 {
		t.Errorf("ref = %+v, want 1:2 to 2:1 (from retry to the end of z)", ref)
	}
}

// A long match shows maxSpanLines lines after its first and says where it
// ends; it is still one result.
func TestALongMatchShowsItsFirstLinesAndWhereItEnds(t *testing.T) {
	var lines []string
	for i := range 30 {
		lines = append(lines, fmt.Sprintf("line %d", i+1))
	}
	repo := repoOf("r", map[string]string{"a.txt": strings.Join(lines, "\n") + "\n"})
	items, stats := runItems(t, `type:code /line 3\n(.*\n)*line 25/`, defaultSettings, "", repo)
	if len(items) != 1 || stats.Total != 1 {
		t.Fatalf("results = %q, want one", spanResultsOf(items))
	}
	item := items[0]
	if item.Line != 3 || len(item.SpanLines) != maxSpanLines || item.EndLine != 25 || item.SpanLines[maxSpanLines-1].Text != "line 13" {
		t.Errorf("result from %d with %d more lines ending on %d; want 3, %d more, 25", item.Line, len(item.SpanLines), item.EndLine, maxSpanLines)
	}
	ref, _ := ParseRef(item.Ref)
	if ref.EndLine != 25 || ref.EndColumn != len("line 25") {
		t.Errorf("ref ends at %d:%d, want 25:%d", ref.EndLine, ref.EndColumn, len("line 25"))
	}
}

// A match that marks nothing on any line it shows, such as a lone
// newline, highlights nothing, so it isn't a result.
func TestAMatchOfOnlyNewlinesIsDropped(t *testing.T) {
	if got := spanResults(t, `type:code /\n/`, defaultSettings, spanRepo); len(got) != 0 {
		t.Errorf("results = %q, want none", got)
	}
	// But a blank line inside a match is part of it.
	got := spanResults(t, `type:code /next_delay\(\)\n.*\n\n    def/`, defaultSettings, spanRepo)
	want := []string{"6             delay = self.retry_policy.[next_delay()] | 7 [        raise TransientError()] | 8  | 9 [    def] refund(self): | ends on 9"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
}

// Hits and columns count UTF-16 units, as the panel and VS Code do.
func TestSpanColumnsAreUTF16(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.txt": "😀 é retry\n😀 next\n"})
	items, _ := runItems(t, `type:code /retry\n😀 n/`, defaultSettings, "", repo)
	if len(items) != 1 {
		t.Fatalf("results = %+v", items)
	}
	item := items[0]
	if !reflect.DeepEqual(item.Hits, []protocol.Hit{{Start: 5, End: 10}}) || !reflect.DeepEqual(item.SpanLines[0].Hits, []protocol.Hit{{Start: 0, End: 4}}) {
		t.Errorf("hits = %+v then %+v, want 5-10 then 0-4", item.Hits, item.SpanLines[0].Hits)
	}
	ref, _ := ParseRef(item.Ref)
	if ref.Column != 5 || ref.EndColumn != 4 {
		t.Errorf("ref = %+v, want column 5 to 4", ref)
	}
}

// A long line in a span is clipped around its part of the match, as a
// result's first line is.
func TestLongSpannedLinesAreClipped(t *testing.T) {
	long := strings.Repeat("x", 2*maxResultLineRunes)
	repo := repoOf("r", map[string]string{"a.min.js": "retry\n" + long + "end\n"})
	items, _ := runItems(t, `type:code /retry\nx+end/`, defaultSettings, "", repo)
	if len(items) != 1 || len(items[0].SpanLines) != 1 {
		t.Fatalf("results = %+v", items)
	}
	later := items[0].SpanLines[0]
	if n := len([]rune(later.Text)); n > maxResultLineRunes+2 || !strings.HasSuffix(later.Text, "…") || len(later.Hits) != 1 {
		t.Errorf("spanned line of %d runes %q with hits %+v, want clipped", n, later.Text[:20], later.Hits)
	}
}

// Context lines come before the first line and after the last.
func TestContextSurroundsTheWholeSpan(t *testing.T) {
	items, _ := runItems(t, `type:code /except TransientError:\n\s*delay = self/`, withContext(defaultSettings, 1), "", spanRepo)
	if len(items) != 1 {
		t.Fatalf("results = %q", spanResultsOf(items))
	}
	if !reflect.DeepEqual(items[0].ContextBefore, []string{"            return self._post()"}) ||
		!reflect.DeepEqual(items[0].ContextAfter, []string{"        raise TransientError()"}) {
		t.Errorf("context = %q, %q; want line 4 before and line 7 after", items[0].ContextBefore, items[0].ContextAfter)
	}
}

// A regex that spans lines works with the rest of the language: NOT,
// word:yes, case:yes and what it hid, and file filters.
func TestARegexThatSpansLinesCombinesWithTheRest(t *testing.T) {
	other := repoOf("o", map[string]string{"errors.py": "except TransientError:\n    pass\n", "notes.md": "Except TransientError:\n    delay\n"})
	got, _ := run(t, `type:code -/:\n\s*delay/ TransientError`, spanRepo, other)
	if len(got) != 1 || !strings.HasPrefix(got[0], "errors.py:1 ") {
		t.Errorf("NOT: results = %q, want only errors.py's line", got)
	}
	if got := spanResults(t, `type:code word:yes /Error:\n\s*delay/`, defaultSettings, spanRepo); len(got) != 0 {
		t.Errorf("word:yes: results = %q, want none: Error is part of TransientError", got)
	}
	_, stats := runItems(t, `type:code case:yes /except TransientError:\n\s*delay/`, defaultSettings, "", spanRepo, other)
	if stats.Total != 2 || hiddenCount(stats, "case") != 1 {
		t.Errorf("case:yes: total %d, hidden %+v; want 2 and notes.md's 1 hidden by case", stats.Total, stats.Hidden)
	}
	if got := spanResults(t, `type:code f:\.md$ /TransientError:\n\s*delay/`, defaultSettings, spanRepo, other); len(got) != 1 {
		t.Errorf("f: results = %q, want notes.md's one", got)
	}
}

func hiddenCount(stats engine.Stats, reason string) int {
	for _, note := range stats.Hidden {
		if note.Reason == reason {
			return note.Count
		}
	}
	return 0
}

// A history search matches a commit's changed lines one at a time, so a
// regex with \n never matches them (and the query warns).
func TestLineFinderKeepsARegexThatSpansLinesToOneLine(t *testing.T) {
	plan := mustPlan(t, `/a\nb/`, defaultSettings, "")
	if got := NewLineFinder().Lines([]byte("a\nb\n"), plan.Terms[0]); len(got) != 0 {
		t.Errorf("Lines = %v, want none", got)
	}
}

// The preview shows the whole match, marks each of its lines, and says
// where it ends; it reads "\r\n" files as the search does.
func TestPreviewShowsAMatchAcrossLines(t *testing.T) {
	// @covers rpc:preview/get
	for name, content := range map[string]string{"LF": spanFile, "CRLF": strings.ReplaceAll(spanFile, "\n", "\r\n")} {
		t.Run(name, func(t *testing.T) {
			root := testutil.WriteTree(t, map[string]string{"client.py": content})
			repo := &Repo{ID: "r", Name: "r", Root: root}
			plan := mustPlan(t, `/except TransientError:\n\s*delay/`, defaultSettings, "")
			ref := Ref{PlanID: 1, RepoID: "r", Path: "client.py", Line: 5, Column: 8, Length: 22, EndLine: 6, EndColumn: 17}
			got, err := Preview(repo, ref, plan, 1)
			if err != nil {
				t.Fatal(err)
			}
			if got.FirstLine != 4 || len(got.Lines) != 4 || got.FocusLine != 5 || got.FocusEndLine != 6 {
				t.Errorf("window from %d of %d lines, focus %d-%d; want 4-7, focus 5-6", got.FirstLine, len(got.Lines), got.FocusLine, got.FocusEndLine)
			}
			want := []protocol.LineHits{
				{Line: 5, Ranges: []protocol.Hit{{Start: 8, End: 30}}},
				{Line: 6, Ranges: []protocol.Hit{{Start: 0, End: 17}}},
			}
			if !reflect.DeepEqual(got.Hits, want) {
				t.Errorf("hits = %+v, want %+v", got.Hits, want)
			}
		})
	}
}

// A preview of a match longer than maxPreviewSpanLines shows its start;
// a single-line result has no focus end.
func TestPreviewOfAVeryLongMatchIsCapped(t *testing.T) {
	var lines []string
	for i := range maxPreviewSpanLines + 50 {
		lines = append(lines, fmt.Sprintf("line %d", i+1))
	}
	root := testutil.WriteTree(t, map[string]string{"a.txt": strings.Join(lines, "\n")})
	repo := &Repo{ID: "r", Name: "r", Root: root}
	ref := Ref{RepoID: "r", Path: "a.txt", Line: 2, EndLine: maxPreviewSpanLines + 40, EndColumn: 3}
	got, err := Preview(repo, ref, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if last := got.FirstLine + len(got.Lines) - 1; last != 2+maxPreviewSpanLines+3 || got.FocusEndLine != maxPreviewSpanLines+40 {
		t.Errorf("preview ends at %d, focus end %d; want %d and %d", last, got.FocusEndLine, 2+maxPreviewSpanLines+3, maxPreviewSpanLines+40)
	}
	single, _ := Preview(repo, Ref{RepoID: "r", Path: "a.txt", Line: 2, Length: 1}, nil, 1)
	if single.FocusEndLine != 0 {
		t.Errorf("single-line focus end = %d, want none", single.FocusEndLine)
	}
}

// Opening a match that spans lines selects all of it.
func TestOpenTargetSelectsAMatchAcrossLines(t *testing.T) {
	// @covers rpc:open/resolve
	root := testutil.WriteTree(t, map[string]string{"client.py": spanFile})
	repo := &Repo{ID: "r", Name: "r", Root: root}
	got, err := OpenTarget(repo, Ref{RepoID: "r", Path: "client.py", Line: 5, Column: 8, Length: 22, EndLine: 6, EndColumn: 17})
	if err != nil {
		t.Fatal(err)
	}
	if got.Line != 5 || got.Column != 9 || got.EndLine != 6 || got.EndColumn != 18 {
		t.Errorf("target = %+v, want 5:9 to 6:18 (1-based)", got)
	}
	single, _ := OpenTarget(repo, Ref{RepoID: "r", Path: "client.py", Line: 5, Column: 8, Length: 22})
	if single.EndLine != 0 || single.EndColumn != 0 {
		t.Errorf("single-line target = %+v, want no end", single)
	}
}

// Joining many matches that share lines stays linear: a file where every
// line ends one match and starts the next is one result, found quickly.
func TestJoiningManyMatchesIsQuick(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.txt": strings.Repeat("ab\n", 200_000)})
	start := time.Now()
	items, stats := runItems(t, `type:code /b\na/`, defaultSettings, "", repo)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("search took %s", elapsed)
	}
	if len(items) != 1 || stats.Total != 1 || len(items[0].SpanLines) != maxSpanLines || items[0].EndLine != 200_000 {
		t.Fatalf("got %d results (total %d), want one from line 1 to 200000", len(items), stats.Total)
	}
}
