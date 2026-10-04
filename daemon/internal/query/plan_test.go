package query

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// mustPlan parses and plans text, which must have no diagnostics.
func mustPlan(t *testing.T, text string, settings protocol.Settings) *Plan {
	t.Helper()
	plan, _, err := NewPlan(mustParseCleanly(t, text), settings, fixedNow, "")
	if err != nil {
		t.Fatalf("NewPlan(%q): %v", text, err)
	}
	return plan
}

// kinds lists the result kinds plan returns, comma-separated.
func kinds(plan *Plan) string {
	var list []string
	for _, kind := range []ResultKind{KindFile, KindLine, KindSymbol, KindCommit} {
		if plan.Kinds[kind] {
			list = append(list, kind)
		}
	}
	return strings.Join(list, ",")
}

func TestPlanLowersTheQuery(t *testing.T) {
	tests := []struct{ query, want string }{
		{"retry_policy", "and(content#0:/(?i)retry_policy/)"},
		{"a b OR c", "and(or(and(content#0:/(?i)a/ content#1:/(?i)b/) content#2:/(?i)c/))"},
		{`f:.*test\.py$ timeout`, `and(path:/(?i).*test\.py$/ content#0:/(?i)timeout/)`},
		{`"a.b" x`, `and(content#0:/(?i)a\.b/ content#1:/(?i)x/)`},
		{"case:yes /Retry(Policy|Config)/ lang:py", "and(content#0:/Retry(Policy|Config)/ lang:python)"},
		{"sym:RetryPolicy", "and(sym:/(?i)^RetryPolicy$/)"},
		{"sym:/Retry.*/", "and(sym:/(?i)Retry.*/)"},
		{`author:jane msg:"fix flaky" -f:vendor/`, `and(author:~"jane" msg:/(?i)fix flaky/ not(path:/(?i)vendor//))`},
		{`author:"Jane Doe" x`, `and(author:="jane doe" content#0:/(?i)x/)`},
		{"repo:web count:20 x", "and(repo:/(?i)web/ content#0:/(?i)x/)"},
	}
	for _, tt := range tests {
		if got := mustPlan(t, tt.query, defaultSettings).Pred.String(); got != tt.want {
			t.Errorf("plan(%q) = %s, want %s", tt.query, got, tt.want)
		}
	}
}

func TestSinceWindowsCountBackFromNow(t *testing.T) {
	tests := map[string]string{
		"since:30d x": "2026-09-03T10:00:00Z",
		"since:2w x":  "2026-09-19T10:00:00Z",
		"since:6m x":  "2026-04-03T10:00:00Z",
		"since:1y x":  "2025-10-03T10:00:00Z",
	}
	for query, want := range tests {
		first := mustPlan(t, query, defaultSettings).Pred.Kids[0]
		since, ok := first.(*Since)
		if !ok {
			t.Fatalf("plan(%q) first conjunct = %s, want since:", query, first)
		}
		got := since.After.UTC().Format(time.RFC3339)
		if got != want {
			t.Errorf("plan(%q) since = %s, want %s", query, got, want)
		}
	}
}

func TestCaseSensitivityComesFromCaseOrTheSetting(t *testing.T) {
	sensitive := defaultSettings
	sensitive.CaseSensitive = true
	tests := []struct {
		query    string
		settings protocol.Settings
		want     bool
	}{
		{"x", defaultSettings, false},
		{"x", sensitive, true},
		{"case:yes x", defaultSettings, true},
		{"case:no x", sensitive, false},
	}
	for _, tt := range tests {
		if got := mustPlan(t, tt.query, tt.settings).CaseSensitive; got != tt.want {
			t.Errorf("plan(%q, setting %v).CaseSensitive = %v, want %v", tt.query, tt.settings.CaseSensitive, got, tt.want)
		}
	}
}

