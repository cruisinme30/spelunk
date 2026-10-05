package query

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// Each diagnostic code: the query that triggers it, the span it points at,
// and what its first fix turns the query into ("" when it has no fix).
func TestDiagnostics(t *testing.T) {
	// @covers diag:unclosed_paren diag:unmatched_paren diag:empty_group diag:missing_operand diag:unclosed_quote diag:unclosed_regex diag:unknown_operator diag:bad_value diag:global_misplaced diag:duplicate_global diag:mixed_mode_or diag:op_wrong_mode diag:invalid_regex diag:no_positive_term diag:query_too_long
	tests := []struct {
		name, query string
		code        string
		spanText    string
		fixed       string
	}{
		{"unclosed paren closes after the OR operand", "author:jane (timeout OR retry -f:vendor/", DiagUnclosedParen, "(", "author:jane (timeout OR retry) -f:vendor/"},
		{"unclosed paren without OR closes at the end", "x (a b", DiagUnclosedParen, "(", "x (a b)"},
		{"stray closing paren", "a ) b", DiagUnmatchedParen, ")", "a b"},
		{"empty parentheses", "a () b", DiagEmptyGroup, "()", "a b"},
		{"OR with nothing after it", "a OR", DiagMissingOperand, "OR", "a"},
		{"OR with nothing before it", "OR a", DiagMissingOperand, "OR", "a"},
		{"AND with nothing after it", "a AND", DiagMissingOperand, "AND", "a"},
		{"NOT with nothing after it", "a NOT", DiagMissingOperand, "NOT", "a"},
		{"NOT before a closing paren", "(a NOT) b", DiagMissingOperand, "NOT", "(a) b"},
		{"unclosed quote", `x "abc`, DiagUnclosedQuote, `"abc`, `x "abc"`},
		{"unclosed regex", "x /abc", DiagUnclosedRegex, "/abc", "x /abc/"},
		{"unknown operator offers the nearest", "sinse:6m timeout", DiagUnknownOperator, "sinse:", "since:6m timeout"},
		{"unknown operator far from any name offers quoting", "wibble:x y", DiagUnknownOperator, "wibble:", `"wibble:x" y`},
		{"since: with a bad unit", "since:6x a", DiagBadValue, "since:6x", "since:30d a"},
		{"since: a day that doesn't exist offers the month's last", "since:2026-02-30 a", DiagBadValue, "since:2026-02-30", "since:2026-02-28 a"},
		{"since: a month that doesn't exist offers the nearest", "since:2026-13 a", DiagBadValue, "since:2026-13", "since:2026-12 a"},
		{"since: a date with a time", "since:2026-09-30T10 a", DiagBadValue, "since:2026-09-30T10", "since:30d a"},
		{"until: a day that doesn't exist offers the month's last", "until:2026-04-31 a", DiagBadValue, "until:2026-04-31", "until:2026-04-30 a"},
		{"until: with a bad unit", "until:6x a", DiagBadValue, "until:6x", "until:30d a"},
		{"count: below one", "count:-1 a", DiagBadValue, "count:-1", "count:50 a"},
		{"lang: misspelled offers the nearest language", "lang:pyhton a", DiagBadValue, "lang:pyhton", "lang:python a"},
		{"operator without a value", "author: a", DiagBadValue, "author:", ""},
		{"author: as a regex", "author:/j.*/ a", DiagBadValue, "author:/j.*/", ""},
		{"case: inside an OR branch moves out and the OR keeps its meaning", "(case:yes a) OR b", DiagGlobalMisplaced, "case:yes", "case:yes ((a) OR b)"},
		{"count: after minus moves with its minus removed", "a -count:5", DiagGlobalMisplaced, "count:5", "count:5 a"},
		{"count: after NOT moves with its NOT removed", "a NOT count:5", DiagGlobalMisplaced, "count:5", "count:5 a"},
		{"case: twice", "case:yes a case:no", DiagDuplicateGlobal, "case:no", "case:yes a"},
		{"OR mixing commits and lines", "author:jane OR f:x", DiagMixedModeOr, "author:jane OR f:x", ""},
		{"sym: in a history query", "author:jane sym:Foo", DiagOpWrongMode, "sym:Foo", "author:jane"},
		{"kind: in a history query", "author:jane kind:class", DiagOpWrongMode, "kind:class", "author:jane"},
		{"ref: in a history query", "msg:fix ref:Retry", DiagOpWrongMode, "ref:Retry", "msg:fix"},
		{"kind: with a bad value", "sym:Retry kind:func", DiagBadValue, "kind:func", "sym:Retry kind:function"},
		{"ref: as a regex", "ref:/Retry.*/", DiagBadValue, "ref:/Retry.*/", ""},
		{"order: in a history query", "author:jane order:path", DiagOpWrongMode, "order:path", "author:jane"},
		{"is:open in a history query", "author:jane is:open", DiagOpWrongMode, "is:open", "author:jane"},
		{"is:changed in a history query", "msg:fix is:changed", DiagOpWrongMode, "is:changed", "msg:fix"},
		{"is: with a bad value", "a is:tests", DiagBadValue, "is:tests", "a is:open"},
		{"order: with a bad value", "a order:random", DiagBadValue, "order:random", "a order:best"},
		{"invalid regex term", "/Retry(/", DiagInvalidRegex, "/Retry(/", ""},
		{"invalid f: regex", "f:a[b x", DiagInvalidRegex, "a[b", ""},
		{"only excluded terms", "-timeout", DiagNoPositiveTerm, "-timeout", ""},
		{"only terms excluded with NOT", "NOT timeout", DiagNoPositiveTerm, "NOT timeout", ""},
		{"globals don't count as positive", "case:yes -timeout", DiagNoPositiveTerm, "case:yes -timeout", ""},
		{"query over 1000 characters", strings.Repeat("a", 1001), DiagQueryTooLong, "a", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := Parse(tt.query, testResolver)
			var found *protocol.Diagnostic
			for i := range q.Diagnostics {
				if q.Diagnostics[i].Code == tt.code {
					found = &q.Diagnostics[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("Parse(%q) diagnostics = %v, want %s", tt.query, codes(q.Diagnostics), tt.code)
			}
			if found.Severity != protocol.SeverityError {
				t.Errorf("%s severity = %s, want error", tt.code, found.Severity)
			}
			src := newSource(tt.query)
			if got := src.slice(found.Span.Start, found.Span.End); got != tt.spanText {
				t.Errorf("%s points at %q, want %q", tt.code, got, tt.spanText)
			}
			if tt.fixed == "" {
				return
			}
			if len(found.Fixes) == 0 {
				t.Fatalf("%s has no fix, want one giving %q", tt.code, tt.fixed)
			}
			if got := ApplyFix(tt.query, found.Fixes[0]); got != tt.fixed {
				t.Errorf("first fix %q gives %q, want %q", found.Fixes[0].Title, got, tt.fixed)
			}
		})
	}
}

func TestTwoProblemsAreReportedInTextOrder(t *testing.T) {
	q := Parse("author:jane (timeout OR retry -f:vendor/ sinse:6m", testResolver)
	if got := strings.Join(codes(q.Diagnostics), " "); got != "unclosed_paren unknown_operator" {
		t.Fatalf("diagnostics = %s, want unclosed_paren unknown_operator", got)
	}
	if got := q.Diagnostics[0].Fixes[0].Title; got != "Close it after retry" {
		t.Errorf("fix title = %q, want %q", got, "Close it after retry")
	}
	if got := q.Diagnostics[1].Fixes[0].Title; got != "Change to since:" {
		t.Errorf("fix title = %q, want %q", got, "Change to since:")
	}
}

func TestEveryFixProducesAQueryWithoutThatDiagnostic(t *testing.T) {
	queries := []string{"a ) b", "a () b", "a OR", `x "abc`, "x /abc", "sinse:6m x", "since:6x a", "since:2026-02-30 a", "since:2026-00 a", "until:2026-02-30 a", "u:6x a", "lang:pyhton a", "(case:yes a) OR b", "case:yes a case:no", "author:jane sym:Foo"}
	for _, query := range queries {
		q := Parse(query, testResolver)
		for _, d := range q.Diagnostics {
			for _, fix := range d.Fixes {
				fixed := ApplyFix(query, fix)
				for _, after := range Parse(fixed, testResolver).Diagnostics {
					if after.Code == d.Code {
						t.Errorf("fix %q on %q gives %q with %s, want it gone", fix.Title, query, fixed, d.Code)
					}
				}
			}
		}
	}
}

func TestSinceDatesThatDoNotExistSayWhy(t *testing.T) {
	// @covers op:since op:until diag:bad_value
	tests := map[string]string{
		"since:2026-02-30": "2026-02-30 isn't a date: February 2026 has 28 days",
		"since:2028-02-30": "2028-02-30 isn't a date: February 2028 has 29 days",
		"since:2026-09-31": "2026-09-31 isn't a date: September 2026 has 30 days",
		"since:2026-09-00": "2026-09-00 isn't a date: September 2026 has 30 days",
		"since:2026-13":    "2026-13 isn't a date: months go from 01 to 12",
		"since:2026-00-10": "2026-00-10 isn't a date: months go from 01 to 12",
		"until:2026-02-29": "2026-02-29 isn't a date: February 2026 has 28 days",
		"since:2026-9":     "since: takes today, yesterday, a date (2026-09-30 or 2026-09), or a number and a unit: min, h, d, w, m (months) or y",
		`since:"2026-09"`:  "since: takes today, yesterday, a date (2026-09-30 or 2026-09), or a number and a unit: min, h, d, w, m (months) or y",
	}
	for text, want := range tests {
		q := Parse(text+" a", testResolver)
		if len(q.Diagnostics) != 1 || q.Diagnostics[0].Code != DiagBadValue || q.Diagnostics[0].Message != want {
			t.Errorf("Parse(%q) diagnostics = %+v, want one bad_value %q", text, q.Diagnostics, want)
		}
	}
}

func TestSinceInMonthsThatLooksLikeMinutesWarns(t *testing.T) {
	q := Parse("since:30m timeout", testResolver)
	if len(q.Diagnostics) != 1 {
		t.Fatalf("Parse(since:30m) diagnostics = %+v, want one warning", q.Diagnostics)
	}
	d := q.Diagnostics[0]
	if d.Severity != protocol.SeverityWarning || d.Code != DiagBadValue {
		t.Errorf("diagnostic = %s %s, want a bad_value warning", d.Severity, d.Code)
	}
	if got := ApplyFix("since:30m timeout", d.Fixes[0]); got != "since:30min timeout" {
		t.Errorf("fix gives %q, want %q", got, "since:30min timeout")
	}
	if q := Parse("until:30m timeout", testResolver); len(q.Diagnostics) != 1 || ApplyFix("until:30m timeout", q.Diagnostics[0].Fixes[0]) != "until:30min timeout" {
		t.Errorf("Parse(until:30m) diagnostics = %+v, want a warning offering until:30min", q.Diagnostics)
	}
	if q := Parse("since:6m timeout", testResolver); len(q.Diagnostics) != 0 {
		t.Errorf("Parse(since:6m) diagnostics = %+v, want none: six months is plausible", q.Diagnostics)
	}
}

func TestALongQueryReportsACappedNumberOfProblems(t *testing.T) {
	q := Parse(strings.Repeat(")", maxParsedLength), testResolver)
	if len(q.Diagnostics) != maxDiagnostics || count(q.Diagnostics, DiagQueryTooLong) != 1 {
		t.Errorf("Parse(%d × \")\") has %d diagnostics with %d %s, want %d with 1", maxParsedLength, len(q.Diagnostics), count(q.Diagnostics, DiagQueryTooLong), DiagQueryTooLong, maxDiagnostics)
	}
}

func TestAPastedMegabyteIsOnlyReportedAsTooLong(t *testing.T) {
	text := "timeout " + strings.Repeat("😀)", 1<<19)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	q := Parse(text, testResolver)
	completions := Complete(text, len(text), testResolver, time.Now())
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Errorf("parsing and completing a 2 MB query allocated %d MB, want it bounded by the parsed prefix", allocated>>20)
	}
	want := protocol.Span{Start: maxQueryLength, End: utf16Length(text)}
	if len(q.Diagnostics) != 1 || q.Diagnostics[0].Code != DiagQueryTooLong || q.Diagnostics[0].Span != want {
		t.Errorf("Parse(2 MB) diagnostics = %+v, want only %s over %v", q.Diagnostics, DiagQueryTooLong, want)
	}
	if q.Raw != text || q.Root == nil || q.Root.Span.End > maxParsedLength {
		t.Errorf("Parse(2 MB) keeps Raw and a tree of the first %d units, got root %+v", maxParsedLength, q.Root)
	}
	if len(completions) != 0 {
		t.Errorf("Complete past the parsed prefix = %d completions, want none", len(completions))
	}
}

