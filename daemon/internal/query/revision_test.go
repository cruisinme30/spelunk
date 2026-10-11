package query

import (
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// at: names the revision whose files the whole query searches: a global
// that keeps the query on files, not commits.
func TestAtIsAGlobalThatSearchesFilesAtARevision(t *testing.T) {
	// @covers op:at
	for _, text := range []string{
		"at:v1.0.0 retry_after f:payments/", "retry at:main", "at:3f9c2e1 sym:Retry", "at:HEAD~10 retry",
		"at:release/2.x retry", "at:HEAD^2 retry", "at:HEAD@{1} retry", `at:"v1.0.0" retry`, "at:v1.0.0+build retry",
		"at:6m retry", "at:2026-01-31 retry", "at:2026-01 retry", "at:yesterday retry", "at:v1.0.0 is:test retry",
		"at:v1.0.0 struct:\"retry(:[a])\"", "at:v1.0.0 order:file retry", "at:v1.0.0 type:file retry",
	} {
		q := mustParseCleanly(t, text)
		if q.Mode != protocol.ModeWorkingTree {
			t.Errorf("Parse(%s): mode %s, want working tree", text, q.Mode)
		}
		if q.Globals.At == "" {
			t.Errorf("Parse(%s): globals.at is empty", text)
		}
	}
	if q := mustParseCleanly(t, `at:"v1.0.0" retry`); q.Globals.At != "v1.0.0" {
		t.Errorf(`at:"v1.0.0" sets globals.at %q, want v1.0.0 without quotes`, q.Globals.At)
	}
	if q := mustParseCleanly(t, "retry"); q.Globals.At != "" {
		t.Errorf("a query without at: has globals.at %q", q.Globals.At)
	}
}

func TestAtPlansTheRevisionOrTheTimeItEnds(t *testing.T) {
	// @covers op:at
	plan := mustPlan(t, "at:v1.0.0 retry", defaultSettings)
	if plan.At != "v1.0.0" || !plan.AtTime.IsZero() {
		t.Errorf("at:v1.0.0 plans At %q AtTime %v, want the name and no time", plan.At, plan.AtTime)
	}
	if kinds(plan) != "file,line" {
		t.Errorf("at:v1.0.0 retry returns %s, want file names and code as without at:", kinds(plan))
	}
	for value, want := range map[string]time.Time{
		"6m":         fixedNow.AddDate(0, -6, 0),
		"2h":         fixedNow.Add(-2 * time.Hour),
		"2026-01-31": time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), // the end of that day
		"2026-01":    time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), // the end of that month
		"yesterday":  time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
		"today":      fixedNow, // the end of today is in the future: now
		"2026-10":    fixedNow,
	} {
		plan := mustPlan(t, "at:"+value+" retry", defaultSettings)
		if plan.At != value || !plan.AtTime.Equal(want) {
			t.Errorf("at:%s plans At %q AtTime %v, want AtTime %v", value, plan.At, plan.AtTime, want)
		}
	}
	for _, value := range []string{"6m", "today", "2026-01-31"} {
		// Quoted, the value names a branch or tag, as asRevision reads it.
		if plan := mustPlan(t, `at:"`+value+`" retry`, defaultSettings); plan.At != value || !plan.AtTime.IsZero() {
			t.Errorf(`at:"%s" plans At %q AtTime %v, want the name and no time`, value, plan.At, plan.AtTime)
		}
	}
	if plan := mustPlan(t, "retry", defaultSettings); plan.At != "" || !plan.AtTime.IsZero() {
		t.Errorf("a plan without at: has At %q AtTime %v", plan.At, plan.AtTime)
	}
}

func TestAtRefusesWhatNamesNoSingleRevision(t *testing.T) {
	// @covers op:at diag:bad_value
	for _, text := range []string{
		"at:-v retry", "at:--output=x retry", "at:v1..v2 retry", "at:HEAD:README.md retry", "at:/v1/ retry",
		`at:"two words" retry`, "at:.hidden retry", "at:2026-13 retry", "at:2026-02-30 retry",
	} {
		q := Parse(text, testResolver)
		if len(q.Diagnostics) == 0 || q.Diagnostics[0].Code != DiagBadValue {
			t.Errorf("Parse(%s) = %+v, want bad_value", text, q.Diagnostics)
		}
	}
	q := Parse("at:v1..v2 retry", testResolver)
	if !strings.Contains(q.Diagnostics[0].Message, "a tag, branch or commit") {
		t.Errorf("at:v1..v2 says %q, want what at: takes", q.Diagnostics[0].Message)
	}
	if q := Parse("at:2026-02-30 retry", testResolver); !strings.Contains(q.Diagnostics[0].Message, "February 2026 has 28 days") {
		t.Errorf("at:2026-02-30 says %q, want why the date isn't real", q.Diagnostics[0].Message)
	}
}

func TestAtAppliesOnceToTheWholeQuery(t *testing.T) {
	// @covers op:at diag:duplicate_global diag:global_misplaced
	if q := Parse("at:v1 at:v2 retry", testResolver); count(q.Diagnostics, DiagDuplicateGlobal) != 1 {
		t.Errorf("two at: = %v, want duplicate_global", codes(q.Diagnostics))
	}
	for _, text := range []string{"(at:v1 retry) OR timeout", "-at:v1 retry", "retry OR at:v1"} {
		if q := Parse(text, testResolver); count(q.Diagnostics, DiagGlobalMisplaced) != 1 {
			t.Errorf("Parse(%s) = %v, want global_misplaced", text, codes(q.Diagnostics))
		}
	}
}

