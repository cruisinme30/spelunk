package engine

import (
	"context"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// Budget is how long one search may run before it returns what it has,
// marked truncated.
const Budget = 2 * time.Second

// Stats describe one page of a search.
type Stats struct {
	Total      int // results counted, at most query.MaxResults
	Truncated  bool
	NextOffset int // where the next page starts; 0 when this is the last page
	Hidden     []protocol.HiddenNote
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

// SearchFunc is an engine's Search.
type SearchFunc[R any] func(ctx context.Context, plan *query.Plan, repos []R, planID int, emit func(protocol.ResultItem)) (Stats, error)

// CountIgnoringCase counts the results search finds for plan without its
// case:yes, or 0 if that search fails.
func CountIgnoringCase[R any](ctx context.Context, plan *query.Plan, repos []R, search SearchFunc[R]) int {
	folded := plan.IgnoringCase()
	folded.Offset, folded.Limit = 0, 0 // count only
	stats, err := search(ctx, folded, repos, 0, func(protocol.ResultItem) {})
	if err != nil {
		return 0
	}
	return stats.Total
}
