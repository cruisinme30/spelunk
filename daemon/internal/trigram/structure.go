package trigram

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/structural"
)

// This file is how a struct: term matches: its template is matched against
// the whole file, read by the file's language (see the structural
// package), and each match becomes one result on the line it starts, like
// a regex with \n (span.go). The template's own code is marked in the
// term's color, each hole's code with its hole's number, and the result
// lists what each named hole took.

// structureFacts is what the result of a struct: match knows beyond the
// lines and marks of any match.
type structureFacts struct {
	// matchEnd is where a match on one line ends in its text, in bytes, so
	// opening it selects all of it; 0 for a match across lines.
	matchEnd int
	// holes is what the named holes of the matches on its lines took.
	holes []protocol.HoleValue
}

// maxHoleRunes is how much of what a hole took a result lists; the rest is
// cut, with an ellipsis.
const maxHoleRunes = 80

// matchStructure finds a struct: term's matches in the text, joined where
// their lines overlap.
func (m *lineMatcher) matchStructure(term *query.Content) []matchedLine {
	text := m.lfContent()
	matches, complete := term.Structural.Find(text, m.lang, term.IgnoreCase, -1)
	m.gaveUp = m.gaveUp || !complete
	if len(matches) == 0 {
		return nil
	}
	lines := newLineIndex(text)
	found := make([]matchedLine, 0, len(matches))
	for _, match := range matches {
		found = append(found, lines.structure(match, term.TermIndex))
	}
	return joinLines(found)
}

// structure turns a template's match into a result: its lines, as for any
// match across lines, marked where the template's code and each hole's
// code fall on them, with what the holes took.
func (x lineIndex) structure(match structural.Match, termIndex int) matchedLine {
	result, _ := x.match(match.Start, match.End, termIndex) // a match has code in it
	first := x.of(match.Start)
	result.hits = nil
	result.structure = &structureFacts{}
	for i := range result.spanned {
		result.spanned[i].hits = nil
	}
	mark := func(start, end, hole int) {
		for i := max(x.of(start), first); i <= min(x.of(end-1), first+len(result.spanned)); i++ {
			from, to := x.shown(i, start, end)
			if from >= to {
				continue
			}
			hit := byteHit{start: from, end: to, termIndex: termIndex, hole: hole}
			if i == first {
				result.hits = append(result.hits, hit)
			} else {
				result.spanned[i-first-1].hits = append(result.spanned[i-first-1].hits, hit)
			}
		}
	}
	for _, code := range match.Code {
		mark(code.Start, code.End, 0)
	}
	for _, hole := range match.Holes {
		mark(hole.Start, hole.End, hole.Number)
		result.addHoles([]protocol.HoleValue{{Hole: hole.Number, Name: hole.Name, Text: holeText(x.text[hole.Start:hole.End])}})
	}
	if !result.spans() {
		lineStart, _ := x.bounds(first)
		result.structure.matchEnd = match.End - lineStart
	}
	result.sortHits()
	return result
}

// addHoles adds what holes took to what l lists, each hole number once:
// joined matches on the same lines keep the first one's.
func (l *matchedLine) addHoles(holes []protocol.HoleValue) {
	if l.structure == nil {
		l.structure = &structureFacts{}
	}
	for _, hole := range holes {
		listed := false
		for _, have := range l.structure.holes {
			listed = listed || have.Hole == hole.Hole
		}
		if !listed {
			l.structure.holes = append(l.structure.holes, hole)
		}
	}
}

// holeText is what a hole took as a result lists it: its runs of space,
// line breaks among them, squeezed to one space, and cut at maxHoleRunes.
func holeText(code []byte) string {
	text := strings.Join(strings.FieldsFunc(string(code), unicode.IsSpace), " ")
	if utf8.RuneCountInString(text) <= maxHoleRunes {
		return text
	}
	return text[:forwardRunes(text, 0, maxHoleRunes-1)] + ellipsis
}

// hitsOfStructure returns, by line number, the marks of a struct: term's
// matches on lines first to last (1-based) of text, read as language: the
// template's code and each hole's code, as results mark them.
func hitsOfStructure(text []byte, term *query.Content, language string, first, last int) map[int][]byteHit {
	matches, _ := term.Structural.Find(text, language, term.IgnoreCase, -1)
	lines := newLineIndex(text)
	hits := map[int][]byteHit{}
	mark := func(start, end, hole int) {
		for i := max(lines.of(start), first-1); i <= min(lines.of(end-1), last-1); i++ {
			if from, to := lines.shown(i, start, end); from < to {
				hits[i+1] = append(hits[i+1], byteHit{start: from, end: to, termIndex: term.TermIndex, hole: hole})
			}
		}
	}
	for _, match := range matches {
		for _, code := range match.Code {
			mark(code.Start, code.End, 0)
		}
		for _, hole := range match.Holes {
			mark(hole.Start, hole.End, hole.Number)
		}
	}
	return hits
}
