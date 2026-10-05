package trigram

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruisinme30/unified-search/daemon/internal/lang"
	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
	"github.com/cruisinme30/unified-search/daemon/internal/symbols"
)

// ErrStale means the file or line behind a ref is gone.
var ErrStale = errors.New("result no longer exists")

// Preview returns the lines around a result, read from disk so they match
// what opening the file shows. With the plan that produced the result, it
// highlights every term that made the file match, in the term's color;
// without it (the plan was forgotten), only the result's own match. A file
// that is gone, or is no longer a regular text file, is ErrStale.
func Preview(repo *Repo, ref Ref, plan *query.Plan, contextLines int) (protocol.Preview, error) {
	content, info, err := readText(filepath.Join(repo.Root, filepath.FromSlash(ref.Path)), 0)
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

	language := lang.Detect(ref.Path, content)
	var terms []*query.Content
	// With type:file the words matched the file's name, not its text, so the
	// text has nothing to mark.
	if plan != nil && (!ref.IsFile() || plan.Kinds[query.KindLine]) {
		doc := &Doc{Path: ref.Path, Lang: language, ModTime: info.ModTime(), Content: content}
		terms = query.Contributing(plan.Pred, contentLeaf(repo, doc, newLineMatcher(content, termCache{})))
	}
	hits := []protocol.LineHits{}
	for number := first; number <= last; number++ {
		ranges := TermHits(lines[number-1], terms)
		if number == ref.Line && len(ranges) == 0 {
			ranges = []protocol.Hit{{Start: ref.Column, End: ref.Column + ref.Length}}
		}
		if len(ranges) > 0 {
			hits = append(hits, protocol.LineHits{Line: number, Ranges: ranges})
		}
	}
	return protocol.Preview{
		Kind: protocol.PreviewKindFile, Path: ref.Path, FirstLine: first, Lines: lines[first-1 : last],
		FocusLine: focus, Hits: hits, DirtyLines: []int{}, Symbols: outline(symbols.Extract(language, content), focus),
	}, nil
}

// outline lists the definitions a preview names under the code: the
// members of the class or interface defined on the focus line, or else
// every definition in the file.
func outline(found []symbols.Symbol, focus int) []protocol.OutlineSymbol {
	start, end := 0, len(found)
	for i, symbol := range found {
		if symbol.Line != focus || (symbol.Kind != protocol.SymbolKindClass && symbol.Kind != protocol.SymbolKindInterface) {
			continue
		}
		start, end = i+1, i+1
		for end < len(found) && found[end].Kind == protocol.SymbolKindMethod {
			end++
		}
	}
	names := make([]protocol.OutlineSymbol, 0, end-start)
	for _, symbol := range found[start:end] {
		names = append(names, protocol.OutlineSymbol{Name: symbol.Name, Line: symbol.Line})
	}
	return names
}

// OpenTarget returns where opening a result goes: the line and column of
// its first match, or the top of the file for a file-name result. It
// returns ErrStale if the file is gone.
func OpenTarget(repo *Repo, ref Ref) (protocol.OpenTarget, error) {
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

// TermHits returns every non-empty match of terms in one line of text, by
// start, in UTF-16 offsets. The history engine marks diff lines with it too.
func TermHits(line string, terms []*query.Content) []protocol.Hit {
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
		r := UTF16Range(line, h.start, h.end)
		out[i] = protocol.Hit{Start: r.Start, End: r.End, TermIndex: h.termIndex}
	}
	return out
}