func TestClosingFixesSurviveATrailingBackslash(t *testing.T) {
	tests := []struct{ query, fixed string }{
		{`"abc\`, `"abc\\"`},
		{`/abc\`, `/abc\\/`},
		{`msg:"abc\`, `msg:"abc\\"`},
		{"\"\xee\\", "\"�\\\\\""},
		{`(x "abc`, `(x "abc")`},
	}
	for _, tt := range tests {
		q := Parse(tt.query, testResolver)
		fixed := ApplyFix(tt.query, q.Diagnostics[0].Fixes[0])
		if fixed != tt.fixed || len(Parse(fixed, testResolver).Diagnostics) > 0 {
			t.Errorf("first fix of %q gives %q with %v, want %q without problems", tt.query, fixed, codes(Parse(fixed, testResolver).Diagnostics), tt.fixed)
		}
	}
}

func TestAnInvalidOperandAfterMinusIsNotAlsoMissing(t *testing.T) {
	q := Parse("timeout -sinse:6m", testResolver)
	if got := codes(q.Diagnostics); len(got) != 1 || got[0] != DiagUnknownOperator {
		t.Errorf("Parse(%q) diagnostics = %v, want only %s", q.Raw, got, DiagUnknownOperator)
	}
}

func TestFixesThatRemoveTextNeverJoinWords(t *testing.T) {
	tests := []struct{ query, code, fixed string }{
		{"timeout sinse:6m -) x", DiagUnmatchedParen, "timeout sinse:6m - x"},
		{"a -() b", DiagEmptyGroup, "a - b"},
		{"(case:yes a) OR b", DiagGlobalMisplaced, "case:yes ((a) OR b)"},
		{"a ) b", DiagUnmatchedParen, "a b"},
	}
	for _, tt := range tests {
		q := Parse(tt.query, testResolver)
		var got []string
		for _, d := range q.Diagnostics {
			if d.Code == tt.code {
				got = append(got, ApplyFix(tt.query, d.Fixes[0]))
			}
		}
		if len(got) != 1 || got[0] != tt.fixed {
			t.Errorf("Parse(%q) %s fix gives %q, want %q", tt.query, tt.code, got, tt.fixed)
		}
	}
}

// A plain word with a pipe, like daemon|search, matches the pipe too, so it
// warns and offers OR and a regex. The query still runs.
func TestPipeInWord(t *testing.T) {
	// @covers diag:pipe_in_word
	tests := []struct {
		name, query string
		fixed       []string // what each fix turns the query into
	}{
		{"alone in a group needs no new parentheses", "(daemon|search)", []string{"(daemon OR search)", "(/daemon|search/)"}},
		{"alone in the query", "daemon|search", []string{"daemon OR search", "/daemon|search/"}},
		{"beside another term keeps its meaning", "a|b c", []string{"(a OR b) c", "/a|b/ c"}},
		{"after a minus stays excluded", "x -a|b", []string{"x -(a OR b)", "x -/a|b/"}},
		{"three words", "a|b|c", []string{"a OR b OR c", "/a|b|c/"}},
		{"regex characters are escaped", "a.b|c/d", []string{"a.b OR c/d", "/a\\.b|c\\/d/"}},
		{"a keyword offers only the regex", "x|OR", []string{"/x|OR/"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := Parse(tt.query, testResolver)
			if len(q.Diagnostics) != 1 || q.Diagnostics[0].Code != DiagPipeInWord {
				t.Fatalf("Parse(%q) diagnostics = %v, want one %s", tt.query, codes(q.Diagnostics), DiagPipeInWord)
			}
			found := q.Diagnostics[0]
			if found.Severity != protocol.SeverityWarning {
				t.Errorf("severity = %s, want warning", found.Severity)
			}
			if len(found.Fixes) != len(tt.fixed) {
				t.Fatalf("%d fixes, want %d", len(found.Fixes), len(tt.fixed))
			}
			for i, fix := range found.Fixes {
				if got := ApplyFix(tt.query, fix); got != tt.fixed[i] {
					t.Errorf("fix %q gives %q, want %q", fix.Title, got, tt.fixed[i])
				}
				if fixed := Parse(ApplyFix(tt.query, fix), testResolver); len(fixed.Diagnostics) != 0 {
					t.Errorf("fix %q leaves diagnostics %v", fix.Title, codes(fixed.Diagnostics))
				}
			}
		})
	}
	for _, query := range []string{"a||b", "a|", "|a", `"a|b"`, "/a|b/"} {
		if q := Parse(query, testResolver); len(q.Diagnostics) != 0 {
			t.Errorf("Parse(%q) diagnostics = %v, want none", query, codes(q.Diagnostics))
		}
	}
}
