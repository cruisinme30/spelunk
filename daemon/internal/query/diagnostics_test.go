package query

import (
	"strings"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
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
		{"unclosed quote", `x "abc`, DiagUnclosedQuote, `"abc`, `x "abc"`},
		{"unclosed regex", "x /abc", DiagUnclosedRegex, "/abc", "x /abc/"},
		{"unknown operator offers the nearest", "sinse:6m timeout", DiagUnknownOperator, "sinse:", "since:6m timeout"},
		{"unknown operator far from any name offers quoting", "wibble:x y", DiagUnknownOperator, "wibble:", `"wibble:x" y`},
		{"since: with a bad unit", "since:6x a", DiagBadValue, "since:6x", "since:30d a"},
		{"count: below one", "count:-1 a", DiagBadValue, "count:-1", "count:50 a"},
		{"lang: misspelled offers the nearest language", "lang:pyhton a", DiagBadValue, "lang:pyhton", "lang:python a"},
		{"operator without a value", "author: a", DiagBadValue, "author:", ""},
		{"author: as a regex", "author:/j.*/ a", DiagBadValue, "author:/j.*/", ""},
		{"case: inside an OR branch moves out and the OR keeps its meaning", "(case:yes a) OR b", DiagGlobalMisplaced, "case:yes", "case:yes ((a) OR b)"},
		{"count: after minus moves with its minus removed", "a -count:5", DiagGlobalMisplaced, "count:5", "count:5 a"},
		{"case: twice", "case:yes a case:no", DiagDuplicateGlobal, "case:no", "case:yes a"},
		{"OR mixing commits and lines", "author:jane OR f:x", DiagMixedModeOr, "author:jane OR f:x", ""},
		{"sym: in a history query", "author:jane sym:Foo", DiagOpWrongMode, "sym:Foo", "author:jane"},
		{"invalid regex term", "/Retry(/", DiagInvalidRegex, "/Retry(/", ""},
		{"invalid f: regex", "f:a[b x", DiagInvalidRegex, "a[b", ""},
		{"only excluded terms", "-timeout", DiagNoPositiveTerm, "-timeout", ""},
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
	queries := []string{"a ) b", "a () b", "a OR", `x "abc`, "x /abc", "sinse:6m x", "since:6x a", "lang:pyhton a", "(case:yes a) OR b", "case:yes a case:no", "author:jane sym:Foo"}
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
	if q := Parse("since:6m timeout", testResolver); len(q.Diagnostics) != 0 {
		t.Errorf("Parse(since:6m) diagnostics = %+v, want none: six months is plausible", q.Diagnostics)
	}
}

func TestALongQueryReportsACappedNumberOfProblems(t *testing.T) {
	q := Parse(strings.Repeat(")", 10*maxQueryLength), testResolver)
	if len(q.Diagnostics) != maxDiagnostics || count(q.Diagnostics, DiagQueryTooLong) != 1 {
		t.Errorf("Parse(10000 × \")\") has %d diagnostics with %d %s, want %d with 1", len(q.Diagnostics), count(q.Diagnostics, DiagQueryTooLong), DiagQueryTooLong, maxDiagnostics)
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
