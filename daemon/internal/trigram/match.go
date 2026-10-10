package trigram

import (
	"bytes"
	"regexp"
	"regexp/syntax"
	"slices"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/symbols"
)

type byteHit struct {
	start, end int
	termIndex  int
	// hole is the number of the struct: template hole whose code the hit
	// marks; 0 for the rest.
	hole int
}

// matchedLine is a line with one or more matches, or the first line of a
// match that spans lines (see span.go) with the lines it runs onto.
type matchedLine struct {
	number int // 1-based
	text   string
	hits   []byteHit
	// spanned holds the lines after number that a match spanning lines runs
	// onto, each with its part of the match: at most maxSpanLines of them,
	// so a longer match ends past the last. Nil for a match within a line.
	spanned []spanLine
	// For a match that spans lines, where it starts in text and where it
	// ends in line endLine, both in UTF-16 units. endLine is 0 otherwise.
	startColumn, endLine, endColumn int
	// structure is set for the result of a struct: match (structure.go).
	structure *structureFacts
}

// lineMatcher finds, and remembers, which lines each term matches in one file.
type lineMatcher struct {
	content []byte
	terms   termCache
	cache   map[*query.Content][]matchedLine
	// lf is content with each "\r\n" as "\n", made the first time a term
	// that spans lines needs it.
	lf []byte
	// perLine keeps every term to one line at a time, even one that spans
	// lines: a history search matches a commit's changed lines, which are
	// not one text.
	perLine bool
	// definitions returns the file's definitions, which ref: terms skip.
	// It is nil for texts that aren't files, such as commits.
	definitions func() []symbols.Symbol
	// lang is the language of a file's text, which struct: terms read it
	// by; "" for a text in none.
	lang string
	// gaveUp is set when a struct: term ran out of steps before the end
	// of the text, so its matches may be missing some.
	gaveUp bool
	// stop, when set, reports whether the search this matcher serves has
	// run out of time or been cancelled. Matching asks it every
	// stopCheckBytes of text it reads and, once it says yes, gives up on
	// the rest of the text and sets cut: one long file must not keep a
	// search running past its budget.
	stop func() bool
	cut  bool
	// work is how many bytes matching has read since it last asked stop.
	work int
}

// stopCheckBytes is how much text matching reads between asking whether
// the search should stop.
const stopCheckBytes = 64 << 10

// halted counts n more bytes of text read and reports whether matching
// should give up on the rest of the text (see lineMatcher.stop).
func (m *lineMatcher) halted(n int) bool {
	if m.cut {
		return true
	}
	if m.stop == nil {
		return false
	}
	m.work += n
	if m.work < stopCheckBytes {
		return false
	}
	m.work = 0
	m.cut = m.stop()
	return m.cut
}

func newLineMatcher(content []byte, terms termCache) *lineMatcher {
	return &lineMatcher{content: content, terms: terms, cache: map[*query.Content][]matchedLine{}}
}

// newFileMatcher returns a lineMatcher for doc's text that knows its
// definitions: the symbol index's, or, when it has none for the file (or
// symbols aren't indexed), the ones found the first time a ref: term asks.
func newFileMatcher(doc *Doc, terms termCache) *lineMatcher {
	m := newLineMatcher(doc.Content, terms)
	m.lang = doc.Lang
	found, done := doc.Symbols, doc.Symbols != nil
	m.definitions = func() []symbols.Symbol {
		if !done {
			found, done = symbols.Extract(doc.Lang, doc.Content), true
		}
		return found
	}
	return m
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
	// multiline means the term is a regex with \n, matched against the
	// whole text (see span.go).
	multiline bool
	// literal finds where the term may match, when hasLiteral.
	literal    literalFinder
	hasLiteral bool
}

// facts returns what matching needs to know about term.
func (c termCache) facts(term *query.Content) *termFacts {
	facts, ok := c[term]
	if !ok {
		facts = &termFacts{anchored: hasAnchors(term.Re), multiline: term.Multiline}
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
// ripgrep: a match never spans a newline, and ^ and $ anchor lines. Only a
// regex with \n in it matches across lines (see span.go).
func (m *lineMatcher) matches(term *query.Content) []matchedLine {
	if found, ok := m.cache[term]; ok {
		return found
	}
	var found []matchedLine
	switch facts := m.terms.facts(term); {
	case term.Structural != nil:
		found = m.matchStructure(term)
	case facts.multiline && !m.perLine:
		found = m.matchAcrossLines(term)
	case !facts.anchored:
		found = m.scanForLines(term)
	default:
		found = m.everyLine(term)
	}
	if term.Reference && m.definitions != nil {
		found = withoutDefinitions(found, term, m.definitions())
	}
	m.cache[term] = found
	return found
}

// withoutDefinitions drops the lines that define the name a ref: term
// looks for, leaving its uses.
func withoutDefinitions(found []matchedLine, term *query.Content, definitions []symbols.Symbol) []matchedLine {
	defining := map[int]bool{}
	for _, symbol := range definitions {
		if term.IsDefinitionOf(symbol.Name) {
			defining[symbol.Line] = true
		}
	}
	if len(defining) == 0 {
		return found
	}
	return slices.DeleteFunc(found, func(line matchedLine) bool { return defining[line.number] })
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
		at, read := next(lineStart)
		if at < 0 || m.halted(read) {
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
// regex itself. It also returns how many bytes the search read, at least.
func (m *lineMatcher) finder(term *query.Content) func(from int) (at, read int) {
	if facts := m.terms.facts(term); facts.hasLiteral && facts.literal.usableOn(m.content) {
		return func(from int) (int, int) {
			at := facts.literal.index(m.content, from)
			if at < 0 {
				return -1, len(m.content) - from
			}
			return at, at - from + len(facts.literal.needle)
		}
	}
	return func(from int) (int, int) {
		loc := term.Re.FindIndex(m.content[from:])
		if loc == nil {
			return -1, len(m.content) - from
		}
		return from + loc[0], loc[1]
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
		if m.halted(len(line) + 1) {
			return found
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
	hits := lineHits(term, func(n int) [][]int { return term.FindAllIndex(line, n) })
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

// lines merges the matched lines of several terms, in line order: the
// matches on one line become one result, and so do matches whose lines
// overlap when one of them spans lines.
func (m *lineMatcher) lines(terms []*query.Content) []matchedLine {
	var found []matchedLine
	for _, term := range terms {
		found = append(found, m.matches(term)...)
	}
	slices.SortStableFunc(found, func(a, b matchedLine) int { return a.number - b.number })
	merged := joinLines(found)
	for i := range merged {
		merged[i].sortHits()
	}
	return merged
}

// sortHits orders hits by where they start in the line.
func sortHits(hits []byteHit) {
	slices.SortStableFunc(hits, func(a, b byteHit) int { return a.start - b.start })
}
