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
// counting the matches there as Excluded.
func Replacements(ctx context.Context, plan *query.Plan, repos []Repo, term *query.Content, replacement string, limit int) (protocol.ReplacePlan, error) {
	s := newSearcher(ctx, plan, 0, func(protocol.ResultItem) {})
	out := protocol.ReplacePlan{Files: []protocol.ReplaceFile{}}
	visible := withoutExcludes(plan)
	s.forEachCandidate(repos, func(c rankedCandidate, matcher *lineMatcher, leaf func(query.Pred) bool) {
		if out.Truncated {
			return
		}
		if !query.Eval(plan.Pred, leaf) {
			out.Excluded += excludedEdits(visible, leaf, matcher, term, replacement)
			return
		}
		file := protocol.ReplaceFile{
			RepoID: c.repo.ID, Path: c.doc.Path, File: filepath.Join(c.repo.Root, filepath.FromSlash(c.doc.Path)),
		}
		for _, line := range matcher.lines(query.Contributing(plan.Pred, leaf)) {
			if isOutputLine(c.doc.Cells, line.number) {
				continue // what a notebook cell printed; running the cell writes it
			}
			text := line.text // decodeText already dropped any byte order mark
			edits := replaceEdits(term, text, replacement)
			if len(edits) == 0 {
				continue // another term's line, or only empty matches
			}
			out.Matches += len(edits)
			file.Lines = append(file.Lines, protocol.ReplaceLine{Line: line.number, Text: text, Edits: edits})
		}
		if out.Matches > limit {
			out.Truncated = true
			s.truncated = true // stops forEachCandidate
		}
		if len(file.Lines) > 0 {
			out.Files = append(out.Files, file)
		}
	})
	if err := ctx.Err(); err != nil {
		return protocol.ReplacePlan{}, context.Cause(ctx)
	}
	if s.incomplete {
		return protocol.ReplacePlan{}, ErrIncomplete
	}
	if out.Truncated {
		out.Files = []protocol.ReplaceFile{}
	}
	return out, nil
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
func excludedEdits(visible *query.And, leaf func(query.Pred) bool, matcher *lineMatcher, term *query.Content, replacement string) int {
	if visible == nil || !query.Eval(visible, leaf) {
		return 0
	}
	count := 0
	for _, line := range matcher.lines(query.Contributing(visible, leaf)) {
		count += len(replaceEdits(term, line.text, replacement))
	}
	return count
}

// replaceEdits replaces every non-empty match of term in text, a line
// without its line end. Unlike a result's hits, matches aren't capped.
func replaceEdits(term *query.Content, text, replacement string) []protocol.ReplaceEdit {
	line := []byte(text)
	var groups [][]int
	if term.Literal == "" && strings.Contains(replacement, "$") {
		groups = term.Re.FindAllSubmatchIndex(line, -1)
	}
	var edits []protocol.ReplaceEdit
	for _, loc := range term.FindAllIndex(line, -1) {
		if loc[1] <= loc[0] {
			continue
		}
		newText := replacement
		if groups != nil {
			newText = expand(term, replacement, line, loc, groups)
		}
		r := UTF16Range(text, loc[0], loc[1])
		edits = append(edits, protocol.ReplaceEdit{Start: r.Start, End: r.End, NewText: newText})
	}
	return edits
}

// expand fills the $1 and ${name} of a /regex/ term's replacement with
// what the match at loc captured. groups are every match's group indexes in
// line; whole-word matching may skip into a match, so one that isn't among
// them is matched again on its own.
func expand(term *query.Content, template string, line []byte, loc []int, groups [][]int) string {
	for _, g := range groups {
		if g[0] == loc[0] && g[1] == loc[1] {
			return string(term.Re.Expand(nil, []byte(template), line, g))
		}
	}
	match := line[loc[0]:loc[1]]
	if g := term.Re.FindSubmatchIndex(match); g != nil {
		return string(term.Re.Expand(nil, []byte(template), match, g))
	}
	return template
}
