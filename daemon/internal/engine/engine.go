package engine

import (
	"context"
	"maps"
	"slices"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// Stats describe one page of a search.
type Stats struct {
	Total      int // results counted, at most query.MaxResults
	Truncated  bool
	NextOffset int // where the next page starts; 0 when this is the last page
	Hidden     []protocol.HiddenNote
	Facets     []protocol.Facet // every counted result by repo, language, folder, author or month
}

// NewStats starts the stats of a plan's search that counted results; the
// engine adds its hidden-results notes.
func NewStats(plan *query.Plan, counted int, truncated bool) Stats {
	stats := Stats{Total: counted, Truncated: truncated, Hidden: []protocol.HiddenNote{}}
	if next := plan.Offset + plan.Limit; next < counted {
		stats.NextOffset = next
	}
	return stats
}

// Hidden is what a search's filters hid: per filter index, results that
// only that filter removed, and what case:yes, word:yes and type: hid.
type Hidden struct {
	ByFilter               map[int]int
	ByCase, ByWord, ByType int
	// ByMatch counts the file names match:fuzzy would add.
	ByMatch int
	// ByIn counts the matches in: hid: in other parts of files.
	ByIn int
}

// AddHidden adds one hidden-results note per filter of plan that hid
// something, counting unit; typeUnit counts what type: hid.
func (s *Stats) AddHidden(plan *query.Plan, hidden Hidden, unit, typeUnit string) {
	for i := range plan.Filters {
		if f := &plan.Filters[i]; hidden.ByFilter[f.Index] > 0 {
			s.Hidden = append(s.Hidden, f.Note(hidden.ByFilter[f.Index], unit))
		}
	}
	for _, by := range []struct {
		filter      *query.Filter
		count       int
		countedUnit string
	}{
		{plan.CaseFilter, hidden.ByCase, unit},
		{plan.WordFilter, hidden.ByWord, unit},
		{plan.MatchFilter, hidden.ByMatch, "files"},
		{plan.InFilter, hidden.ByIn, "matches"},
		{plan.TypeFilter, hidden.ByType, typeUnit},
	} {
		if by.filter != nil && by.count > 0 {
			s.Hidden = append(s.Hidden, by.filter.Note(by.count, by.countedUnit))
		}
	}
}

// AddExcludes names the patterns of VS Code's excludes that hid results
// (or, with excludes:no, matched results that show) on the excludes note,
// and with excludes:no adds the note that counts the shown results, shown,
// whose undo hides them again.
func (s *Stats) AddExcludes(plan *query.Plan, patterns map[string]bool, shown int, unit string) {
	names := slices.Sorted(maps.Keys(patterns))
	for i := range s.Hidden {
		if s.Hidden[i].Reason == "excludes" {
			s.Hidden[i].Patterns = names
		}
	}
	if plan.ShownFilter != nil && shown > 0 {
		note := plan.ShownFilter.Note(shown, unit)
		note.Shown, note.Patterns = true, names
		s.Hidden = append(s.Hidden, note)
	}
}

// SearchFunc is an engine's Search.
type SearchFunc[R any] func(ctx context.Context, plan *query.Plan, repos []R, planID int, emit func(protocol.ResultItem)) (Stats, error)

// The Count functions count the results search finds for a plan relaxed
// in one way, so an engine can say how many results that one filter hid:
// the count, less the results the plan itself found. complete is false
// when the count stopped short, out of time (the search's own budget,
// whatever of it is left) or at query.MaxResults, or failed: the
// difference would then be a guess, so no note is shown.

// CountIgnoringCase counts the results search finds for plan without its
// case:yes (see the Count functions).
func CountIgnoringCase[R any](ctx context.Context, plan *query.Plan, repos []R, search SearchFunc[R]) (count int, complete bool) {
	return countAll(ctx, plan.IgnoringCase(), repos, search)
}

// CountAnywhere counts the results search finds for plan without its in:
// (see the Count functions).
func CountAnywhere[R any](ctx context.Context, plan *query.Plan, repos []R, search SearchFunc[R]) (count int, complete bool) {
	return countAll(ctx, plan.Anywhere(), repos, search)
}

// CountFuzzyNames counts the results search finds for plan with words that
// also find file names by their letters (see the Count functions).
func CountFuzzyNames[R any](ctx context.Context, plan *query.Plan, repos []R, search SearchFunc[R]) (count int, complete bool) {
	return countAll(ctx, plan.MatchingFuzzy(), repos, search)
}

// CountPartialWords counts the results search finds for plan without its
// word:yes (see the Count functions).
func CountPartialWords[R any](ctx context.Context, plan *query.Plan, repos []R, search SearchFunc[R]) (count int, complete bool) {
	return countAll(ctx, plan.MatchingPartialWords(), repos, search)
}

// CountOnEitherSide counts the commits search finds for plan with its text
// terms on both sides of diffs and in messages, as without type:added or
// type:removed (see the Count functions).
func CountOnEitherSide[R any](ctx context.Context, plan *query.Plan, repos []R, search SearchFunc[R]) (count int, complete bool) {
	return countAll(ctx, plan.OnEitherSide(), repos, search)
}

// countAll counts every result search finds for a relaxed copy of a plan.
func countAll[R any](ctx context.Context, relaxed *query.Plan, repos []R, search SearchFunc[R]) (count int, complete bool) {
	relaxed.Offset, relaxed.Limit = 0, 0 // count only
	stats, err := search(ctx, relaxed, repos, 0, func(protocol.ResultItem) {})
	if err != nil || stats.Truncated {
		return 0, false
	}
	return stats.Total, true
}

// HiddenBy is how many results a filter hid: count, from one of the Count
// functions, less counted, what the search found. It is 0 when either
// count is incomplete: the search's own (truncated), or the relaxed one.
func HiddenBy(count int, complete bool, counted int, truncated bool) int {
	if !complete || truncated {
		return 0
	}
	return max(count-counted, 0)
}

// AllIDs returns the ids 0 to n-1: every doc or commit, when narrowing
// can't rule any out.
func AllIDs(n int) []uint32 {
	ids := make([]uint32, n)
	for i := range ids {
		ids[i] = uint32(i) //nolint:gosec // G115: shards and segments hold far fewer than 2^32 ids
	}
	return ids
}
