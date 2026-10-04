package trigram

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruisinme30/unified-search/daemon/internal/lang"
	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
)

// ErrStale means the file or line behind a ref is gone.
var ErrStale = errors.New("result no longer exists")

// Preview returns the lines around a result, read from disk so they match
// what opening the file shows. With the plan that produced the result, it
// highlights every term that made the file match, in the term's color;
// without it (the plan was forgotten), only the result's own match.
func Preview(repo *Repo, ref Ref, plan *query.Plan, contextLines int) (protocol.Preview, error) {
	full := filepath.Join(repo.Root, filepath.FromSlash(ref.Path))
	content, err := os.ReadFile(full)
	if err != nil {
		return protocol.Preview{}, ErrStale
	}
	info, err := os.Stat(full)
	if err != nil {
		return protocol.Preview{}, ErrStale
	}
	lines := splitLines(content)
	focus := ref.Line
	if ref.IsFile() {
		focus = 1
	}
	if focus > len(lines) {
		return protocol.Preview{}, ErrStale
	}
	first := max(1, focus-contextLines)
	last := min(len(lines), focus+contextLines)
	if ref.IsFile() {
		last = min(len(lines), 1+2*contextLines) // the top of the file
	}

	var terms []*query.Content
	if plan != nil {
		doc := &Doc{Path: ref.Path, Lang: lang.Detect(ref.Path, content), ModTime: info.ModTime(), Content: content}
		terms = query.Contributing(plan.Pred, contentLeaf(repo, doc, newLineMatcher(content, anchorCache{})))
	}
	hits := []protocol.LineHits{}
	for number := first; number <= last; number++ {
		ranges := lineHits(lines[number-1], terms)
		if number == ref.Line && len(ranges) == 0 {
			ranges = []protocol.Hit{{Start: ref.Column, End: ref.Column + ref.Length}}
		}
		if len(ranges) > 0 {
			hits = append(hits, protocol.LineHits{Line: number, Ranges: ranges})
		}
	}
	return protocol.Preview{
		Kind: "file", Path: ref.Path, FirstLine: first, Lines: lines[first-1 : last],
		FocusLine: focus, Hits: hits, DirtyLines: []int{},
	}, nil
}

// Resolve returns where opening a result goes: the line and column of its
// first match, or the top of the file for a file-name result.
func Resolve(repo *Repo, ref Ref) (protocol.OpenTarget, error) {
	full := filepath.Join(repo.Root, filepath.FromSlash(ref.Path))
	if _, err := os.Stat(full); err != nil {
		return protocol.OpenTarget{}, ErrStale
	}
	target := protocol.OpenTarget{Path: full, Line: 1, Column: 1}
	if !ref.IsFile() {
		// OpenTarget.Column is 1-based, like VS Code's UI; refs are 0-based.
		target.Line, target.Column, target.Length = ref.Line, ref.Column+1, ref.Length
	}
	return target, nil
}

// splitLines splits text into the lines a search sees: split on "\n",
// with a trailing "\r" removed.
func splitLines(content []byte) []string {
	lines := strings.Split(string(content), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

// lineHits returns every non-empty match of terms in one line, by start.
func lineHits(line string, terms []*query.Content) []protocol.Hit {
	var hits []byteHit
	for _, term := range terms {
		for _, loc := range term.Re.FindAllStringIndex(line, -1) {
			if loc[1] > loc[0] {
				hits = append(hits, byteHit{start: loc[0], end: loc[1], termIndex: term.TermIndex})
			}
		}
	}
	sortHits(hits)
	out := make([]protocol.Hit, len(hits))
	for i, h := range hits {
		r := utf16Range(line, h.start, h.end)
		out[i] = protocol.Hit{Start: r.Start, End: r.End, TermIndex: h.termIndex}
	}
	return out
}
