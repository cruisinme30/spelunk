package trigram

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/symbols"
)

// ErrStale means the file or line behind a ref is gone.
var ErrStale = errors.New("result no longer exists")

// maxPreviewSpanLines is how many lines after its first a preview shows of
// a match that spans lines; a longer one is shown from its start.
const maxPreviewSpanLines = 200

// Preview returns the lines around a result, read from disk so they match
// what opening the file shows. With the plan that produced the result, it
// highlights every term that made the file match, in the term's color;
// without it (the plan was forgotten), only the result's own match. A
// match that spans lines is shown whole (up to maxPreviewSpanLines more
// lines), with the lines around it. A file that is gone, is no longer a
// regular text file, or has grown past maxBytes (the index's size limit;
// 0 for none) since it was indexed is ErrStale, and so is a path that a
// symbolic link leads out of the repo's folder.
func Preview(repo *Repo, ref Ref, plan *query.Plan, contextLines int, maxBytes int64) (protocol.Preview, error) {
	content, info, err := readTextIn(repo.Root, ref.Path, maxBytes)
	if err != nil {
		return protocol.Preview{}, ErrStale
	}
	return PreviewText(repo, ref, plan, contextLines, content, info.ModTime())
}

// PreviewText is Preview of a file whose text is already read: from a
// commit's tree, for a result at a revision. modTime stands in for the
// file's modification time.
func PreviewText(repo *Repo, ref Ref, plan *query.Plan, contextLines int, content []byte, modTime time.Time) (protocol.Preview, error) {
	contextLines = max(contextLines, 0)
	lines := splitLines(content)
	focus := ref.Line
	if ref.IsFile() {
		focus = 1
	}
	if focus > len(lines) {
		return protocol.Preview{}, ErrStale
	}
	focusEnd := focus
	if ref.Spans() {
		focusEnd = min(ref.EndLine, focus+maxPreviewSpanLines, len(lines))
	}
	first := max(1, focus-contextLines)
	last := min(len(lines), focusEnd+contextLines)
	if ref.IsFile() {
		last = min(len(lines), 1+2*contextLines) // the top of the file
	}

	language := lang.Detect(ref.Path, content)
	var terms []*query.Content
	// With type:file the words matched the file's name, not its text, so the
	// text has nothing to mark.
	if plan != nil && (!ref.IsFile() || plan.Kinds[query.KindLine]) {
		doc := &Doc{Path: ref.Path, Lang: language, ModTime: modTime, Content: content}
		terms = query.Contributing(plan.Pred, contentLeaf(repo, doc, newFileMatcher(doc, termCache{})))
	}
	// A term that spans lines is matched against the whole file; the others
	// line by line, as markLine does.
	terms, acrossLines := splitAcrossLines(terms, content, language, first, last)
	hits := []protocol.LineHits{}
	shown := make([]string, 0, last-first+1)
	for number := first; number <= last; number++ {
		line := lines[number-1]
		var own *Ref
		if number == ref.Line {
			own = &ref
		}
		text, ranges := markLine(line, terms, acrossLines[number], own)
		shown = append(shown, text)
		if len(ranges) > 0 {
			hits = append(hits, protocol.LineHits{Line: number, Ranges: ranges})
		}
	}
	preview := protocol.Preview{
		Kind: protocol.PreviewKindFile, Path: ref.Path, FirstLine: first, Lines: shown,
		FocusLine: focus, Hits: hits, DirtyLines: []int{}, Symbols: outline(symbols.Outline(language, content)),
		Ownership: ownership(repo, ref.Path),
	}
	if ref.Spans() && len(lines) > focus {
		preview.FocusEndLine = min(ref.EndLine, len(lines))
	}
	return preview, nil
}

// ownership says who owns the file at path and which CODEOWNERS line says
// so, for the preview's "Owned by" line; nil when the repo has no
// CODEOWNERS file.
func ownership(repo *Repo, path string) *protocol.Ownership {
	if repo.Owners == nil {
		return nil
	}
	rule, _ := repo.Owners.Match(path)
	return &protocol.Ownership{Owners: rule.Owners, File: repo.Owners.Path, Line: rule.Line}
}

// outline lists the file's definitions for the preview's Outline, each
// class, type or interface followed by its members.
func outline(entries []symbols.Entry) []protocol.OutlineSymbol {
	names := make([]protocol.OutlineSymbol, 0, len(entries))
	for _, entry := range entries {
		names = append(names, protocol.OutlineSymbol{Name: entry.Name, Line: entry.Line, Kind: entry.Kind, Depth: entry.Depth})
	}
	return names
}

// OpenTarget returns where opening a result goes: the line and column of
// its first match (to where it ends, for a match that spans lines), or the
// top of the file for a file-name result. It returns ErrStale if the file
// is gone.
func OpenTarget(repo *Repo, ref Ref) (protocol.OpenTarget, error) {
	full := filepath.Join(repo.Root, filepath.FromSlash(ref.Path))
	if _, err := os.Stat(full); err != nil {
		return protocol.OpenTarget{}, ErrStale
	}
	return targetIn(full, ref), nil
}

// RevisionTarget is OpenTarget for a result at a revision, which opens
// read-only with the file's text at that revision: path is where the file
// is (or was) in the repo, and label names the revision in the editor's
// title. The caller checks that the revision has the file.
func RevisionTarget(repo *Repo, ref Ref, label string) protocol.OpenTarget {
	target := targetIn(filepath.Join(repo.Root, filepath.FromSlash(ref.Path)), ref)
	target.Revision, target.RevisionLabel = ref.Revision, label
	return target
}

// targetIn is where opening ref goes in the file at full.
func targetIn(full string, ref Ref) protocol.OpenTarget {
	target := protocol.OpenTarget{Path: full, Line: 1, Column: 1}
	if !ref.IsFile() {
		// OpenTarget.Column is 1-based, like VS Code's UI; refs are 0-based.
		target.Line, target.Column, target.Length = ref.Line, ref.Column+1, ref.Length
		if ref.Spans() {
			target.EndLine, target.EndColumn = ref.EndLine, ref.EndColumn+1
		}
	}
	return target
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
	text, hits := markLine(line, terms, nil, nil)
	if hits == nil {
		hits = []protocol.Hit{}
	}
	return text, hits
}

// markLine is PreviewLine for a working-tree preview: marked are terms'
// matches in the line and the parts of matches across lines already found
// on it, and when those mark nothing on the result's own line (own, nil on
// other lines), its ref's match instead.
func markLine(line string, terms []*query.Content, acrossLines []byteHit, own *Ref) (string, []protocol.Hit) {
	hits := slices.Clone(acrossLines)
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
