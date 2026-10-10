package query

import (
	"regexp"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// A regex spans lines only when it has a newline in it, written as \n or
// another way; \s, [^x] and . don't make it span lines.
func TestOnlyARegexWithANewlineSpansLines(t *testing.T) {
	// @covers syntax:regex
	tests := map[string]bool{
		`foo\nbar`:         true,
		`foo\n`:            true,
		`[^\n]+`:           true, // still stops at a line end, so harmless
		`a\x0ab`:           true,
		`a\012b`:           true,
		`a\x{0A}b`:         true,
		`(a|b\n)c`:         true,
		`foo\s+bar`:        false,
		`[^x]+`:            false,
		`a.*b`:             false,
		`^foo$`:            false,
		`foo\\n`:           false, // a backslash, then n
		`foo\\\nbar`:       true,  // a backslash, then a newline
		`retry\(.*\)\Nope`: false, // invalid: never spans lines
	}
	for pattern, want := range tests {
		if got := spansLines(pattern); got != want {
			t.Errorf("spansLines(%q) = %v, want %v", pattern, got, want)
		}
	}
}

// A regex term with \n is lowered as Multiline, with the m flag so ^ and $
// still mean the start and end of a line; other terms are left alone.
func TestARegexWithANewlineIsLoweredAsMultiline(t *testing.T) {
	tests := []struct {
		query     string
		multiline bool
		pattern   string
	}{
		{`/except\n\s+raise/`, true, `(?i)(?m)except\n\s+raise`},
		{`case:yes /except\n\s+raise/`, true, `(?m)except\n\s+raise`},
		{`content:/^def \w+\(\):\n/`, true, `(?i)(?m)^def \w+\(\):\n`},
		{`/except\s+raise/`, false, `(?i)except\s+raise`},
		{`"except\n"`, false, `(?i)except\\n`}, // a phrase is literal text
		{`except\n`, false, `(?i)except\\n`},
	}
	for _, tt := range tests {
		plan := mustPlan(t, tt.query, defaultSettings)
		if len(plan.Terms) != 1 {
			t.Fatalf("%s: %d terms, want 1", tt.query, len(plan.Terms))
		}
		term := plan.Terms[0]
		if term.Multiline != tt.multiline || term.Re.String() != tt.pattern {
			t.Errorf("%s: Multiline %v, Re %q; want %v, %q", tt.query, term.Multiline, term.Re, tt.multiline, tt.pattern)
		}
	}
}

// Relaxed copies of a plan, which count what case:yes and word:yes hid,
// keep matching across lines.
func TestRelaxedPlansKeepMultiline(t *testing.T) {
	plan := mustPlan(t, `case:yes word:yes /Except\n/`, defaultSettings)
	for name, relaxed := range map[string]*Plan{"IgnoringCase": plan.IgnoringCase(), "MatchingPartialWords": plan.MatchingPartialWords()} {
		if term := relaxed.Terms[0]; !term.Multiline || !term.Re.MatchString("x\nExcept\ny") {
			t.Errorf("%s: term %+v doesn't match across lines", name, term)
		}
	}
}

// The literal used to narrow files never holds a newline: a file whose
// lines end in "\r\n" must stay a candidate for a regex with \n.
func TestRequiredLiteralNeverHoldsANewline(t *testing.T) {
	tests := map[string]string{
		`except TransientError:\n`:            "except TransientError:",
		`\):\n\s*retry`:                       "retry",
		`(?i)Retry\nPolicy`:                   "policy", // the longer piece, folded
		`if err != nil \{\n\t+return err\n\}`: "if err != nil {",
		`a\nb`:                                "a",
	}
	for pattern, want := range tests {
		if got := RequiredLiteral(regexp.MustCompile(pattern)); got != want {
			t.Errorf("RequiredLiteral(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// In a history query a regex with \n can never match, since commits are
// searched one changed line at a time, so it warns; the query still runs.
func TestMultilineRegexInHistoryWarns(t *testing.T) {
	// @covers diag:multiline_in_history
	for _, query := range []string{`author:jane /except\n\s+raise/`, `type:commit (retry OR content:/a\nb/)`, `type:added /x\x0ay/`} {
		q := Parse(query, testResolver)
		if len(q.Diagnostics) != 1 || q.Diagnostics[0].Code != DiagMultilineInHistory || q.Diagnostics[0].Severity != protocol.SeverityWarning {
			t.Errorf("Parse(%q) diagnostics = %+v, want one multiline_in_history warning", query, q.Diagnostics)
			continue
		}
		if span := q.Diagnostics[0].Span; query[span.Start:span.End] == "" || query[span.End-1] != '/' {
			t.Errorf("Parse(%q) warning span %+v doesn't cover the regex", query, span)
		}
		if _, _, err := NewPlan(q, defaultSettings, fixedNow, ""); err != nil {
			t.Errorf("NewPlan(%q): %v, want it to run", query, err)
		}
	}
	for _, quiet := range []string{`/except\n\s+raise/`, `author:jane /except\s+raise/`, `author:jane "a\n"`, `msg:/a\nb/ retry`} {
		if q := Parse(quiet, testResolver); len(q.Diagnostics) != 0 {
			t.Errorf("Parse(%q) diagnostics = %+v, want none", quiet, q.Diagnostics)
		}
	}
}
