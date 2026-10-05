package query

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// querySeeds are queries for the fuzzers to start from: the mockups'
// queries, every diagnostic, and inputs that once panicked, hung or gave
// bad spans.
var querySeeds = []string{
	"",
	"   \t\r\n",
	"retry_policy",
	`author:jane f:.*test\.py$ timeout`,
	`author:jane (timeout OR retry) -f:vendor/ since:6m`,
	`case:yes /Retry(Policy|Config)/ lang:python`,
	`msg:"fix flaky" repo:web count:20 type:commit`,
	`sym:/^Retry/ f:src/**/test_*.py since:30m`,
	"(case:yes a) OR b",
	"x -case:yes -case:yes -case:yes -case:yes",
	"a ) b () c OR AND -",
	"f: author: since: sym:",
	"/a{1000}{1000}/",
	"f:a{1000}{1000}",
	"/((a{100}){100}){100}/ /(?i)[\\p{L}k]{1000}/ sym:/\\pN{999}/",
	"f:(.{999}){2} repo:x{1000}",
	"(?i)ABC /(?i)abc/ sym:/(?U)a+/",
	"f:**/[ f:{a,b f:src/[a-z f:? f:\\",
	"😀 f:😀*.go \U0001F600\u0301 \u05e9\u05dc\u05d5\u05dd",
	"\x00 \xff\xfe f:\xc3\x28",
	"std::vector http://x.com a:b:c",
	"((((((((((a",
	"-(a OR -b) AND (c OR)",
	"lang:pyhton since:6x count:0 count:all type:x case:maybe",
	`"abc\`,
	`/abc\`,
	`f:/abc\`,
	`msg:"abc\`,
}

// FuzzParse checks Parse's invariants on any text: it never panics, every
// span lies inside the text on code point boundaries, fixes apply and fix
// their problem, the same text parses the same way, and a query without
// errors always plans.
func FuzzParse(f *testing.F) {
	for _, seed := range querySeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		q := Parse(text, testResolver)
		src := newSource(text)
		if q.Raw != text {
			t.Fatalf("Parse(%q).Raw = %q", text, q.Raw)
		}
		if again := Parse(text, testResolver); !reflect.DeepEqual(again, q) {
			t.Fatalf("Parse(%q) differs from one call to the next", text)
		}
		if q.Root != nil {
			checkNode(t, src, q.Root, protocol.Span{Start: 0, End: src.length()})
		}
		if len(q.Diagnostics) > maxDiagnostics {
			t.Fatalf("Parse(%q) has %d diagnostics, want at most %d", text, len(q.Diagnostics), maxDiagnostics)
		}
		hasError := false
		for _, d := range q.Diagnostics {
			hasError = hasError || d.Severity == protocol.SeverityError
			checkSpan(t, src, d.Span, d.Code)
			for _, fix := range d.Fixes {
				checkFix(t, src, text, d, fix)
			}
		}
		if q.Root != nil && !hasError {
			checkPlan(t, q)
		}
	})
}

// checkSpan fails unless span lies inside the text, in order, on code point boundaries.
func checkSpan(t *testing.T, src *source, span protocol.Span, what string) {
	t.Helper()
	_, startOK := slices.BinarySearch(src.offsets, span.Start)
	_, endOK := slices.BinarySearch(src.offsets, span.End)
	if span.Start > span.End || !startOK || !endOK {
		t.Fatalf("%q: %s span %v is not a range of whole code points in 0..%d", src.text, what, span, src.length())
	}
}

// checkNode checks the spans of node and its children, which must lie inside parent.
func checkNode(t *testing.T, src *source, node *protocol.Node, parent protocol.Span) {
	t.Helper()
	checkSpan(t, src, node.Span, node.Kind)
	if node.Span.Start < parent.Start || node.Span.End > parent.End {
		t.Fatalf("%q: %s span %v is outside its parent's %v", src.text, node.Kind, node.Span, parent)
	}
	for i := range node.Children {
		checkNode(t, src, &node.Children[i], node.Span)
	}
	if node.Child != nil {
		checkNode(t, src, node.Child, node.Span)
	}
}

