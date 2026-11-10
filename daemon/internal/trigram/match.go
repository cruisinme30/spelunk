package trigram

import (
	"bytes"
	"context"
	"regexp"
	"regexp/syntax"
	"slices"

	"github.com/cruisinme30/spelunk/daemon/internal/notebook"
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
	// For a match that spans lines, where it starts in text, in bytes, and
	// where it ends in line endLine, in UTF-16 units. endLine is 0
	// otherwise. joinLines also sets startByte of a line it joins matches
	// on, to the first of them.
	startByte, endLine, endColumn int
	// structure is set for the result of a struct: match (structure.go).
	structure *structureFacts
}

// lineMatcher finds, and remembers, which lines each term matches in one file.
type lineMatcher struct {
	content []byte
	file    *Doc // immutable indexed generation; nil for standalone text
	terms   termCache
	cache   map[*query.Content]matchResult
	// ctx cancels bounded match enumeration. Callers that do not have a
	// request context leave the background context installed by the
	// constructor.
	ctx context.Context
	// complete keeps legacy wrappers exact. Request-driven search, preview and
	// history paths leave it false and propagate ErrMatchBudget instead.
	complete bool
	// matchErr is the first error from a term scan. Its partial lines are a
	// lower bound, but callers must not use their absence through NOT.
	matchErr error
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
	// cells are a notebook's cells, which no match may run across; nil for
	// any other text.
	cells []notebook.Cell
	// lang is the language of a file's text, which struct: terms read it
	// by; "" for a text in none.
	lang string
	// in is the plan's in:, which keeps matches in comments, strings or
	// code (places.go); inRead is the text read for it.
	in     query.Places
	inRead inText
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

// matchResult caches one term's lines together with whether their scan was
// complete. Keeping the error per term makes repeated predicate evaluation
// return the same status instead of turning a partial cache into a success.
type matchResult struct {
	lines []matchedLine
	err   error
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
	return &lineMatcher{
		content: content,
		terms:   terms,
		cache:   map[*query.Content]matchResult{},
		ctx:     context.Background(),
	}
}

// newFileMatcher returns a lineMatcher for doc's text that knows its
// definitions: the symbol index's, or, when it has none for the file (or
// symbols aren't indexed), the ones found the first time a ref: term asks.
func newFileMatcher(doc *Doc, terms termCache) *lineMatcher {
	m := newLineMatcher(doc.Content, terms)
	m.file = doc
	m.lang = doc.Lang
	m.cells = doc.Cells
	found, done := doc.Symbols, doc.Symbols != nil
	m.definitions = func() []symbols.Symbol {
		if !done {
			found, done = symbolsOf(doc.Lang, doc.Content, doc.Cells), true
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
	// crossesLines means the regex can match a newline ([^;], \s, (?s).)
	// though it isn't multiline. Searching the whole text with it would
	// find matches that run from one line onto later ones, which no line
	// has on its own, so it is never used to find candidate lines.
	crossesLines bool
	// literal finds where the term may match, when hasLiteral.
	literal    literalFinder
	hasLiteral bool
}

// facts returns what matching needs to know about term.
func (c termCache) facts(term *query.Content) *termFacts {
	facts, ok := c[term]
	if !ok {
		facts = &termFacts{anchored: hasAnchors(term.Re), multiline: term.Multiline, crossesLines: canMatchNewline(term.Re)}
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

// canMatchNewline reports whether re can match a "\n": it has a dot that
// matches one ((?s)), a class that takes one ([^;], \s, [\s\S]) or the
// character itself.
func canMatchNewline(re *regexp.Regexp) bool {
	parsed, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return true
	}
	var visit func(*syntax.Regexp) bool
	visit = func(r *syntax.Regexp) bool {
		switch r.Op { //nolint:exhaustive // only the ops that match characters matter; every other op recurses below
		case syntax.OpAnyChar:
			return true
		case syntax.OpLiteral:
			return slices.Contains(r.Rune, '\n')
		case syntax.OpCharClass:
			for i := 0; i+1 < len(r.Rune); i += 2 {
				if r.Rune[i] <= '\n' && '\n' <= r.Rune[i+1] {
					return true
				}
			}
			return false
		}
		return slices.ContainsFunc(r.Sub, visit)
	}
	return visit(parsed)
}

// matches returns the lines term matches. Matching is per line, as in
// ripgrep: a match never spans a newline, and ^ and $ anchor lines. Only a
// regex with \n in it matches across lines (see span.go).
func (m *lineMatcher) matches(term *query.Content) []matchedLine {
	if result, ok := m.cache[term]; ok {
		m.rememberMatchErr(result.err)
		return result.lines
	}
	var found []matchedLine
	var err error
	switch facts := m.terms.facts(term); {
	case term.Structural != nil:
		found, err = m.matchStructure(term)
	case facts.multiline && !m.perLine:
		found, err = m.matchAcrossLines(term)
	case !facts.anchored:
		found, err = m.scanForLines(term)
	default:
		found, err = m.everyLine(term)
	}
	found = m.keepIn(term, found)
	if term.Reference && m.definitions != nil {
		found = withoutDefinitions(found, term, m.definitions())
	}
	if m.cells != nil {
		found = slices.DeleteFunc(found, func(line matchedLine) bool {
			return line.spans() && !notebook.SameCell(m.cells, line.number, line.last())
		})
	}
	m.cache[term] = matchResult{lines: found, err: err}
	m.rememberMatchErr(err)
	return found
}

func (m *lineMatcher) rememberMatchErr(err error) {
	if err != nil && m.matchErr == nil {
		m.matchErr = err
	}
	if context.Cause(m.ctx) != nil {
		m.cut = true
	}
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
// matches on its own also matches the file at that line. A regex that can
// cross lines and has no literal to look for is matched a line at a time
// instead (see finder).
func (m *lineMatcher) scanForLines(term *query.Content) ([]matchedLine, error) {
	content := m.content
	next := m.finder(term)
	if next == nil {
		return m.everyLine(term)
	}
	var found []matchedLine
	// lineStart is the byte offset where line number begins. Each search
	// resumes there, so the newlines between lineStart and a match are all
	// that is left to count to get the match's line number.
	number, lineStart := 1, 0
	for {
		at, read := next(lineStart)
		if at < 0 || m.halted(read) {
			return found, context.Cause(m.ctx)
		}
		start := bytes.LastIndexByte(content[:at], '\n') + 1
		number += bytes.Count(content[lineStart:start], []byte("\n"))
		end := len(content)
		if i := bytes.IndexByte(content[at:], '\n'); i >= 0 {
			end = at + i
		}
		line, ok, err := m.matchLine(term, content[start:end], number)
		if err != nil {
			return found, err
		}
		if ok {
			found = append(found, line)
		}
		if end == len(content) {
			return found, nil
		}
		lineStart, number = end+1, number+1
	}
}

// finder returns how to find the next position, at or after a line start,
// where term may match: a fast literal search when that is sound, or the
// regex itself. It also returns how many bytes the search read, at least.
// It returns nil for a regex that can cross lines: its leftmost match from
// a line start can begin there and run on to a far line, only to fail on
// the line alone, and searching again from each next line made matching a
// file quadratic in its lines ([^;]*zz over a file without semicolons).
func (m *lineMatcher) finder(term *query.Content) func(from int) (at, read int) {
	if facts := m.terms.facts(term); facts.hasLiteral && facts.literal.usableOn(m.content) {
		positions := foldedPositions{lower: -1, upper: -1}
		return func(from int) (int, int) {
			var at int
			if facts.literal.folded {
				at = positions.index(m.content, facts.literal.needle, from)
			} else {
				at = facts.literal.index(m.content, from)
			}
			if at < 0 {
				return -1, len(m.content) - from
			}
			return at, at - from + len(facts.literal.needle)
		}
	}
	if m.terms.facts(term).crossesLines {
		return nil
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
func (m *lineMatcher) everyLine(term *query.Content) ([]matchedLine, error) {
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
			return found, context.Cause(m.ctx)
		}
		matched, ok, err := m.matchLine(term, line, number)
		if err != nil {
			return found, err
		}
		if ok {
			found = append(found, matched)
		}
	}
	return found, nil
}

// matchLine returns the first kept, non-empty matches of term in one line,
// without its trailing "\r". Applying in: while enumerating ensures rejected
// hits do not fill the bounded highlight window before a later valid one.
func (m *lineMatcher) matchLine(term *query.Content, line []byte, number int) (matchedLine, bool, error) {
	line = bytes.TrimSuffix(line, []byte("\r"))
	keep := m.keepsOnLine(number)
	var hits []byteHit
	var err error
	if m.complete {
		hits = completeLineHits(term, line, keep)
	} else {
		hits, err = lineHitsContext(m.ctx, term, line, keep)
	}
	if err != nil {
		return matchedLine{}, false, err
	}
	if len(hits) == 0 {
		return matchedLine{}, false, nil
	}
	return matchedLine{number: number, text: string(line), hits: hits}, true, nil
}

// maxLineHits is how many matches of one term in one line are kept. A
// result shows a window of maxResultLineRunes runes from shortly before the
// first match, so later ones are never shown; finding every one of the
// millions in a long minified line would take seconds and gigabytes.
const maxLineHits = 2 * maxResultLineRunes

// lineHitsContext returns the first maxLineHits non-empty matches kept by
// keep. Empty and rejected candidates still spend the shared position budget,
// so a late match is either found or reported as unknown, never silently lost.
func lineHitsContext(ctx context.Context, term *query.Content, line []byte, keep func(int, int) bool) ([]byteHit, error) {
	var hits []byteHit
	err := term.VisitMatches(ctx, line, query.MaxMatchPositions, func(loc []int) bool {
		if loc[1] > loc[0] && (keep == nil || keep(loc[0], loc[1])) {
			hits = append(hits, byteHit{start: loc[0], end: loc[1], termIndex: term.TermIndex})
		}
		return len(hits) < maxLineHits
	})
	return hits, err
}

// completeLineHits is the exact, allocation-unbounded implementation used by
// compatibility wrappers that cannot report ErrMatchBudget. Request paths use
// lineHitsContext instead.
func completeLineHits(term *query.Content, line []byte, keep func(int, int) bool) []byteHit {
	var hits []byteHit
	for _, loc := range term.FindAllIndex(line, -1) {
		if loc[1] > loc[0] && (keep == nil || keep(loc[0], loc[1])) {
			hits = append(hits, byteHit{start: loc[0], end: loc[1], termIndex: term.TermIndex})
			if len(hits) == maxLineHits {
				break
			}
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
