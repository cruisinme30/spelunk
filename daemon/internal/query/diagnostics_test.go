package query

import (
	"strings"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// applyFix applies a fix's edits (UTF-16 spans) to text.
func applyFix(text string, fix protocol.Fix) string {
	src := newSource(text)
	var b strings.Builder
	position := 0
	// Edits in a fix never overlap; apply them in span order.
	edits := append([]protocol.TextEdit(nil), fix.Edits...)
	for i := 1; i < len(edits); i++ {
		for j := i; j > 0 && edits[j].Span.Start < edits[j-1].Span.Start; j-- {
			edits[j], edits[j-1] = edits[j-1], edits[j]
		}
	}
	for _, edit := range edits {
		b.WriteString(src.slice(position, edit.Span.Start))
		b.WriteString(edit.NewText)
		position = edit.Span.End
	}
	b.WriteString(src.slice(position, src.length()))
	return b.String()
}

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
			if got := applyFix(tt.query, found.Fixes[0]); got != tt.fixed {
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
				fixed := applyFix(query, fix)
				for _, after := range Parse(fixed, testResolver).Diagnostics {
					if after.Code == d.Code {
						t.Errorf("fix %q on %q gives %q, which still has %s", fix.Title, query, fixed, d.Code)
					}
				}
			}
		}
	}
}