// checkFix applies fix and checks that it leaves fewer problems of its kind.
func checkFix(t *testing.T, src *source, text string, d protocol.Diagnostic, fix protocol.Fix) {
	t.Helper()
	edits := slices.Clone(fix.Edits)
	slices.SortFunc(edits, func(a, b protocol.TextEdit) int { return a.Span.Start - b.Span.Start })
	for i, edit := range edits {
		checkSpan(t, src, edit.Span, "fix "+fix.Title)
		if i > 0 && edit.Span.Start < edits[i-1].Span.End {
			t.Fatalf("%q: fix %q has overlapping edits", text, fix.Title)
		}
	}
	before := Parse(text, testResolver).Diagnostics
	if d.Severity != protocol.SeverityError || len(text) > maxQueryLength || len(before) == maxDiagnostics {
		return // a capped list can't show that one problem went
	}
	fixed := ApplyFix(text, fix)
	if count(Parse(fixed, testResolver).Diagnostics, d.Code) >= count(before, d.Code) {
		t.Fatalf("fix %q on %q gives %q, which still has as many %s", fix.Title, text, fixed, d.Code)
	}
}

// checkPlan plans a query without errors and runs what engines run on the plan.
func checkPlan(t *testing.T, q protocol.ParsedQuery) {
	t.Helper()
	for _, caseSensitive := range []protocol.CaseSetting{protocol.CaseSettingOff, protocol.CaseSettingOn, protocol.CaseSettingSmart} {
		plan, _, err := NewPlan(q, protocol.Settings{DefaultCount: 500, CaseSensitive: caseSensitive}, fixedNow, "")
		if err != nil {
			t.Fatalf("NewPlan(%q) = %v, want a plan: the query has no errors", q.Raw, err)
		}
		_ = plan.Pred.String()
		folded := plan.IgnoringCase()
		for i, term := range plan.Terms {
			RequiredLiteral(term.Re)
			if folded.Terms[i] == nil {
				t.Fatalf("NewPlan(%q).IgnoringCase() lost term %d", q.Raw, i)
			}
		}
		for _, leaf := range []bool{false, true} {
			Eval(plan.Pred, func(Pred) bool { return leaf })
			Contributing(plan.Pred, func(Pred) bool { return leaf })
		}
	}
}

// FuzzComplete checks that completions never panic, whatever the cursor,
// and that their edits lie inside the text.
func FuzzComplete(f *testing.F) {
	for _, seed := range querySeeds {
		f.Add(seed, len(seed))
		f.Add(seed, len(seed)/2)
	}
	f.Add("since:3", 7)
	f.Add("since:99999mi", 13)
	f.Add("f:*.p", 5)
	f.Add("lang:", -1)
	f.Fuzz(func(t *testing.T, text string, cursor int) {
		src := newSource(text)
		for _, completion := range Complete(text, cursor, testResolver, fixedNow) {
			for _, edit := range completion.Insert.Edits {
				checkSpan(t, src, edit.Span, "completion "+completion.Label)
			}
			Parse(ApplyFix(text, completion.Insert), testResolver)
		}
	})
}

// FuzzGlobPattern checks that every glob turns into a regex that compiles,
// with or without (?i).
func FuzzGlobPattern(f *testing.F) {
	for _, seed := range []string{"*.go", "src/**/test_*.py", "**", "a/**", "[", "{a,b", `\`, `a\*`, "^*.go$", "?", "😀*"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, glob string) {
		looksLikeGlob(glob, true)
		looksLikeGlob(glob, false)
		pattern := globPattern(glob)
		for _, prefix := range []string{"", "(?i)"} {
			if _, err := regexp.Compile(prefix + pattern); err != nil {
				t.Fatalf("globPattern(%q) = %q, which doesn't compile: %v", glob, pattern, err)
			}
		}
	})
}

// TestParseIsLinearOnLongQueries parses a megabyte of each construct that
// once took quadratic time or overflowed the stack, and a query at the
// length limit whose misplaced globals took factorial time.
func TestParseIsLinearOnLongQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test; skipped with -short")
	}
	const size = 1 << 20
	queries := map[string]string{
		"stray closing parens":         strings.Repeat(")", size),
		"opening parens":               strings.Repeat("(", size),
		"nested groups":                strings.Repeat("(a ", size/3),
		"minus signs":                  strings.Repeat("-", size) + "x",
		"misplaced globals":            "x" + strings.Repeat(" -case:yes", size/10),
		"a short query's globals":      "x" + strings.Repeat(" -case:yes", maxQueryLength/10-1),
		"keywords":                     strings.Repeat("OR AND ", size/7),
		"unknown operators":            strings.Repeat("zz:x ", size/5),
		"an unclosed quote of escapes": `"` + strings.Repeat(`\`, size),
	}
	for name, text := range queries {
		done := make(chan struct{})
		go func() {
			defer close(done)
			parseOnce(text)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("parsing a megabyte of %s took over 10 s", name)
		}
	}
}
