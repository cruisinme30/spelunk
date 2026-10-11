package query

import (
	"slices"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// planIn plans text and returns its in: and its warnings.
func planIn(t *testing.T, text string) (*Plan, []protocol.Diagnostic) {
	t.Helper()
	plan, warnings, err := NewPlan(mustParseCleanly(t, text), defaultSettings, fixedNow, "")
	if err != nil {
		t.Fatal(err)
	}
	return plan, warnings
}

// in: says which parts of files a match may be in; -in: leaves one out.
func TestInSaysWhereMatchesMayBe(t *testing.T) {
	// @covers op:in
	tests := map[string]Places{
		"retry":              0,
		"retry in:comments":  PlaceComment,
		"in:strings retry":   PlaceString,
		"retry in:code":      PlaceCode,
		"retry -in:comments": PlaceString | PlaceCode,
		"retry NOT in:code":  PlaceComment | PlaceString,
		"retry -in:strings":  PlaceComment | PlaceCode,
	}
	for text, want := range tests {
		plan, _ := planIn(t, text)
		if plan.In != want {
			t.Errorf("%s: In = %b, want %b", text, plan.In, want)
		}
		if (plan.InFilter != nil) != (want != 0) {
			t.Errorf("%s: InFilter = %+v", text, plan.InFilter)
		}
	}
	plan, _ := planIn(t, "retry -in:comments")
	if f := plan.InFilter; f.Text != "-in:comments" || f.Reason != "in" || ApplyFix("retry -in:comments", f.Undo) != "retry" {
		t.Errorf("filter = %+v", f)
	}
	// in: never shapes the predicate: it judges each match instead.
	if got := plan.Pred.String(); strings.Contains(got, "in") && !strings.Contains(got, "retry") {
		t.Errorf("pred = %s", got)
	}
}

// in: searches file text, so file names drop; definitions are code, so
// they drop only when in: leaves code out.
func TestInSearchesFileTextOnly(t *testing.T) {
	// @covers op:in
	for text, want := range map[string][]ResultKind{
		"retry":              {KindFile, KindLine},
		"retry in:comments":  {KindLine},
		"retry in:code":      {KindLine},
		"sym:retry in:code":  {KindSymbol},
		"sym:retry -in:code": {},
	} {
		plan, _ := planIn(t, text)
		var got []ResultKind
		for _, kind := range []ResultKind{KindFile, KindLine, KindSymbol} {
			if plan.Kinds[kind] {
				got = append(got, kind)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: kinds = %v, want %v", text, got, want)
		}
	}
	if plan, _ := planIn(t, "retry in:comments"); plan.MatchFilter != nil {
		t.Errorf("in: still offers fuzzy file names: %+v", plan.MatchFilter)
	}
}

// in: applies to the whole query: once, at the top level, and -in: there
// too; anywhere else it moves out, keeping its minus.
func TestInGoesAtTheTopLevelOnce(t *testing.T) {
	// @covers op:in diag:global_misplaced diag:duplicate_global
	tests := []struct{ text, code, fixed string }{
		{"(retry in:comments) OR timeout", DiagGlobalMisplaced, "in:comments ((retry) OR timeout)"},
		{"retry (timeout -in:code)", DiagGlobalMisplaced, "-in:code retry (timeout)"},
		{"retry in:comments in:strings", DiagDuplicateGlobal, "retry in:comments"},
		{"retry -in:comments in:strings", DiagDuplicateGlobal, "retry -in:comments"},
	}
	for _, test := range tests {
		q := Parse(test.text, testResolver)
		if len(q.Diagnostics) == 0 || q.Diagnostics[0].Code != test.code {
			t.Errorf("Parse(%s) = %+v, want %s", test.text, q.Diagnostics, test.code)
			continue
		}
		d := q.Diagnostics[0]
		if test.code == DiagGlobalMisplaced && !strings.HasSuffix(d.Message, "can't be inside ( ) or in an OR branch") {
			t.Errorf("Parse(%s) says %q", test.text, d.Message)
		}
		if len(d.Fixes) == 0 || ApplyFix(test.text, d.Fixes[0]) != test.fixed {
			t.Errorf("Parse(%s) fixes to %+v, want %q", test.text, d.Fixes, test.fixed)
		}
	}
	// Other globals still can't take a minus.
	if q := Parse("retry -case:yes", testResolver); len(q.Diagnostics) == 0 || q.Diagnostics[0].Code != DiagGlobalMisplaced {
		t.Errorf("-case:yes: %+v", q.Diagnostics)
	}
}

// in: reads current files, so a history query can't have it; its values
// are comments, strings and code.
func TestInIsRefusedInHistoryAndChecksItsValue(t *testing.T) {
	// @covers op:in diag:op_wrong_mode diag:bad_value
	q := Parse("author:jane retry in:comments", testResolver)
	if len(q.Diagnostics) == 0 || q.Diagnostics[0].Code != DiagOpWrongMode ||
		!strings.HasPrefix(q.Diagnostics[0].Message, "in: searches current files") {
		t.Errorf("history: %+v", q.Diagnostics)
	}
	q = Parse("retry in:docstrings", testResolver)
	if len(q.Diagnostics) == 0 || q.Diagnostics[0].Code != DiagBadValue {
		t.Fatalf("bad value: %+v", q.Diagnostics)
	}
	var titles []string
	for _, fix := range q.Diagnostics[0].Fixes {
		titles = append(titles, fix.Title)
	}
	if !slices.Contains(titles, "Use in:comments") || !slices.Contains(titles, "Use in:code") {
		t.Errorf("fixes = %q", titles)
	}
	mustParseCleanly(t, "retry in:comments at:v1.0.0")
}

// A warning says why a query can find nothing: what it asks for is only
// ever code, or file names, and in: leaves those out.
func TestInWarnsWhenItLeavesNothingToFind(t *testing.T) {
	// @covers op:in
	tests := map[string]string{
		`struct:"retry(:[x])" in:comments`:       "struct: matches code, so in:comments hides all of it",
		"sym:retry in:strings":                   "sym: finds definitions, which are code, so in:strings hides all of it",
		"kind:function retry -in:code":           "kind: finds definitions, which are code, so -in:code hides all of it",
		"type:file retry in:code":                "type:file finds file names, which are in no file's text, so in:code hides all of it",
		`struct:"retry(:[x])" in:code`:           "",
		"kind:function content:TODO in:comments": "",
		"retry in:comments":                      "",
	}
	for text, want := range tests {
		_, warnings := planIn(t, text)
		var got string
		if len(warnings) > 0 {
			got = warnings[0].Message
			if warnings[0].Severity != protocol.SeverityWarning || len(warnings[0].Fixes) == 0 ||
				!strings.HasPrefix(warnings[0].Fixes[0].Title, "Remove ") {
				t.Errorf("%s: warning %+v", text, warnings[0])
			}
		}
		if got != want {
			t.Errorf("%s: warns %q, want %q", text, got, want)
		}
	}
}

// Typing in: offers its three values.
func TestCompletingInOffersItsPlaces(t *testing.T) {
	// @covers op:in
	text := "retry in:"
	if got := labels(text, len(text)); strings.Join(got, " ") != "comments strings code" {
		t.Errorf("labels = %v", got)
	}
}
