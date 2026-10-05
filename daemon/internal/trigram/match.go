package trigram

import (
	"bytes"
	"regexp"
	"regexp/syntax"
	"slices"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

type byteHit struct {
	start, end int
	termIndex  int
}

// matchedLine is a line with one or more matches.
type matchedLine struct {
	number int // 1-based
	text   string
	hits   []byteHit
}

// lineMatcher finds, and remembers, which lines each term matches in one file.
type lineMatcher struct {
	content []byte
	terms   termCache
	cache   map[*query.Content][]matchedLine
}

func newLineMatcher(content []byte, terms termCache) *lineMatcher {
	return &lineMatcher{content: content, terms: terms, cache: map[*query.Content][]matchedLine{}}
}

// termCache holds what matching needs to know about each term, worked out
// once per search and shared by every file it matches.
type termCache map[*query.Content]*termFacts

// termFacts is what matching needs to know about one term.
type termFacts struct {
	// anchored means the regex uses ^, $, \A or \z. Matching it against a
	// whole file would read ^ as the start of the file, not of a line, so
	// anchored terms are matched line by line.
	anchored bool
	// literal finds where the term may match, when hasLiteral.
	literal    literalFinder
	hasLiteral bool
}

// facts returns what matching needs to know about term.
func (c termCache) facts(term *query.Content) *termFacts {
	facts, ok := c[term]
	if !ok {
		facts = &termFacts{anchored: hasAnchors(term.Re)}
		facts.literal, facts.hasLiteral = newLiteralFinder(term)
		c[term] = facts
	}
	return facts
}

// hasAnchors reports whether re asserts a line or text boundary.
func hasAnchors(re *regexp.Regexp) bool {
	parsed, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return true
	}
	var visit func(*syntax.Regexp) bool
	visit = func(r *syntax.Regexp) bool {
		switch r.Op { //nolint:exhaustive // only the four anchors matter; every other op recurses below
		case syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText:
			return true
		}
		for _, sub := range r.Sub {
			if visit(sub) {
				return true
			}
		}
		return false
	}
	return visit(parsed)
}

// matches returns the lines term matches. Matching is per line, as in
// ripgrep: a match never spans a newline, and ^ and $ anchor lines.
func (m *lineMatcher) matches(term *query.Content) []matchedLine {
	if found, ok := m.cache[term]; ok {
		return found
	}
	var found []matchedLine
	if !m.terms.facts(term).anchored {
		found = m.scanForLines(term)
	} else {
		found = m.everyLine(term)
	}
	m.cache[term] = found
	return found
}

// scanForLines finds candidate lines by matching the whole file, then
// matches each candidate line on its own. That skips the many lines with
// no match, and it is sound for terms without anchors: a line that
// matches on its own also matches the file at that line.
func (m *lineMatcher) scanForLines(term *query.Content) []matchedLine {
	content := m.content
	next := m.finder(term)
	var found []matchedLine
	// lineStart is the byte offset where line number begins. Each search
	// resumes there, so the newlines between lineStart and a match are all
	// that is left to count to get the match's line number.
	number, lineStart := 1, 0
	for {
		at := next(lineStart)
		if at < 0 {
			return found
		}
		start := bytes.LastIndexByte(content[:at], '\n') + 1
		number += bytes.Count(content[lineStart:start], []byte("\n"))
		end := len(content)
		if i := bytes.IndexByte(content[at:], '\n'); i >= 0 {
			end = at + i
		}
		if line, ok := matchLine(term, content[start:end], number); ok {
			found = append(found, line)
		}
		if end == len(content) {
			return found
		}
		lineStart, number = end+1, number+1
	}
}

// finder returns how to find the next position, at or after a line start,
// where term may match: a fast literal search when that is sound, or the
// regex itself.
func (m *lineMatcher) finder(term *query.Content) func(from int) int {
	if facts := m.terms.facts(term); facts.hasLiteral && facts.literal.usableOn(m.content) {
		return func(from int) int { return facts.literal.index(m.content, from) }
	}
	return func(from int) int {
		loc := term.Re.FindIndex(m.content[from:])
		if loc == nil {
			return -1
		}
		return from + loc[0]
	}
}

// everyLine matches term against each line in turn.
func (m *lineMatcher) everyLine(term *query.Content) []matchedLine {
	var found []matchedLine
	number := 0
	for rest := m.content; len(rest) > 0; {
		number++
		line := rest
		if end := bytes.IndexByte(rest, '\n'); end >= 0 {
			line, rest = rest[:end], rest[end+1:]
		} else {
			rest = nil
		}
		if matched, ok := matchLine(term, line, number); ok {
			found = append(found, matched)
		}
	}
	return found
}

// matchLine returns the first non-empty matches of term in one line,
// without its trailing "\r" (see lineHits).
func matchLine(term *query.Content, line []byte, number int) (matchedLine, bool) {
	line = bytes.TrimSuffix(line, []byte("\r"))
	hits := lineHits(term, func(n int) [][]int { return term.Re.FindAllIndex(line, n) })
	if len(hits) == 0 {
		return matchedLine{}, false
	}
	return matchedLine{number: number, text: string(line), hits: hits}, true
}

// maxLineHits is how many matches of one term in one line are kept. A
// result shows a window of maxResultLineRunes runes from shortly before the
// first match, so later ones are never shown; finding every one of the
// millions in a long minified line would take seconds and gigabytes.
const maxLineHits = 2 * maxResultLineRunes

// lineHits returns the first maxLineHits non-empty matches of term that
// findAll (a FindAllIndex over one line, given a limit) reports. Empty
// matches highlight nothing and are dropped; if the limit fills up with
// them, the whole line is searched, so a match after them isn't missed.
func lineHits(term *query.Content, findAll func(n int) [][]int) []byteHit {
	locs := findAll(maxLineHits)
	hits := nonEmptyHits(locs, term.TermIndex)
	if len(hits) == 0 && len(locs) == maxLineHits {
		hits = nonEmptyHits(findAll(-1), term.TermIndex)
	}
	return hits[:min(len(hits), maxLineHits)]
}

// nonEmptyHits turns the non-empty matches of a term into hits.
func nonEmptyHits(locs [][]int, termIndex int) []byteHit {
	var hits []byteHit
	for _, loc := range locs {
		if loc[1] > loc[0] {
			hits = append(hits, byteHit{start: loc[0], end: loc[1], termIndex: termIndex})
		}
	}
	return hits
}

// lines merges the matched lines of several terms, in line order.
func (m *lineMatcher) lines(terms []*query.Content) []matchedLine {
	byNumber := map[int]*matchedLine{}
	var order []int
	for _, term := range terms {
		for _, line := range m.matches(term) {
			if merged, ok := byNumber[line.number]; ok {
				merged.hits = append(merged.hits, line.hits...)
				continue
			}
			copied := line
			copied.hits = append([]byteHit(nil), line.hits...)
			byNumber[line.number] = &copied
			order = append(order, line.number)
		}
	}
	slices.Sort(order)
	merged := make([]matchedLine, len(order))
	for i, number := range order {
		line := byNumber[number]
		sortHits(line.hits)
		merged[i] = *line
	}
	return merged
}

// sortHits orders hits by where they start in the line.
func sortHits(hits []byteHit) {
	slices.SortStableFunc(hits, func(a, b byteHit) int { return a.start - b.start })
}
