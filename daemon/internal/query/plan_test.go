package query

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
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
		{"sym:RetryPolicy", "and(sym:/(?i)RetryPolicy/)"},
		{"sym:/Retry.*/", "and(sym:/(?i)Retry.*/)"},
		{"sym:Retry kind:class", "and(sym:/(?i)Retry/ kind:class)"},
		{"k:method -f:test", "and(kind:method not(path:/(?i)test/))"},
		{"timeout ref:RetryPolicy", "and(content#0:/(?i)timeout/ ref#1:/(?i)RetryPolicy/)"},
		{`author:jane msg:"fix flaky" -f:vendor/`, `and(author:~"jane" msg:/(?i)fix flaky/ not(path:/(?i)vendor//))`},
		{`author:"Jane Doe" x`, `and(author:="jane doe" content#0:/(?i)x/)`},
		{"repo:web count:20 x", "and(repo:/(?i)web/ content#0:/(?i)x/)"},
		{"since:2026-09 until:2026-09 x", "and(since:2026-09-01T00:00:00Z not(since:2026-10-01T00:00:00Z) content#0:/(?i)x/)"},
	}
	for _, tt := range tests {
		if got := mustPlan(t, tt.query, defaultSettings).Pred.String(); got != tt.want {
			t.Errorf("plan(%q) = %s, want %s", tt.query, got, tt.want)
		}
	}
}

func TestKindReturnsDefinitionsAndRefReturnsCodeLines(t *testing.T) {
	// @covers op:kind op:ref
	tests := map[string]string{
		"kind:function":            "symbol",
		"sym:Retry k:class":        "symbol",
		"ref:RetryPolicy":          "line",
		"x:RetryPolicy f:payments": "line",
		"ref:RetryPolicy timeout":  "line",
	}
	for query, want := range tests {
		if got := kinds(mustPlan(t, query, defaultSettings)); got != want {
			t.Errorf("plan(%q) returns %s, want %s", query, got, want)
		}
	}
}

func TestKindIsAFilterThatCountsWhatItHid(t *testing.T) {
	plan := mustPlan(t, "sym:Retry kind:class", defaultSettings)
	if len(plan.Filters) != 1 || plan.Filters[0].Reason != "kind" || plan.Filters[0].Index != 1 || plan.Filters[0].Undo.Title != "Remove kind:class" {
		t.Errorf("filters = %+v, want kind:class, undone by removing it", plan.Filters)
	}
}

func TestRefMatchesWholeWordsWithoutWord(t *testing.T) {
	plan := mustPlan(t, "retry ref:RetryPolicy", defaultSettings)
	ref := plan.Terms[1]
	if !ref.Reference || !ref.WholeWord || plan.Terms[0].WholeWord {
		t.Fatalf("terms = %s, want only the ref: term whole-word", plan.Pred)
	}
	if got := ref.FindStringIndex("p = RetryPolicyConfig(RetryPolicy())"); len(got) != 2 || got[0] != len("p = RetryPolicyConfig(") {
		t.Errorf("ref:RetryPolicy matched %v, want only the whole word", got)
	}
	if !ref.IsDefinitionOf("retrypolicy") || ref.IsDefinitionOf("RetryPolicyConfig") {
		t.Error("IsDefinitionOf should match the whole name, ignoring case like the term")
	}
	// Counting what word:yes hid relaxes text terms, but ref: stays whole words.
	if partial := mustPlan(t, "word:yes retry ref:RetryPolicy", defaultSettings).MatchingPartialWords(); partial.Terms[0].WholeWord || !partial.Terms[1].WholeWord {
		t.Errorf("relaxed terms = %s, want retry relaxed and ref: still whole words", partial.Pred)
	}
}

func TestRefWithACapitalTurnsOnSmartCase(t *testing.T) {
	smart := defaultSettings
	smart.CaseSensitive = protocol.CaseSettingSmart
	if !mustPlan(t, "ref:RetryPolicy", smart).CaseSensitive || mustPlan(t, "ref:retry", smart).CaseSensitive {
		t.Error("smart case should match case for ref:RetryPolicy and ignore it for ref:retry")
	}
}

