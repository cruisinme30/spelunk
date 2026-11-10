package trigram

import (
	"context"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// ErrMatchBudget means a replacement needs more positional matches on
// one line than it can safely materialize. The whole plan must fail.
var ErrMatchBudget = query.ErrMatchBudget

// maxReplacementPositions bounds position pairs independently of capture
// groups. One additional match probes whether a larger prefix is needed.
const maxReplacementPositions = query.MaxMatchPositions

// replacementMatches visits each non-empty match kept by in:, stopping
// when the caller has enough edits or the request is cancelled. Prefixes
// always match the original full line, preserving anchors and assertions.
func replacementMatches(ctx context.Context, term *query.Content, line []byte, keep func(int, int) bool, visit func([]int) bool) error {
	return term.VisitMatches(ctx, line, maxReplacementPositions, func(loc []int) bool {
		if loc[1] <= loc[0] || (keep != nil && !keep(loc[0], loc[1])) {
			return true
		}
		return visit(loc)
	})
}
