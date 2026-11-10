package trigram

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// ErrIncomplete means a struct: template gave up on a file, so the files
// a replace would change can't all be known.
var ErrIncomplete = errors.New("a struct: filter gave up on a file, so some matches may be missed; narrow the search to replace")

// Replacements lists the edits that replacing term's matches with
// replacement would make: one per match of term on every line that the
// query's code results show. Unlike a search it never stops part way: it
// returns ctx's error instead, or ErrIncomplete when a struct: template
// gave up on a file, and once the edits pass limit it returns
// only their count, marked truncated, so a caller never mistakes part of a
// replace for all of it. It leaves alone the files VS Code's excludes hide,
// counting the editable matches there as Excluded. A truncated count is a
// lower bound: work stops as soon as limit is exceeded.
func Replacements(ctx context.Context, plan *query.Plan, repos []Repo, term *query.Content, replacement string, limit int) (protocol.ReplacePlan, error) {
	s := newSearcher(ctx, plan, 0, func(protocol.ResultItem) {})
	out := protocol.ReplacePlan{Files: []protocol.ReplaceFile{}}
	visible := withoutExcludes(plan)
	builder := newReplacementBuilder(ctx, term, replacement)
	var buildErr error
	s.forEachCandidate(repos, func(c rankedCandidate, matcher *lineMatcher, leaf func(query.Pred) bool) {
		if out.Truncated {
			return
		}
		if !query.Eval(plan.Pred, leaf) {
			excluded, err := excludedEdits(ctx, visible, leaf, matcher, term)
			out.Excluded += excluded
			if err != nil {
				buildErr = err
				s.truncated = true
			}
			return
		}
		file, matches, err := builder.file(c, matcher, query.Contributing(plan.Pred, leaf), max(1, limit-out.Matches+1))
		if err != nil {
			buildErr = err
			s.truncated = true
			return
		}
		out.Matches += matches
		if out.Matches > limit {
			out.Truncated = true
			s.truncated = true // stops forEachCandidate
		}
		if len(file.Lines) > 0 && !out.Truncated {
			file.SourceHash, file.Cells = replacementSource(c.doc)
			file.OutputHash = replacementOutputs(c.doc)
			out.Files = append(out.Files, file)
		}
	})
	if err := ctx.Err(); err != nil {
		return protocol.ReplacePlan{}, context.Cause(ctx)
	}
	if buildErr != nil {
		return protocol.ReplacePlan{}, buildErr
	}
	if s.matchErr != nil {
		return protocol.ReplacePlan{}, s.matchErr
	}
	if s.incomplete {
		return protocol.ReplacePlan{}, ErrIncomplete
	}
	if out.Truncated {
		out.Files = []protocol.ReplaceFile{}
	}
	return out, nil
}

// replacementBuilder keeps a request's replacement settings together while
// building each file, stopping at the first edit beyond the allowed total.
type replacementBuilder struct {
	ctx         context.Context
	term        *query.Content
	replacement string
	budget      *replacementBudget
	refs        *captureReferences
}

func newReplacementBuilder(ctx context.Context, term *query.Content, replacement string) replacementBuilder {
	builder := replacementBuilder{ctx: ctx, term: term, replacement: replacement, budget: newReplacementBudget()}
	if term.Literal == "" && strings.Contains(replacement, "$") {
		builder.refs = newCaptureReferences(term.Re)
	}
	return builder
}

func (b replacementBuilder) file(c rankedCandidate, matcher *lineMatcher, terms []*query.Content, limit int) (protocol.ReplaceFile, int, error) {
	file := protocol.ReplaceFile{
		RepoID: c.repo.ID, Path: c.doc.Path, File: filepath.Join(c.repo.Root, filepath.FromSlash(c.doc.Path)),
	}
	count := 0
	for _, line := range matcher.lines(terms) {
		if isOutputLine(c.doc.Cells, line.number) {
			continue // running a cell writes its outputs
		}
		edits, err := b.edits(line.text, matcher.keepsOnLine(line.number), limit-count, func() error {
			if count == 0 {
				if err := b.budget.file(&file, c.doc); err != nil {
					return err
				}
			}
			return b.budget.line(line.text)
		})
		if err != nil {
			return protocol.ReplaceFile{}, 0, err
		}
		if len(edits) == 0 {
			continue
		}
		count += len(edits)
		file.Lines = append(file.Lines, protocol.ReplaceLine{Line: line.number, Text: line.text, Edits: edits})
		if count >= limit {
			break
		}
	}
	return file, count, nil
}

// withoutExcludes is plan's predicate without the conjunct that leaves out
// what VS Code's excludes hide, or nil when it has none.
func withoutExcludes(plan *query.Plan) *query.And {
	for _, f := range plan.Filters {
		if f.Reason == "excludes" {
			kids := plan.Pred.Kids
			return &query.And{Kids: append(append([]query.Pred{}, kids[:f.Index]...), kids[f.Index+1:]...)}
		}
	}
	return nil
}

// excludedEdits counts the edits a replace would make in a file that only
// VS Code's excludes hid: one that visible, the plan without them, keeps.
func excludedEdits(ctx context.Context, visible *query.And, leaf func(query.Pred) bool, matcher *lineMatcher, term *query.Content) (int, error) {
	if visible == nil || !query.Eval(visible, leaf) {
		return 0, nil
	}
	count := 0
	for _, line := range matcher.lines(query.Contributing(visible, leaf)) {
		if isOutputLine(matcher.cells, line.number) {
			continue
		}
		if err := replacementMatches(ctx, term, []byte(line.text), matcher.keepsOnLine(line.number), func(_ []int) bool {
			count++
			return true
		}); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// edits replaces non-empty matches on one line, up to limit. Byte
// and capture cursors advance with the ordered matches, so a dense line's
// replacements take linear work instead of rescanning every prefix.
func (b replacementBuilder) edits(text string, keep func(start, end int) bool, limit int, prepare func() error) ([]protocol.ReplaceEdit, error) {
	if len(text) > b.budget.remaining {
		return nil, ErrReplaceBudget
	}
	var line []byte
	var captures *captureExpander
	if b.refs != nil {
		captures = newCaptureExpander(b.term, text, limit)
		line = captures.line
	} else {
		line = []byte(text)
	}
	var edits []protocol.ReplaceEdit
	var err error
	byteAt, units := 0, 0
	matchErr := replacementMatches(b.ctx, b.term, line, keep, func(loc []int) bool {
		if len(edits) == 0 {
			if err = prepare(); err != nil {
				return false
			}
		}
		if err = b.budget.charge(96); err != nil {
			return false
		}
		newText := b.replacement
		if captures != nil {
			newText, err = captures.expand(b.ctx, b.replacement, loc, b.refs, b.budget)
		} else {
			err = b.budget.text(newText)
		}
		if err != nil {
			return false
		}
		start := units + utf16Len(text[byteAt:loc[0]])
		units = start + utf16Len(text[loc[0]:loc[1]])
		byteAt = loc[1]
		edits = append(edits, protocol.ReplaceEdit{Start: start, End: units, NewText: newText})
		return len(edits) < limit
	})
	if err != nil {
		return nil, err
	}
	if matchErr != nil {
		return nil, matchErr
	}
	return edits, nil
}
