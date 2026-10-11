package trigram

import (
	"sort"

	"github.com/cruisinme30/spelunk/daemon/internal/notebook"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/structural"
)

// This file is how in: keeps the matches in a file's comments, strings or
// code. The file is read by its language's rules (structural.Places), and
// each match is judged on its own, never the file: a line with retry in
// its code and in its comment keeps only the part in: wants. Prose (plain
// text, Markdown, a notebook's markdown cells) has nothing to tell apart,
// so in: keeps its matches whatever it asks for.

// region is a stretch of a file's text that in: reads as one place.
type region struct {
	start, end int
	kind       regionKind
}

type regionKind uint8

const (
	regionComment regionKind = iota + 1
	regionString
	regionProse
)

// inText is a file's text as in: reads it, worked out once per file.
type inText struct {
	read    bool
	regions []region // in order and apart; the text between them is code
	starts  []int    // where each line of the "\n"-only text starts
}

// inRegions reads the matcher's text for in:, once.
func (m *lineMatcher) inRegions() *inText {
	if !m.inRead.read {
		text := m.lfContent()
		m.inRead.starts = lineStarts(text)
		m.inRead.regions = regionsOf(text, m.lang, m.cells, m.inRead.starts)
		m.inRead.read = true
	}
	return &m.inRead
}

// regionsOf lists the comments, strings and prose of a file's text: a
// notebook cell by cell, each by its own language, anything else by
// language.
func regionsOf(text []byte, language string, cells []notebook.Cell, starts []int) []region {
	if cells == nil {
		return regionsIn(text, language, 0, len(text))
	}
	var regions []region
	for _, cell := range cells {
		first := cell.StartLine - 1
		if first < 0 || first >= len(starts) || cell.Lines <= 0 {
			continue
		}
		start, end := starts[first], len(text)
		if next := first + cell.Lines; next < len(starts) {
			end = starts[next]
		}
		language := cell.Lang
		if cell.Kind != notebook.KindCode {
			language = "markdown" // markdown and raw cells, and what a cell printed
		}
		regions = append(regions, regionsIn(text, language, start, end)...)
	}
	return regions
}

// regionsIn lists the regions of text[start:end], read as language.
func regionsIn(text []byte, language string, start, end int) []region {
	if structural.IsProse(language) {
		return []region{{start: start, end: end, kind: regionProse}}
	}
	places := structural.Places(text[start:end], language)
	regions := make([]region, len(places))
	for i, p := range places {
		kind := regionString
		if p.Kind == structural.Comment {
			kind = regionComment
		}
		regions[i] = region{start: start + p.Start, end: start + p.End, kind: kind}
	}
	return regions
}

// keeps reports whether in: keeps a match from start to end, offsets in the
// "\n"-only text.
func (m *lineMatcher) keeps(start, end int) bool {
	regions := m.inRegions().regions
	// The first region that ends after start is the only one start can be in.
	i := sort.Search(len(regions), func(i int) bool { return regions[i].end > start })
	if i == len(regions) || regions[i].start >= end {
		return m.in.Keeps(query.PlaceCode)
	}
	r := regions[i]
	if r.start > start || end > r.end {
		return m.in.Keeps(0) // partly in code, partly in a comment or string
	}
	switch r.kind {
	case regionProse:
		return true
	case regionComment:
		return m.in.Keeps(query.PlaceComment)
	default:
		return m.in.Keeps(query.PlaceString)
	}
}

// keepIn applies in: to term's matches in the file: a struct: template's
// are code; a match across lines was judged as it was found (span.go);
// each one-line match is judged on its own, and lines left with none drop.
func (m *lineMatcher) keepIn(term *query.Content, found []matchedLine) []matchedLine {
	switch {
	case m.in == 0 || len(found) == 0:
		return found
	case term.Structural != nil:
		if m.keepsStructure() {
			return found
		}
		return nil
	case m.terms.facts(term).multiline && !m.perLine:
		return found
	}
	starts := m.inRegions().starts
	kept := found[:0]
	for _, line := range found {
		lineStart := starts[line.number-1]
		hits := make([]byteHit, 0, len(line.hits))
		for _, hit := range line.hits {
			if m.keeps(lineStart+hit.start, lineStart+hit.end) {
				hits = append(hits, hit)
			}
		}
		if len(hits) > 0 {
			line.hits = hits
			kept = append(kept, line)
		}
	}
	return kept
}

// keepsStructure reports whether in: keeps a struct: template's matches,
// which are code: with no in:, one that keeps code, or in prose, which in:
// leaves alone.
func (m *lineMatcher) keepsStructure() bool {
	return m.in == 0 || m.in.Keeps(query.PlaceCode) || (m.cells == nil && structural.IsProse(m.lang))
}

// keepsOnLine returns whether in: keeps a match from start to end of line
// number (1-based), in bytes of the line, for a preview or a replace that
// matches the line again; nil without in:.
func (m *lineMatcher) keepsOnLine(number int) func(start, end int) bool {
	if m.in == 0 {
		return nil
	}
	starts := m.inRegions().starts
	if number < 1 || number > len(starts) {
		return func(int, int) bool { return false }
	}
	lineStart := starts[number-1]
	return func(start, end int) bool { return m.keeps(lineStart+start, lineStart+end) }
}
