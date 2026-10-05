package trigram

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/symbols"
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
	shown := make([]string, 0, last-first+1)
	for number := first; number <= last; number++ {
		line := lines[number-1]
		var own *Ref
		if number == ref.Line {
			own = &ref
		}
		text, ranges := markLine(line, terms, own)
		shown = append(shown, text)
		if len(ranges) > 0 {
			hits = append(hits, protocol.LineHits{Line: number, Ranges: ranges})
		}
	}
	return protocol.Preview{
		Kind: protocol.PreviewKindFile, Path: ref.Path, FirstLine: first, Lines: shown,
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

// maxPreviewLineRunes is how much of one line a preview shows; a longer
// line (minified code, a data file) is clipped to a window around its
// first match, so one line can't make a preview megabytes long.
const maxPreviewLineRunes = 2_000

// PreviewLine returns a line as a preview shows it, clipped if it is
// longer than maxPreviewLineRunes, with the non-empty matches of terms
// marked by start, in UTF-16 offsets into the text returned. The history
// engine shows diff lines with it too.
func PreviewLine(line string, terms []*query.Content) (string, []protocol.Hit) {
	text, hits := markLine(line, terms, nil)
	if hits == nil {
		hits = []protocol.Hit{}
	}
	return text, hits
}

// markLine is PreviewLine for a working-tree preview: when terms mark
// nothing on the result's own line (own, nil on other lines), its ref's
// match is marked instead.
func markLine(line string, terms []*query.Content, own *Ref) (string, []protocol.Hit) {
	var hits []byteHit
	for _, term := range terms {
		hits = append(hits, lineHits(term, func(n int) [][]int { return term.FindAllStringIndex(line, n) })...)
	}
	sortHits(hits)
	if len(hits) == 0 && own != nil {
		start, end := byteOffset(line, own.Column), byteOffset(line, own.Column+own.Length)
		if len(line) <= maxPreviewLineRunes || start >= end {
			// Marked as the ref says, even past the end of a line that changed.
			return clipShort(line, []protocol.Hit{{Start: own.Column, End: own.Column + own.Length}})
		}
		hits = []byteHit{{start: start, end: end}}
	}
	return clipWindow(line, hits, maxPreviewLineRunes)
}

// clipShort returns a line that needs no clipping with the hits given.
func clipShort(line string, hits []protocol.Hit) (string, []protocol.Hit) {
	text, _ := clipWindow(line, nil, maxPreviewLineRunes)
	return text, hits
}

// byteOffset converts a UTF-16 offset into text to a byte offset, clamped
// to the end of text.
func byteOffset(text string, units int) int {
	for i, r := range text {
		if units <= 0 {
			return i
		}
		units--
		if r >= 0x10000 {
			units--
		}
	}
	return len(text)
}