// at: searches files, so it can't be part of a commit search.
func TestAtIsWrongInAHistoryQuery(t *testing.T) {
	// @covers op:at diag:op_wrong_mode
	for _, text := range []string{"at:v1.0.0 author:jane retry", "at:v1.0.0 msg:retry", "at:v1.0.0 type:added retry"} {
		q := Parse(text, testResolver)
		if count(q.Diagnostics, DiagOpWrongMode) != 1 {
			t.Fatalf("Parse(%s) = %v, want op_wrong_mode", text, codes(q.Diagnostics))
		}
		d := q.Diagnostics[0]
		if !strings.Contains(d.Message, "at: searches the files of one revision, but this query searches commits") {
			t.Errorf("Parse(%s) says %q", text, d.Message)
		}
		if len(d.Fixes) != 1 || d.Fixes[0].Title != "Remove at:v1.0.0" {
			t.Errorf("Parse(%s) offers %+v, want to remove at:", text, d.Fixes)
		}
	}
}

// Operators about files as they are now, or about when they changed, have
// no answer for a past revision's files.
func TestAtRefusesOperatorsAboutFilesNow(t *testing.T) {
	// @covers op:at diag:op_with_at
	tests := map[string]string{
		"at:v1.0.0 is:open retry":            "is: describes files as they are now, but at:v1.0.0 searches the files as they were then",
		"at:v1.0.0 retry -is:changed":        "is: describes files as they are now",
		"at:v1.0.0 since:30d retry":          "since: picks files by when they last changed",
		"at:6m until:1y retry":               "until: picks files by when they last changed, but at:6m",
		"at:v1.0.0 owner:@jane retry":        "owner: reads today's CODEOWNERS file",
		"at:v1.0.0 (owner:@jane OR f:x) foo": "owner: reads today's CODEOWNERS file",
	}
	for text, want := range tests {
		q := Parse(text, testResolver)
		if count(q.Diagnostics, DiagOpWithAt) != 1 {
			t.Errorf("Parse(%s) = %v, want one op_with_at", text, codes(q.Diagnostics))
			continue
		}
		var d protocol.Diagnostic
		for _, found := range q.Diagnostics {
			if found.Code == DiagOpWithAt {
				d = found
			}
		}
		if d.Severity != protocol.SeverityError || !strings.Contains(d.Message, want) {
			t.Errorf("Parse(%s) says %s %q, want an error saying %q", text, d.Severity, d.Message, want)
		}
		if len(d.Fixes) != 1 || !strings.HasPrefix(d.Fixes[0].Title, "Remove ") {
			t.Errorf("Parse(%s) offers %+v, want to remove the operator", text, d.Fixes)
		}
	}
	// is:test, is:generated and is:vendored are rules about paths, which a
	// revision's files have too.
	for _, text := range []string{"at:v1.0.0 is:test retry", "at:v1.0.0 -is:vendored retry", "at:v1.0.0 is:generated"} {
		mustParseCleanly(t, text)
	}
	// Without at:, they're fine.
	mustParseCleanly(t, "since:30d is:open owner:@jane retry")
}

func TestAtSuggestsTagsThenTimes(t *testing.T) {
	// @covers op:at
	resolver := testResolver
	resolver.tags = []TagStat{
		{Name: "v1.1.0", At: fixedNow.AddDate(0, 0, -12), Repos: []string{"payments-api", "web-checkout"}},
		{Name: "v1.0.0", At: fixedNow.AddDate(0, -7, 0), Repos: []string{"payments-api"}},
		{Name: "v0.9.0", At: time.Date(2025, 11, 2, 9, 0, 0, 0, time.UTC), Repos: []string{"payments-api"}},
	}
	want := strings.Join([]string{
		"[Tags] v1.1.0 | Tag | Made Mon, Sep 21 | payments-api, web-checkout",
		"v1.0.0 | Tag | Made Tue, Mar 3 | payments-api",
		"v0.9.0 | Tag | Made Sun, Nov 2, 2025 | payments-api",
		"[Times] yesterday | End of yesterday | Files as they were at midnight |",
	}, "\n")
	got := shown(Complete("retry at:", len("retry at:"), resolver, fixedNow))
	if !strings.HasPrefix(got, want) {
		t.Errorf("Complete(at:) =\n%s\nwant it to start\n%s", got, want)
	}
	if !strings.Contains(got, "6m | 6 months ago | Files as they were on Fri, Apr 3 |") ||
		!strings.Contains(got, "2h | 2 hours ago | Files as they were at 08:00 |") {
		t.Errorf("Complete(at:) =\n%s\nwant the times with when each is", got)
	}
	// What's typed narrows the tags, anywhere in the name, and the times by their start.
	if got, want := shown(Complete("at:v1.0", len("at:v1.0"), resolver, fixedNow)), "[Tags] v1.0.0 | Tag | Made Tue, Mar 3 | payments-api"; got != want {
		t.Errorf("Complete(at:v1.0) = %q, want %q", got, want)
	}
	if got, want := shown(Complete("at:6", len("at:6"), resolver, fixedNow)), "6min"; !strings.HasPrefix(got, "[Times] "+want) {
		t.Errorf("Complete(at:6) = %q, want the times that start with 6", got)
	}
	// Without tags, the times alone.
	if got := shown(Complete("at:", len("at:"), testResolver, fixedNow)); !strings.HasPrefix(got, "[Times] yesterday") {
		t.Errorf("Complete(at:) without tags = %q", got)
	}
}