func TestSinceWindowsCountBackFromNow(t *testing.T) {
	tests := map[string]string{
		"since:30d x":       "2026-09-03T10:00:00Z",
		"since:2w x":        "2026-09-19T10:00:00Z",
		"since:6m x":        "2026-04-03T10:00:00Z",
		"since:1y x":        "2025-10-03T10:00:00Z",
		"since:90min x":     "2026-10-03T08:30:00Z",
		"since:2h x":        "2026-10-03T08:00:00Z",
		"since:today x":     "2026-10-03T00:00:00Z",
		"since:Yesterday x": "2026-10-02T00:00:00Z",
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

// pacific is a time zone west of UTC, so a local midnight differs from UTC's.
var pacific = time.FixedZone("PDT", -7*60*60)

// windowBound plans text with now in pacific and returns the time its
// first conjunct's since: (or the since: inside until:'s not) compares with.
func windowBound(t *testing.T, text string) time.Time {
	t.Helper()
	plan, _, err := NewPlan(mustParseCleanly(t, text), defaultSettings, fixedNow.In(pacific), "")
	if err != nil {
		t.Fatalf("NewPlan(%q): %v", text, err)
	}
	pred := plan.Pred.Kids[0]
	if not, ok := pred.(*Not); ok {
		pred = not.Kid
	}
	since, ok := pred.(*Since)
	if !ok {
		t.Fatalf("plan(%q) first conjunct = %s, want since: or until:", text, plan.Pred.Kids[0])
	}
	return since.After
}

func TestSinceDatesStartAtLocalMidnight(t *testing.T) {
	// @covers op:since
	// Now is 2026-10-03 03:00 in pacific (10:00 UTC).
	tests := []struct{ query, want string }{
		{"since:2026-09-30 x", "2026-09-30T00:00:00-07:00"},
		{"since:2026-09 x", "2026-09-01T00:00:00-07:00"},
		{"since:2024-02-29 x", "2024-02-29T00:00:00-07:00"},
		{"since:2026-10-03 x", "2026-10-03T00:00:00-07:00"},
		{"since:2027-01 x", "2027-01-01T00:00:00-07:00"},
		{"since:today x", "2026-10-03T00:00:00-07:00"},
		{"since:yesterday x", "2026-10-02T00:00:00-07:00"},
		{"since:2h x", "2026-10-03T01:00:00-07:00"},
	}
	for _, tt := range tests {
		if got := windowBound(t, tt.query).In(pacific).Format(time.RFC3339); got != tt.want {
			t.Errorf("plan(%q) since = %s, want %s", tt.query, got, tt.want)
		}
	}
}

func TestUntilKeepsChangesBeforeTheEndOfThePeriodItNames(t *testing.T) {
	// @covers op:until
	// Now is 2026-10-03 03:00 in pacific (10:00 UTC).
	tests := []struct{ query, want string }{
		{"until:2026-09 x", "2026-10-01T00:00:00-07:00"},
		{"until:2026-09-30 x", "2026-10-01T00:00:00-07:00"},
		{"until:2026-12 x", "2027-01-01T00:00:00-07:00"},
		{"until:2026-12-31 x", "2027-01-01T00:00:00-07:00"},
		{"until:2024-02 x", "2024-03-01T00:00:00-07:00"},
		{"until:2024-02-29 x", "2024-03-01T00:00:00-07:00"},
		{"until:yesterday x", "2026-10-03T00:00:00-07:00"},
		{"until:today x", "2026-10-04T00:00:00-07:00"},
		{"until:Today x", "2026-10-04T00:00:00-07:00"},
		{"until:2w x", "2026-09-19T03:00:00-07:00"},
		{"until:30min x", "2026-10-03T02:30:00-07:00"},
		{"until:6m x", "2026-04-03T03:00:00-07:00"},
	}
	for _, tt := range tests {
		if _, negated := mustPlan(t, tt.query, defaultSettings).Pred.Kids[0].(*Not); !negated {
			t.Errorf("plan(%q) = %s, want until: lowered to not(since:)", tt.query, mustPlan(t, tt.query, defaultSettings).Pred)
		}
		if got := windowBound(t, tt.query).In(pacific).Format(time.RFC3339); got != tt.want {
			t.Errorf("plan(%q) until = %s, want %s", tt.query, got, tt.want)
		}
	}
	// A length of time ends at the instant since: starts.
	if end, start := windowBound(t, "until:2w x"), windowBound(t, "since:2w x"); !end.Equal(start) {
		t.Errorf("until:2w ends %s, since:2w starts %s, want the same instant", end, start)
	}
}

func TestUntilBeforeSinceWarnsThatNothingCanMatch(t *testing.T) {
	// @covers op:until diag:bad_value
	warnings := func(query string) []protocol.Diagnostic {
		_, warnings, err := NewPlan(mustParseCleanly(t, query), defaultSettings, fixedNow.In(pacific), "")
		if err != nil {
			t.Fatal(err)
		}
		return warnings
	}
	tests := map[string]string{
		"since:2026-09 until:2026-08 x": "until:2026-08 ends before since:2026-09 starts, so nothing can match both",
		"until:1y d:1w x":               "until:1y ends before d:1w starts, so nothing can match both",
		"since:2w until:2w x":           "until:2w ends before since:2w starts, so nothing can match both",
		"since:today until:yesterday x": "until:yesterday ends before since:today starts, so nothing can match both",
	}
	for query, want := range tests {
		got := warnings(query)
		if len(got) != 1 || got[0].Code != DiagBadValue || got[0].Severity != protocol.SeverityWarning || got[0].Message != want {
			t.Errorf("NewPlan(%q) warnings = %+v, want a bad_value warning %q", query, got, want)
			continue
		}
		until := strings.Fields(want)[0]
		if span := got[0].Span; query[span.Start:span.End] != until {
			t.Errorf("NewPlan(%q) warning points at %q, want %q", query, query[span.Start:span.End], until)
		}
	}
	for _, quiet := range []string{"since:2026-09 until:2026-09 x", "since:2w until:1w x", "since:today until:today x", "(since:2026-09 OR x) until:2026-08", "until:2026-08 x"} {
		if got := warnings(quiet); len(got) != 0 {
			t.Errorf("NewPlan(%q) warnings = %+v, want none", quiet, got)
		}
	}
}

func TestCaseSensitivityComesFromCaseOrTheSetting(t *testing.T) {
	sensitive, smart := defaultSettings, defaultSettings
	sensitive.CaseSensitive = protocol.CaseSettingOn
	smart.CaseSensitive = protocol.CaseSettingSmart
	tests := []struct {
		query    string
		settings protocol.Settings
		want     bool
	}{
		{"x", defaultSettings, false},
		{"x", sensitive, true},
		{"case:yes x", defaultSettings, true},
		{"case:no x", sensitive, false},
		{"retry", smart, false},
		{"RetryPolicy", smart, true},
		{"case:smart retry", sensitive, false},
		{"case:smart Retry", defaultSettings, true},
		{"case:no Retry", smart, false},
		{"case:yes retry", smart, true},
	}
	for _, tt := range tests {
		if got := mustPlan(t, tt.query, tt.settings).CaseSensitive; got != tt.want {
			t.Errorf("plan(%q, setting %v).CaseSensitive = %v, want %v", tt.query, tt.settings.CaseSensitive, got, tt.want)
		}
	}
}

func TestWholeWordComesFromWordOrTheSetting(t *testing.T) {
	// @covers setting:wholeWord
	wholeWords := defaultSettings
	wholeWords.WholeWord = true
	tests := []struct {
		query    string
		settings protocol.Settings
		want     bool
	}{
		{"x", defaultSettings, false},
		{"x", wholeWords, true},
		{"word:yes x", defaultSettings, true},
		{"w:no x", wholeWords, false},
	}
	for _, tt := range tests {
		plan := mustPlan(t, tt.query, tt.settings)
		if plan.WholeWord != tt.want || plan.Terms[0].WholeWord != tt.want {
			t.Errorf("plan(%q, setting %v): WholeWord %v, term %v; want %v", tt.query, tt.settings.WholeWord, plan.WholeWord, plan.Terms[0].WholeWord, tt.want)
		}
	}
}

func TestOrderComesFromOrderOrTheSetting(t *testing.T) {
	byPath := defaultSettings
	byPath.Order = protocol.ResultOrderPath
	tests := []struct {
		query    string
		settings protocol.Settings
		want     protocol.ResultOrder
	}{
		{"x", defaultSettings, protocol.ResultOrderBest},
		{"x", byPath, protocol.ResultOrderPath},
		{"order:path x", defaultSettings, protocol.ResultOrderPath},
		{"o:best x", byPath, protocol.ResultOrderBest},
	}
	for _, tt := range tests {
		if got := mustPlan(t, tt.query, tt.settings).Order; got != tt.want {
			t.Errorf("plan(%q, setting %q).Order = %q, want %q", tt.query, tt.settings.Order, got, tt.want)
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
		"type:added timeout":          "commit",
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
	text := "author:jane (timeout OR retry) -f:vendor/ since:6m -flaky until:1w"
	plan := mustPlan(t, text, defaultSettings)
	var got []string
	for _, f := range plan.Filters {
		got = append(got, f.Reason+"@"+strconv.Itoa(f.Index)+":"+f.Text+"→"+ApplyFix(text, f.Undo))
	}
	want := []string{
		"pathFilter@2:-f:vendor/→author:jane (timeout OR retry) since:6m -flaky until:1w",
		"since@3:since:6m→author:jane (timeout OR retry) -f:vendor/ -flaky until:1w",
		"not@4:-flaky→author:jane (timeout OR retry) -f:vendor/ since:6m until:1w",
		"since@5:until:1w→author:jane (timeout OR retry) -f:vendor/ since:6m -flaky",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("filters =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTypeIsAKindFilterWithAnUndo(t *testing.T) {
	text := "type:file lang:python retry"
	plan := mustPlan(t, text, defaultSettings)
	if plan.KindFilter == nil || plan.KindFilter.Text != "type:file" || ApplyFix(text, plan.KindFilter.Undo) != "lang:python retry" {
		t.Errorf("KindFilter = %+v, want type:file with an undo giving %q", plan.KindFilter, "lang:python retry")
	}
	if mustPlan(t, "retry", defaultSettings).KindFilter != nil {
		t.Error("KindFilter of a query without type: = non-nil, want nil")
	}
}

func TestTypeAddedOrRemovedPicksOneSideOfTheDiff(t *testing.T) {
	// @covers op:type
	tests := map[string]string{"type:added retry": TypeAdded, "t:removed retry": TypeRemoved, "type:commit retry": "", "author:jane": ""}
	for text, want := range tests {
		if got := mustPlan(t, text, defaultSettings).DiffSide; got != want {
			t.Errorf("plan(%q).DiffSide = %q, want %q", text, got, want)
		}
	}
	// Removing type:added could leave a query of current files, so its undo
	// searches every changed line instead.
	text := "type:added retry -f:vendor/"
	f := mustPlan(t, text, defaultSettings).KindFilter
	if f == nil || f.Text != "type:added" || ApplyFix(text, f.Undo) != "type:commit retry -f:vendor/" {
		t.Errorf("KindFilter = %+v, want type:added with an undo giving %q", f, "type:commit retry -f:vendor/")
	}
	either := mustPlan(t, text, defaultSettings).OnEitherSide()
	if either.DiffSide != "" || either.KindFilter != nil || len(either.Filters) != 0 {
		t.Errorf("OnEitherSide() = side %q, kind filter %v, %d filters; want both sides and no filters", either.DiffSide, either.KindFilter, len(either.Filters))
	}
}

func TestCaseYesIsACaseFilterThatCanBeIgnored(t *testing.T) {
	text := "case:yes /Retry(Policy|Config)/ lang:python"
	plan := mustPlan(t, text, defaultSettings)
	f := plan.CaseFilter
	if f == nil || f.Reason != "case" || f.Undo.Title != "Ignore case" || ApplyFix(text, f.Undo) != "/Retry(Policy|Config)/ lang:python" {
		t.Errorf("CaseFilter = %+v, want case:yes with an Ignore case undo", f)
	}
	if mustPlan(t, "case:no retry", defaultSettings).CaseFilter != nil {
		t.Error("CaseFilter of case:no = non-nil, want nil")
	}
	sensitive := defaultSettings
	sensitive.CaseSensitive = protocol.CaseSettingOn
	if mustPlan(t, "Retry", sensitive).CaseFilter != nil {
		t.Error("CaseFilter from the caseSensitive setting = non-nil, want nil (nothing in the query to undo)")
	}
}

func TestSmartCaseThatMatchesCaseCanBeUndone(t *testing.T) {
	smart := defaultSettings
	smart.CaseSensitive = protocol.CaseSettingSmart
	tests := []struct {
		query    string
		settings protocol.Settings
		undone   string // the query after the undo; "" when there is no filter
	}{
		{"case:smart Retry lang:python", defaultSettings, "case:no Retry lang:python"},
		{"Retry lang:python", smart, "case:no Retry lang:python"},
		{"case:smart retry", defaultSettings, ""},
		{"retry", smart, ""},
	}
	for _, tt := range tests {
		f := mustPlan(t, tt.query, tt.settings).CaseFilter
		switch {
		case tt.undone == "" && f != nil:
			t.Errorf("plan(%q).CaseFilter = %+v, want nil: smart case ignores case here", tt.query, f)
		case tt.undone != "" && (f == nil || f.Text != "case:smart" || f.Undo.Title != "Ignore case" || ApplyFix(tt.query, f.Undo) != tt.undone):
			t.Errorf("plan(%q).CaseFilter = %+v, want case:smart with an Ignore case undo to %q", tt.query, f, tt.undone)
		}
	}
}

func TestWordYesIsAWordFilterThatCanBeUndone(t *testing.T) {
	text := "retry word:yes lang:python"
	f := mustPlan(t, text, defaultSettings).WordFilter
	if f == nil || f.Reason != "word" || f.Undo.Title != "Match parts of words" || ApplyFix(text, f.Undo) != "retry lang:python" {
		t.Errorf("WordFilter = %+v, want word:yes with a Match parts of words undo", f)
	}
	if mustPlan(t, "word:no retry", defaultSettings).WordFilter != nil {
		t.Error("WordFilter of word:no = non-nil, want nil")
	}
	wholeWords := defaultSettings
	wholeWords.WholeWord = true
	if mustPlan(t, "retry", wholeWords).WordFilter != nil {
		t.Error("WordFilter from the wholeWord setting = non-nil, want nil (nothing in the query to undo)")
	}
}

func TestMatchingPartialWordsDropsWholeWordOnly(t *testing.T) {
	plan := mustPlan(t, "w:yes case:yes Retry f:Src/ -Draft", defaultSettings)
	partial := plan.MatchingPartialWords()
	if got, want := partial.Pred.String(), "and(content#0:/Retry/ path:/Src// not(content#1:/Draft/))"; got != want {
		t.Errorf("MatchingPartialWords().Pred = %s, want %s", got, want)
	}
	if partial.WholeWord || partial.WordFilter != nil || partial.CaseFilter != nil || !partial.CaseSensitive {
		t.Errorf("partial plan = %+v, want parts of words, case still matched, and no filters", partial)
	}
	if len(partial.Terms) != 2 || partial.Terms[0] != partial.Pred.Kids[0] {
		t.Errorf("partial Terms = %v, want the copied content predicates", partial.Terms)
	}
	if got, want := plan.Pred.String(), "and(content#0:word/Retry/ path:/Src// not(content#1:word/Draft/))"; got != want {
		t.Errorf("original Pred after MatchingPartialWords = %s, want it unchanged: %s", got, want)
	}
	if got, want := plan.IgnoringCase().Pred.String(), "and(content#0:word/(?i)Retry/ path:/(?i)Src// not(content#1:word/(?i)Draft/))"; got != want {
		t.Errorf("IgnoringCase().Pred = %s, want whole words kept: %s", got, want)
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
	for _, quiet := range []string{"type:commit /timeout.*/", "author:jane /a.b/", "since:2w type:commit /a.b/", "until:2w type:commit /a.b/", "/a.b/"} {
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

func TestSymbolQueriesOfferToSearchTheNameAsText(t *testing.T) {
	for _, tt := range []struct{ query, filter, title, text string }{
		{"sym:RetryPolicy", "sym:RetryPolicy", "Search RetryPolicy as text", "RetryPolicy"},
		{"case:yes sym:/Retry.*/ f:src/", "sym:/Retry.*/", "Search /Retry.*/ as text", "case:yes /Retry.*/ f:src/"},
	} {
		plan := mustPlan(t, tt.query, defaultSettings)
		f := plan.SymbolFilter
		if f == nil || f.Reason != "symbol" || f.Text != tt.filter || f.Undo.Title != tt.title {
			t.Errorf("%s: symbol filter = %+v, want %s with %q", tt.query, f, tt.filter, tt.title)
			continue
		}
		if got := ApplyFix(tt.query, f.Undo); got != tt.text {
			t.Errorf("%s: undo gives %q, want %q", tt.query, got, tt.text)
		}
	}
	if plan := mustPlan(t, "retry", defaultSettings); plan.SymbolFilter != nil {
		t.Errorf("retry: symbol filter = %+v, want none", plan.SymbolFilter)
	}
}
