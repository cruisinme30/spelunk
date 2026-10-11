package trigram

import (
	"bytes"
	"slices"
	"sort"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// This file is how a regex with \n in it matches across lines. Every
// other term matches one line at a time (match.go); such a regex is
// matched against the whole file instead, with "\r\n" read as "\n" so a
// file's line ends don't decide what it finds. A match becomes one result
// on the line it starts, carrying the lines it runs onto.

// maxSpanLines is how many lines after its first a result shows of a match
// that spans lines. A longer match still counts once and opens whole; the
// result says where it ends.
const maxSpanLines = 10

// spanLine is a later line of a match that spans lines, with the match's
// part of it (and any other term's matches there).
type spanLine struct {
	text string
	hits []byteHit
}

// spans reports whether the match runs onto later lines.
func (l *matchedLine) spans() bool { return l.endLine > l.number }

// last is the number of the match's last line.
func (l *matchedLine) last() int {
	if l.spans() {
		return l.endLine
	}
	return l.number
}

// firstByte is where the match starts in its first line, in bytes.
func (l *matchedLine) firstByte() int {
	if l.spans() {
		return l.startByte
	}
	start := len(l.text)
	for _, hit := range l.hits {
		start = min(start, hit.start)
	}
	return start
}

// sortHits orders the hits of each of the match's lines by where they start.
func (l *matchedLine) sortHits() {
	sortHits(l.hits)
	for i := range l.spanned {
		sortHits(l.spanned[i].hits)
	}
}

// clone copies l, so changing the copy's hits leaves l's alone.
func (l *matchedLine) clone() matchedLine {
	copied := *l
	copied.hits = slices.Clone(l.hits)
	if l.structure != nil {
		facts := *l.structure
		facts.holes = slices.Clone(facts.holes)
		copied.structure = &facts
	}
	copied.spanned = slices.Clone(l.spanned)
	for i := range copied.spanned {
		copied.spanned[i].hits = slices.Clone(l.spanned[i].hits)
	}
	return copied
}

// absorb adds other, which starts on one of l's lines, to l: its hits on
// lines l shows, up to maxLineHits a line, as for a term matched line by
// line, its later lines while l may show more, and its end when it ends
// later. l is a result of joinLines, whose startByte is set. Its cost
// depends on other alone, however many matches l has absorbed: a long
// minified line can have tens of thousands.
func (l *matchedLine) absorb(other matchedLine) {
	if other.number == l.number {
		l.startByte = min(l.startByte, other.firstByte())
	}
	lines := append([]spanLine{{text: other.text, hits: other.hits}}, other.spanned...)
	for i, line := range lines {
		switch at := other.number + i - l.number; {
		case at == 0:
			l.hits = appendHits(l.hits, line.hits)
		case at <= len(l.spanned):
			l.spanned[at-1].hits = appendHits(l.spanned[at-1].hits, line.hits)
		case at == len(l.spanned)+1 && at <= maxSpanLines:
			l.spanned = append(l.spanned, spanLine{text: line.text, hits: slices.Clone(line.hits)})
		}
	}
	if other.last() > l.last() {
		l.endLine, l.endColumn = other.endLine, other.endColumn
	}
	if other.structure != nil {
		l.addHoles(other.structure.holes)
	}
}

// appendHits adds more to a line's hits while it has fewer than
// maxLineHits.
func appendHits(hits, more []byteHit) []byteHit {
	return append(hits, more[:min(len(more), max(maxLineHits-len(hits), 0))]...)
}

// joinLines joins matches, sorted by their first line, whose lines
// overlap: matches on the same line, and every match that starts on a line
// an earlier one spans. It never changes found.
func joinLines(found []matchedLine) []matchedLine {
	var joined []matchedLine
	for _, line := range found {
		if n := len(joined); n > 0 && line.number <= joined[n-1].last() {
			joined[n-1].absorb(line)
			continue
		}
		first := line.clone()
		first.startByte = line.firstByte()
		joined = append(joined, first)
	}
	return joined
}

// lfContent is the matcher's text with each "\r\n" as "\n". The lines and
// their numbers are the same; only the "\r" a search drops is gone.
func (m *lineMatcher) lfContent() []byte {
	if m.lf == nil {
		m.lf = lfText(m.content)
	}
	return m.lf
}

// lfText returns text with each "\r\n" as "\n" (text itself when it has none).
func lfText(text []byte) []byte {
	if !bytes.Contains(text, []byte("\r\n")) {
		return text
	}
	return bytes.ReplaceAll(text, []byte("\r\n"), []byte("\n"))
}

// matchAcrossLines finds term's matches in the whole text, each a result
// on the line it starts, joined where their lines overlap. A match no line
// shows any of (a lone newline, say) highlights nothing and is dropped.
func (m *lineMatcher) matchAcrossLines(term *query.Content) []matchedLine {
	text := m.lfContent()
	locs := term.FindAllIndex(text, -1)
	if len(locs) == 0 || m.halted(len(text)) {
		return nil
	}
	lines := newLineIndex(text)
	var found []matchedLine
	for _, loc := range locs {
		if m.halted(loc[1] - loc[0] + 1) {
			break
		}
		if m.in != 0 && !m.keeps(loc[0], loc[1]) {
			continue // in: judges a match across lines as a whole
		}
		if match, ok := lines.match(loc[0], loc[1], term.TermIndex); ok {
			found = append(found, match)
		}
	}
	return joinLines(found)
}

// lineIndex finds the lines of a text whose lines end in "\n" alone.
type lineIndex struct {
	text   []byte
	starts []int
	// texts holds, by 0-based line, the text of the lines results have
	// asked for, so the many matches of one long line share one copy.
	texts map[int]string
}

func newLineIndex(text []byte) lineIndex {
	return lineIndex{text: text, starts: lineStarts(text), texts: map[int]string{}}
}

// lineText returns the text of 0-based line i, without its "\n".
func (x lineIndex) lineText(i int) string {
	text, ok := x.texts[i]
	if !ok {
		start, end := x.bounds(i)
		text = string(x.text[start:end])
		x.texts[i] = text
	}
	return text
}

// of returns the 0-based line that byte offset is on.
func (x lineIndex) of(offset int) int {
	return sort.Search(len(x.starts), func(i int) bool { return x.starts[i] > offset }) - 1
}

// bounds returns where 0-based line i starts and ends, without its "\n".
func (x lineIndex) bounds(i int) (start, end int) {
	start, end = x.starts[i], len(x.text)
	if i+1 < len(x.starts) {
		end = x.starts[i+1]
	}
	if end > start && x.text[end-1] == '\n' {
		end--
	}
	return start, end
}

// shown returns the part of 0-based line i, in bytes from its start, that
// marks the match from start to end. On a line the match runs onto, its
// indentation isn't marked: a highlighted indent reads as noise. from >= to
// when the line shows none of it.
func (x lineIndex) shown(i, start, end int) (from, to int) {
	lineStart, lineEnd := x.bounds(i)
	from, to = max(start, lineStart)-lineStart, min(end, lineEnd)-lineStart
	if start < lineStart {
		line := x.text[lineStart:lineEnd]
		from = max(from, len(line)-len(bytes.TrimLeft(line, " \t")))
	}
	return from, to
}

// match turns the bytes from start to end into a result: its first line
// and up to maxSpanLines more, each with the part of the match on it.
// ok is false when the match shows nothing on those lines.
func (x lineIndex) match(start, end, termIndex int) (match matchedLine, ok bool) {
	if start == end {
		return matchedLine{}, false
	}
	first, last := x.of(start), x.of(end-1)
	for i := first; i <= min(last, first+maxSpanLines); i++ {
		line := spanLine{text: x.lineText(i)}
		if from, to := x.shown(i, start, end); from < to {
			line.hits = []byteHit{{start: from, end: to, termIndex: termIndex}}
			ok = true
		}
		if i == first {
			match = matchedLine{number: i + 1, text: line.text, hits: line.hits}
		} else {
			match.spanned = append(match.spanned, line)
		}
	}
	if last > first {
		firstStart, _ := x.bounds(first)
		lastStart, lastEnd := x.bounds(last)
		match.startByte = start - firstStart
		match.endLine, match.endColumn = last+1, utf16Len(string(x.text[lastStart:min(end, lastEnd)]))
	}
	return match, ok
}

// splitAcrossLines takes the terms that span lines out of terms (a regex
// with \n, or a struct: template, read as language), returning the rest and,
// by line number, the parts of the taken terms' matches on lines first to
// last of content.
func splitAcrossLines(terms []*query.Content, content []byte, language string, first, last int, m *lineMatcher) ([]*query.Content, map[int][]byteHit) {
	acrossLines := map[int][]byteHit{}
	rest := slices.DeleteFunc(slices.Clone(terms), func(term *query.Content) bool {
		var found map[int][]byteHit
		switch {
		case term.Structural != nil && !m.keepsStructure():
			// in: leaves out code, all a template matches
		case term.Structural != nil:
			found = hitsOfStructure(lfText(content), term, language, first, last)
		case term.Multiline:
			found = hitsAcrossLines(lfText(content), term, first, last, m)
		default:
			return false
		}
		for number, hits := range found {
			acrossLines[number] = append(acrossLines[number], hits...)
		}
		return true
	})
	return rest, acrossLines
}

// hitsAcrossLines returns, by line number, the parts of term's matches
// that fall on lines first to last (1-based) of text, a text whose lines
// end in "\n" alone. A preview marks a match that spans lines with them.
func hitsAcrossLines(text []byte, term *query.Content, first, last int, m *lineMatcher) map[int][]byteHit {
	lines := newLineIndex(text)
	hits := map[int][]byteHit{}
	for _, loc := range term.FindAllIndex(text, -1) {
		if loc[0] == loc[1] || (m.in != 0 && !m.keeps(loc[0], loc[1])) {
			continue
		}
		for i := max(lines.of(loc[0]), first-1); i <= min(lines.of(loc[1]-1), last-1); i++ {
			if from, to := lines.shown(i, loc[0], loc[1]); from < to {
				hits[i+1] = append(hits[i+1], byteHit{start: from, end: to, termIndex: term.TermIndex})
			}
		}
	}
	return hits
}
