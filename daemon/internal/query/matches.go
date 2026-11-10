package query

import (
	"context"
	"errors"
)

// MaxMatchPositions bounds raw candidate positions in one text scan, including
// empty matches and candidates rejected by word:yes. This keeps the position
// arrays to a few MiB even for a dense, multi-megabyte line. It is independent
// of result pages and of how many highlights a caller wants to display.
const MaxMatchPositions = 32_768

// ErrMatchBudget means a text scan could not establish all of its matches.
// Search may report a lower bound; a replacement must discard the whole plan.
var ErrMatchBudget = errors.New("match position budget exhausted; narrow the search")

// VisitMatches visits term matches in b, including empty matches, until visit
// returns false. It returns ErrMatchBudget if more than maxPositions raw
// candidates are needed, or ctx's cause if cancelled. A nil error means the
// input was exhausted or the caller intentionally stopped visiting. A budget
// above MaxMatchPositions is clamped to it; a nonpositive budget is exhausted.
//
// Assertions always read the original text. RE2 cannot resume an assertion at
// an offset, so bounded, growing prefixes of its match list are requested over
// b itself. Cancellation is checked between those calls and candidate matches;
// a single RE2 call over b cannot be interrupted.
func (c *Content) VisitMatches(ctx context.Context, b []byte, maxPositions int, visit func([]int) bool) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	budget := min(maxPositions, MaxMatchPositions)
	if budget <= 0 {
		return ErrMatchBudget
	}
	if c.Structural != nil {
		return c.visitStructure(ctx, b, budget, visit)
	}
	if c.WholeWord && c.resumable {
		return c.visitWholeWords(ctx, b, budget, visit)
	}
	return c.visitPrefixes(ctx, b, budget, visit)
}

func (c *Content) visitPrefixes(ctx context.Context, b []byte, budget int, visit func([]int) bool) error {
	visited, requested := 0, min(64, budget+1)
	for {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		locs := c.Re.FindAllIndex(b, requested)
		for _, loc := range locs[visited:] {
			if err := context.Cause(ctx); err != nil {
				return err
			}
			if visited == budget {
				return ErrMatchBudget
			}
			visited++
			if (!c.WholeWord || isWholeWord(b, loc[0], loc[1])) && !visit(loc) {
				return nil
			}
		}
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if len(locs) < requested {
			return nil
		}
		requested = min(2*requested, budget+1)
	}
}

// Assertion-free whole-word terms can resume inside a rejected match: a-a
// in ba-a-a still finds the final a-a. Each attempted match spends a position.
func (c *Content) visitWholeWords(ctx context.Context, b []byte, budget int, visit func([]int) bool) error {
	for from, visited := 0, 0; from <= len(b); {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		loc := c.Re.FindIndex(b[from:])
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if loc == nil {
			return nil
		}
		if visited == budget {
			return ErrMatchBudget
		}
		visited++
		loc[0], loc[1] = from+loc[0], from+loc[1]
		if !isWholeWord(b, loc[0], loc[1]) {
			from = loc[0] + runeLen(b, loc[0])
			continue
		}
		if !visit(loc) {
			return nil
		}
		from = loc[1]
		if loc[0] == loc[1] {
			from += runeLen(b, from)
		}
	}
	return nil
}

func (c *Content) visitStructure(ctx context.Context, b []byte, budget int, visit func([]int) bool) error {
	matches, complete := c.Structural.Find(b, "", c.IgnoreCase, budget+1)
	for i, match := range matches {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if i == budget {
			return ErrMatchBudget
		}
		if !visit([]int{match.Start, match.End}) {
			return nil
		}
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if !complete {
		return ErrMatchBudget
	}
	return nil
}