func TestResultKinds(t *testing.T) {
	tests := map[string]string{
		"retry":                       "file,line",
		"type:file lang:python retry": "file",
		"type:code retry":             "line",
		"sym:RetryPolicy":             "symbol",
		"type:code sym:Retry":         "symbol",
		"author:jane timeout":         "commit",
		"type:commit timeout":         "commit",
	}
	for query, want := range tests {
		if got := kinds(mustPlan(t, query, defaultSettings)); got != want {
			t.Errorf("kinds(%q) = %s, want %s", query, got, want)
		}
	}
}

func TestLimitAndPaging(t *testing.T) {
	if got := mustPlan(t, "x", defaultSettings).Limit; got != 500 {
		t.Errorf("default limit = %d, want 500", got)
	}
	if got := mustPlan(t, "count:20 x", defaultSettings).Limit; got != 20 {
		t.Errorf("count:20 limit = %d, want 20", got)
	}
	if got := mustPlan(t, "count:all x", defaultSettings).Limit; got != MaxResults {
		t.Errorf("count:all limit = %d, want %d", got, MaxResults)
	}
	plan, _, err := NewPlan(mustParseCleanly(t, "x"), defaultSettings, fixedNow, "40")
	if err != nil {
		t.Fatalf("cursor 40: %v", err)
	}
	if plan.Offset != 40 {
		t.Errorf("cursor 40: offset %d, want 40", plan.Offset)
	}
	if _, _, err := NewPlan(mustParseCleanly(t, "x"), defaultSettings, fixedNow, "-3"); err == nil {
		t.Error("cursor -3: want an error")
	}
}

