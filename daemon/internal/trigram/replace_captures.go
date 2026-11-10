package trigram

import (
	"context"
	"errors"
	"regexp"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// ErrCaptureBudget means a replacement needs more capture indexes than
// can safely be materialized. The whole plan fails, never a partial edit.
var ErrCaptureBudget = errors.New("replacement capture budget exhausted; narrow the search")

// maxReplacementCaptureIndexes bounds a line's capture matrix to 2 MiB
// of indexes on a 64-bit host. A growing prefix briefly retains its prior
// smaller matrix too. Small edit limits allocate only the prefix needed.
const maxReplacementCaptureIndexes = 1 << 18

// captureExpander retains a bounded prefix of the regex's original-line
// capture matches. Matching the whole line preserves anchors, word
// boundaries and alternation, which rematching a clipped match would not.
type captureExpander struct {
	re                         *regexp.Regexp
	line                       []byte
	source                     string
	groups                     [][]int
	requested, maximum, cursor int
	complete                   bool
	wholeWord                  bool
}

func newCaptureExpander(term *query.Content, text string, limit int) *captureExpander {
	maximum := maxReplacementCaptureIndexes / (2 * (term.Re.NumSubexp() + 1))
	return &captureExpander{re: term.Re, line: []byte(text), source: text, requested: min(max(1, limit), maximum), maximum: maximum, wholeWord: term.WholeWord}
}

func (e *captureExpander) expand(ctx context.Context, template string, loc []int, refs *captureReferences, budget *replacementBudget) (string, error) {
	if e.re.NumSubexp() == 0 {
		return expandReplacement(ctx, refs, template, e.source, loc, budget)
	}
	if err := e.through(ctx, loc[0]); err != nil {
		return "", err
	}
	for e.cursor < len(e.groups) && e.groups[e.cursor][0] < loc[0] {
		e.cursor++
	}
	if e.cursor < len(e.groups) {
		g := e.groups[e.cursor]
		if g[0] == loc[0] && g[1] == loc[1] {
			return expandReplacement(ctx, refs, template, e.source, g, budget)
		}
	}
	if !e.wholeWord {
		return "", ErrCaptureBudget
	}
	// Whole-word matching alone can restart inside a rejected match. It
	// only does so for regexes without assertions (query/word.go), so
	// matching its suffix preserves context; never clip off the right end.
	suffix := e.line[loc[0]:]
	if g := e.re.FindSubmatchIndex(suffix); len(g) >= 2 && g[0] == 0 && g[1] == loc[1]-loc[0] {
		return expandReplacement(ctx, refs, template, e.source[loc[0]:], g, budget)
	}
	return "", ErrCaptureBudget // no verified capture map: never guess an edit
}

// through grows the original-line prefix only until it covers offset.
// Every allocation has a finite match count and index budget.
func (e *captureExpander) through(ctx context.Context, offset int) error {
	for !e.complete && (len(e.groups) == 0 || e.groups[len(e.groups)-1][1] <= offset) {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if e.requested == 0 || len(e.groups) == e.maximum {
			return ErrCaptureBudget
		}
		if len(e.groups) > 0 {
			e.requested = min(e.maximum, 2*e.requested)
		}
		e.groups = e.re.FindAllSubmatchIndex(e.line, e.requested)
		e.complete = len(e.groups) < e.requested
	}
	return nil
}
