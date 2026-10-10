package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// struct:"…" is a text term numbered like the others, matched by its
// structure and never against file names.
func TestStructIsAStructuralTextTerm(t *testing.T) {
	// @covers op:struct
	q := mustParseCleanly(t, `retry struct:"retry(:[call], attempts=:[n])" -struct:"f(:[_])"`)
	var terms []string
	walk(q.Root, func(n *protocol.Node) {
		if n.Kind == protocol.NodeKindText {
			terms = append(terms, fmt.Sprintf("%s:%s#%d only=%v", n.Match, n.Value, n.TermIndex, n.ContentOnly))
		}
	})
	want := "literal:retry#0 only=false structural:retry(:[call], attempts=:[n])#1 only=true structural:f(:[_])#2 only=true"
	if got := strings.Join(terms, " "); got != want {
		t.Errorf("terms = %s\nwant %s", got, want)
	}
	if q.Mode != protocol.ModeWorkingTree {
		t.Errorf("mode = %s, want working tree", q.Mode)
	}
}

// A template is quoted, and must make sense; what's wrong is said in words.
func TestStructExplainsBadTemplates(t *testing.T) {
	// @covers op:struct diag:bad_value
	tests := map[string]string{
		`struct:retry`:                 `struct: takes a template in quotes, as in struct:"retry(:[args])"`,
		`struct:/retry\(/`:             `struct: takes a template in quotes`,
		`struct:"retry(:[a]) :[b]"`:    "A template can't end with a hole",
		`struct:":[f](:[a])"`:          "A template can't start with a hole",
		`struct:"f(:[a]:[b])"`:         "Two holes in a row",
		`struct:"f(:[a"`:               "A hole opens with :[ but never closes with ]",
		`struct:"f(:[a-b])"`:           `":[a-b]" isn't a hole`,
		`struct:":[a]"`:                "A template needs some code",
		`struct:""`:                    "struct: needs a value",
		`struct:"   "`:                 "A template needs some code",
		`struct:"retry(:[a])" foo bar`: "",
	}
	for query, want := range tests {
		q := Parse(query, testResolver)
		var got string
		if len(q.Diagnostics) > 0 {
			got = q.Diagnostics[0].Message
			if q.Diagnostics[0].Code != DiagBadValue {
				t.Errorf("Parse(%s): code %s, want bad_value", query, q.Diagnostics[0].Code)
			}
		}
		if want == "" && got != "" || !strings.Contains(got, want) {
			t.Errorf("Parse(%s) says %q, want %q", query, got, want)
		}
	}
}

// struct: searches current files, so a history query can't have it.
func TestStructIsRefusedInHistory(t *testing.T) {
	// @covers op:struct diag:op_wrong_mode
	for _, query := range []string{
		`author:jane struct:"retry(:[a])"`,
		`msg:fix -struct:"retry(:[a])"`,
		`type:commit (struct:"retry(:[a])" timeout)`,
	} {
		q := Parse(query, testResolver)
		if len(q.Diagnostics) == 0 || q.Diagnostics[0].Code != DiagOpWrongMode {
			t.Errorf("Parse(%s) = %+v, want op_wrong_mode", query, q.Diagnostics)
			continue
		}
		d := q.Diagnostics[0]
		if !strings.HasPrefix(d.Message, "struct: searches current files") {
			t.Errorf("Parse(%s) says %q", query, d.Message)
		}
		if len(d.Fixes) != 1 || !strings.HasPrefix(d.Fixes[0].Title, `Remove struct:"retry(:[a])"`) {
			t.Errorf("Parse(%s) fixes = %+v", query, d.Fixes)
		}
	}
}

// A struct: term lowers into a content term that carries its template and
// a regex of the template's words in order, which narrows by the longest.
func TestStructLowersToATemplateTerm(t *testing.T) {
	// @covers op:struct
	tests := []struct {
		query, regex, literal string
		ignoreCase            bool
	}{
		{`struct:"retry(:[call], attempts=:[n])"`, `(?i)(?s)retry.*attempts`, "attempts", true},
		{`case:yes struct:"log.error(:[e])"`, `(?s)log.*error`, "error", false},
		{`struct:"Retry(:[x])"`, `(?s)Retry`, "Retry", false}, // smart case: a capital
		{`struct:"x[:[i]] = x[:[i]]"`, `(?i)(?s)x.*x`, "x", true},
		{`struct:"(:[a]) + 1"`, `(?i)(?s)1`, "1", true},
		{`struct:"if err != nil {"`, `(?i)(?s)if.*err.*nil`, "err", true},
	}
	settings := defaultSettings
	settings.CaseSensitive = protocol.CaseSettingSmart
	for _, test := range tests {
		plan := mustPlan(t, test.query, settings)
		if len(plan.Terms) != 1 {
			t.Fatalf("%s: %d terms", test.query, len(plan.Terms))
		}
		term := plan.Terms[0]
		if term.Structural == nil || !term.ContentOnly || term.Fuzzy || term.WholeWord || term.Multiline {
			t.Errorf("%s: term %+v", test.query, term)
		}
		if term.Re.String() != test.regex {
			t.Errorf("%s: regex %s, want %s", test.query, term.Re, test.regex)
		}
		if got := RequiredLiteral(term.Re); got != test.literal {
			t.Errorf("%s: narrows by %q, want %q", test.query, got, test.literal)
		}
		if term.IgnoreCase != test.ignoreCase {
			t.Errorf("%s: ignore case %v, want %v", test.query, term.IgnoreCase, test.ignoreCase)
		}
	}
}

// A struct: term's Find methods match its template as plain text, with
// whole matches, for callers that don't know the file's language.
func TestStructTermsFindTheirTemplate(t *testing.T) {
	plan := mustPlan(t, `struct:"retry(:[a])"`, defaultSettings)
	term := plan.Terms[0]
	text := "x = Retry(f(1)) # retry(2)\nretry(\n 3)"
	got := term.FindAllStringIndex(text, -1)
	want := [][]int{{4, 15}, {18, 26}, {27, 37}} // plain text has no comments
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("FindAllStringIndex = %v, want %v", got, want)
	}
	if loc := term.FindStringIndex(text); fmt.Sprint(loc) != "[4 15]" {
		t.Errorf("FindStringIndex = %v", loc)
	}
	if !term.MatchString("retry()") || term.MatchString("retry") {
		t.Error("MatchString")
	}
	if got := term.String(); got != `struct#0:"retry(:[a])"` {
		t.Errorf("String() = %s", got)
	}
}

// word:yes changes nothing for a template: its words already match whole.
func TestWordYesLeavesTemplatesAlone(t *testing.T) {
	plan := mustPlan(t, `word:yes struct:"retry(:[a])"`, defaultSettings)
	if plan.Terms[0].WholeWord {
		t.Error("a template term matches whole words already; WholeWord is set")
	}
}