func TestFiltersThatCanHideResultsCarryAnUndo(t *testing.T) {
	text := "author:jane (timeout OR retry) -f:vendor/ since:6m -flaky"
	plan := mustPlan(t, text, defaultSettings)
	var got []string
	for _, f := range plan.Filters {
		got = append(got, f.Reason+"@"+strconv.Itoa(f.Index)+":"+f.Text+"→"+applyFix(text, f.Undo))
	}
	want := []string{
		"pathFilter@2:-f:vendor/→author:jane (timeout OR retry) since:6m -flaky",
		"since@3:since:6m→author:jane (timeout OR retry) -f:vendor/ -flaky",
		"not@4:-flaky→author:jane (timeout OR retry) -f:vendor/ since:6m",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("filters =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTypeIsAKindFilterWithAnUndo(t *testing.T) {
	text := "type:file lang:python retry"
	plan := mustPlan(t, text, defaultSettings)
	if plan.KindFilter == nil || plan.KindFilter.Text != "type:file" || applyFix(text, plan.KindFilter.Undo) != "lang:python retry" {
		t.Errorf("KindFilter = %+v, want type:file with an undo giving %q", plan.KindFilter, "lang:python retry")
	}
	if mustPlan(t, "retry", defaultSettings).KindFilter != nil {
		t.Error("KindFilter of a query without type: = non-nil, want nil")
	}
}

func TestCaseYesIsACaseFilterThatCanBeIgnored(t *testing.T) {
	text := "case:yes /Retry(Policy|Config)/ lang:python"
	plan := mustPlan(t, text, defaultSettings)
	f := plan.CaseFilter
	if f == nil || f.Reason != "case" || f.Undo.Title != "Ignore case" || applyFix(text, f.Undo) != "/Retry(Policy|Config)/ lang:python" {
		t.Errorf("CaseFilter = %+v, want case:yes with an Ignore case undo", f)
	}
	if mustPlan(t, "case:no retry", defaultSettings).CaseFilter != nil {
		t.Error("CaseFilter of case:no = non-nil, want nil")
	}
	sensitive := defaultSettings
	sensitive.CaseSensitive = true
	if mustPlan(t, "Retry", sensitive).CaseFilter != nil {
		t.Error("CaseFilter from the caseSensitive setting = non-nil, want nil (nothing in the query to undo)")
	}
}

func TestPositivePathIsTheScopeNotAFilter(t *testing.T) {
	if filters := mustPlan(t, `f:.*test\.py$ timeout`, defaultSettings).Filters; len(filters) != 0 {
		t.Errorf("Filters of f:… timeout = %+v, want none", filters)
	}
}

func TestIgnoringCaseFoldsEveryRegex(t *testing.T) {
	plan := mustPlan(t, "case:yes Retry f:Src/ -Draft", defaultSettings)
	folded := plan.IgnoringCase()
	if got, want := folded.Pred.String(), "and(content#0:/(?i)Retry/ path:/(?i)Src// not(content#1:/(?i)Draft/))"; got != want {
		t.Errorf("IgnoringCase().Pred = %s, want %s", got, want)
	}
	if folded.CaseSensitive || folded.CaseFilter != nil || folded.Filters != nil {
		t.Errorf("folded plan = %+v, want case ignored and no filters", folded)
	}
	if len(folded.Terms) != 2 || folded.Terms[0] != folded.Pred.Kids[0] {
		t.Errorf("folded Terms = %v, want the folded content predicates", folded.Terms)
	}
	if got, want := plan.Pred.String(), "and(content#0:/Retry/ path:/Src// not(content#1:/Draft/))"; got != want {
		t.Errorf("original Pred after IgnoringCase = %s, want it unchanged: %s", got, want)
	}
}

func TestEvalAndContributingTerms(t *testing.T) {
	plan := mustPlan(t, "(a OR b) c -d", defaultSettings)
	present := map[string]bool{"a": true, "c": true}
	leaf := func(p Pred) bool {
		c, ok := p.(*Content)
		return ok && present[c.Literal]
	}
	if !Eval(plan.Pred, leaf) {
		t.Fatal("Eval((a OR b) c -d) with a and c present = false, want true")
	}
	var terms []string
	for _, c := range Contributing(plan.Pred, leaf) {
		terms = append(terms, c.Literal)
	}
	if got := strings.Join(terms, ","); got != "a,c" {
		t.Errorf("contributing terms = %s, want a,c (b is absent, d is negated)", got)
	}
	present["d"] = true
	if Eval(plan.Pred, leaf) {
		t.Error("Eval with d present = true, want false")
	}
}

func TestHistoryRegexWithoutALiteralWarns(t *testing.T) {
	// @covers diag:history_full_scan
	warn := func(query string) bool {
		_, warnings, err := NewPlan(mustParseCleanly(t, query), defaultSettings, fixedNow, "")
		if err != nil {
			t.Fatal(err)
		}
		return len(warnings) == 1 && warnings[0].Code == DiagHistoryFullScan && warnings[0].Severity == protocol.SeverityWarning
	}
	if !warn("type:commit /a.b/") {
		t.Error("type:commit /a.b/: want a history_full_scan warning")
	}
	for _, quiet := range []string{"type:commit /timeout.*/", "author:jane /a.b/", "since:2w type:commit /a.b/", "/a.b/"} {
		if warn(quiet) {
			t.Errorf("%s: want no warning", quiet)
		}
	}
}

func TestRequiredLiteral(t *testing.T) {
	tests := map[string]string{
		"retry_policy":       "retry_policy",
		"(?i)RetryPolicy":    "retrypolicy",
		"Retry(Policy|Conf)": "Retry",
		"a.b":                "a",
		"(foo|bar)":          "",
		`timeout\s+\d+`:      "timeout",
	}
	for pattern, want := range tests {
		if got := RequiredLiteral(regexp.MustCompile(pattern)); got != want {
			t.Errorf("RequiredLiteral(%q) = %q, want %q", pattern, got, want)
		}
	}
}

func TestPlanRefusesQueriesWithErrors(t *testing.T) {
	if _, _, err := NewPlan(Parse("sinse:6m", nil), defaultSettings, fixedNow, ""); err == nil {
		t.Error("NewPlan of a query with errors: want an error")
	}
}
