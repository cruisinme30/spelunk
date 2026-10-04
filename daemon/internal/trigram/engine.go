package trigram

import (
	"bytes"
	"context"
	"regexp"
	"regexp/syntax"
	"time"
	"unicode/utf8"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
)

// SearchBudget is how long one search may run before it returns what it
// has, marked truncated (plan: "Hard limits").
const SearchBudget = 2 * time.Second

// Repo is a repo's published shard plus what results need to name it.
type Repo struct {
	ID    string
	Name  string
	Root  string // absolute path
	Shard *Shard
}

// Stats describe one page of a search.
type Stats struct {
	Total      int // results counted, at most query.MaxResults
	Truncated  bool
	NextOffset int // where the next page starts; 0 when this is the last page
	Hidden     []protocol.HiddenNote
}

// Search runs plan over repos, in order, and calls emit for each result on
// the requested page. File-name results come before code results. It also
// counts every result (for Total and Load more) and what each filter hid.
func Search(ctx context.Context, plan *query.Plan, repos []Repo, planID int, emit func(protocol.ResultItem)) (Stats, error) {
	ctx, cancel := context.WithTimeout(ctx, SearchBudget)
	defer cancel()
	s := &searcher{ctx: ctx, plan: plan, planID: planID, emit: emit, hidden: map[int]int{}, anchors: anchorCache{}}
	onlyFiles := plan.Kinds[query.KindFile] && !plan.Kinds[query.KindLine]

	for i := range repos {
		if plan.Kinds[query.KindFile] {
			s.fileNames(&repos[i], onlyFiles)
		}
	}
	for i := range repos {
		if plan.Kinds[query.KindLine] && len(plan.Terms) > 0 {
			s.codeLines(&repos[i], true)
		} else if onlyFiles && plan.KindFilter != nil && len(plan.Terms) > 0 {
			s.codeLines(&repos[i], false) // count the code matches type:file hides
		}
	}
	if plan.CaseFilter != nil {
		s.hiddenByCase = countIgnoringCase(ctx, plan, repos) - s.counted
	}
	if err := context.Cause(ctx); err != nil && ctx.Err() == context.Canceled {
		return Stats{}, err
	}
	return s.stats(onlyFiles), nil
}

// countIgnoringCase counts the results plan would have without case:yes.
func countIgnoringCase(ctx context.Context, plan *query.Plan, repos []Repo) int {
	folded := plan.IgnoringCase()
	folded.Offset, folded.Limit = 0, 0 // count only
	stats, err := Search(ctx, folded, repos, 0, func(protocol.ResultItem) {})
	if err != nil {
		return 0
	}
	return stats.Total
}

// searcher holds one search's progress.
type searcher struct {
	ctx    context.Context
	plan   *query.Plan
	planID int
	emit   func(protocol.ResultItem)

	counted   int
	truncated bool
	// hidden counts, per filter index, results that only that filter removed.
	hidden map[int]int
	// hiddenByKind counts code matches a type:file query left out.
	hiddenByKind int
	// hiddenByCase counts results that differ only in case from case:yes.
	hiddenByCase int
	anchors      anchorCache
}

func (s *searcher) stopped() bool {
	if s.ctx.Err() != nil {
		s.truncated = true
	}
	return s.truncated
}

// add counts a result and emits it if it falls on the requested page.
func (s *searcher) add(item protocol.ResultItem) {
	if s.counted >= query.MaxResults {
		s.truncated = true
		return
	}
	if s.counted >= s.plan.Offset && s.counted < s.plan.Offset+s.plan.Limit {
		s.emit(item)
	}
	s.counted++
}

func (s *searcher) stats(onlyFiles bool) Stats {
	stats := Stats{Total: s.counted, Truncated: s.truncated, Hidden: []protocol.HiddenNote{}}
	if next := s.plan.Offset + s.plan.Limit; next < s.counted {
		stats.NextOffset = next
	}
	unit := "matches"
	if onlyFiles {
		unit = "files"
	}
	for _, f := range s.plan.Filters {
		if n := s.hidden[f.Index]; n > 0 {
			stats.Hidden = append(stats.Hidden, protocol.HiddenNote{Reason: f.Reason, Filter: f.Text, Count: n, Unit: unit, Undo: f.Undo})
		}
	}
	if s.hiddenByCase > 0 {
		c := s.plan.CaseFilter
		stats.Hidden = append(stats.Hidden, protocol.HiddenNote{Reason: c.Reason, Filter: c.Text, Count: s.hiddenByCase, Unit: unit, Undo: c.Undo})
	}
	if s.hiddenByKind > 0 && s.plan.KindFilter != nil {
		k := s.plan.KindFilter
		stats.Hidden = append(stats.Hidden, protocol.HiddenNote{Reason: k.Reason, Filter: k.Text, Count: s.hiddenByKind, Unit: "matches", Undo: k.Undo})
	}
	return stats
}

// repoExcluded reports whether a top-level repo: rules the whole repo out.
func (s *searcher) repoExcluded(repo *Repo) bool {
	for _, kid := range s.plan.Pred.Kids {
		if r, ok := kid.(*query.Repo); ok && !r.Re.MatchString(repo.Name) {
			return true
		}
	}
	return false
}

// docLeaf evaluates the leaves that don't depend on text.
func docLeaf(p query.Pred, repo *Repo, doc *Doc) (value, ok bool) {
	switch p := p.(type) {
	case *query.Path:
		return p.Re.MatchString(doc.Path), true
	case *query.Repo:
		return p.Re.MatchString(repo.Name), true
	case *query.Lang:
		return doc.Lang == p.Name, true
	case *query.Since:
		return !doc.ModTime.Before(p.After), true
	case *query.Symbol:
		return false, true // symbol definitions come with the symbol index
	case *query.Author, *query.Message:
		return false, true // history-only; the parser keeps them out of these plans
	}
	return false, false
}

// fileNames adds a result for each file whose path satisfies the query,
// with text terms matched against the path (mock 1: "File names").
func (s *searcher) fileNames(repo *Repo, countHidden bool) {
	if s.repoExcluded(repo) {
		return
	}
	for i := range repo.Shard.Docs {
		if s.stopped() {
			return
		}
		doc := &repo.Shard.Docs[i]
		leaf := func(p query.Pred) bool {
			if c, ok := p.(*query.Content); ok {
				return c.Re.MatchString(doc.Path)
			}
			value, _ := docLeaf(p, repo, doc)
			return value
		}
		if !query.Eval(s.plan.Pred, leaf) {
			if countHidden {
				s.countHidden(leaf, func([]*query.Content) int { return 1 })
			}
			continue
		}
		s.add(protocol.ResultItem{
			Kind: query.KindFile, Ref: Ref{PlanID: s.planID, RepoID: repo.ID, Path: doc.Path}.String(),
			RepoID: repo.ID, Path: doc.Path, NameHits: s.nameHits(doc.Path, query.Contributing(s.plan.Pred, leaf)),
			Dirty: false,
		})
	}
}

// nameHits highlights the matching text terms in a path; with no text
// terms (a query of only f:, lang: …) it highlights the f: match.
func (s *searcher) nameHits(path string, terms []*query.Content) []protocol.Range {
	var ranges []protocol.Range
	for _, term := range terms {
		for _, loc := range term.Re.FindAllStringIndex(path, -1) {
			ranges = append(ranges, utf16Range(path, loc[0], loc[1]))
		}
	}
	if len(s.plan.Terms) == 0 {
		for _, kid := range s.plan.Pred.Kids {
			if p, ok := kid.(*query.Path); ok {
				if loc := p.Re.FindStringIndex(path); loc != nil && loc[1] > loc[0] {
					ranges = append(ranges, utf16Range(path, loc[0], loc[1]))
				}
			}
		}
	}
	return ranges
}

// countHidden credits a result that fails only one filter to that filter.
// size says how many results the item would have produced given the terms
// that would then contribute.
func (s *searcher) countHidden(leaf func(query.Pred) bool, size func([]*query.Content) int) {
	if len(s.plan.Filters) == 0 {
		return
	}
	kids := s.plan.Pred.Kids
	failing := -1
	for i, kid := range kids {
		if !query.Eval(kid, leaf) {
			if failing >= 0 {
				return // fails more than one conjunct: no single filter hid it
			}
			failing = i
		}
	}
	for _, f := range s.plan.Filters {
		if f.Index == failing {
			without := &query.And{Kids: append(append([]query.Pred{}, kids[:failing]...), kids[failing+1:]...)}
			s.hidden[failing] += size(query.Contributing(without, leaf))
			return
		}
	}
}

// codeLines adds a result for each matching line of each file that
// satisfies the query. With emit false it only counts what type:file hid.
func (s *searcher) codeLines(repo *Repo, emit bool) {
	if s.repoExcluded(repo) {
		return
	}
	shard := repo.Shard
	for _, id := range s.candidateDocs(shard) {
		if s.stopped() {
			return
		}
		doc := &shard.Docs[id]
		matcher := newLineMatcher(doc.Content, s.anchors)
		leaf := contentLeaf(repo, doc, matcher)
		if !query.Eval(s.plan.Pred, leaf) {
			if emit {
				s.countHidden(leaf, func(terms []*query.Content) int { return len(matcher.lines(terms)) })
			}
			continue
		}
		lines := matcher.lines(query.Contributing(s.plan.Pred, leaf))
		if !emit {
			s.hiddenByKind += len(lines)
			continue
		}
		for _, line := range lines {
			s.add(s.lineResult(repo, doc, line))
		}
	}
}

// contentLeaf evaluates leaves against a file's text: a content term is
// true when it matches at least one line.
func contentLeaf(repo *Repo, doc *Doc, matcher *lineMatcher) func(query.Pred) bool {
	return func(p query.Pred) bool {
		if c, ok := p.(*query.Content); ok {
			return len(matcher.matches(c)) > 0
		}
		value, _ := docLeaf(p, repo, doc)
		return value
	}
}

// candidateDocs narrows the docs to search using trigrams, in path order.
func (s *searcher) candidateDocs(shard *Shard) []uint32 {
	ids := narrow(shard, s.plan.Pred, s.plan.CaseSensitive)
	if ids == nil {
		ids = make([]uint32, len(shard.Docs))
		for i := range ids {
			ids[i] = uint32(i)
		}
	}
	return ids
}

// narrow returns the docs that can satisfy p, or nil for "any doc".
func narrow(shard *Shard, p query.Pred, caseSensitive bool) []uint32 {
	switch p := p.(type) {
	case *query.And:
		var result []uint32
		narrowed := false
		for _, kid := range p.Kids {
			ids := narrow(shard, kid, caseSensitive)
			if ids == nil {
				continue
			}
			if !narrowed {
				result, narrowed = ids, true
			} else {
				result = intersect(result, ids)
			}
		}
		if !narrowed {
			return nil
		}
		return result
	case *query.Or:
		result := []uint32{}
		for _, kid := range p.Kids {
			ids := narrow(shard, kid, caseSensitive)
			if ids == nil {
				return nil
			}
			result = union(result, ids)
		}
		return result
	case *query.Content:
		literal := p.Literal
		if literal == "" {
			literal = query.RequiredLiteral(p.Re)
		}
		return shard.Candidates(literal, caseSensitive)
	default: // NOT, paths, languages…: any doc may qualify
		return nil
	}
}

// lineResult builds a code result, clipping very long lines around the match.
func (s *searcher) lineResult(repo *Repo, doc *Doc, line matchedLine) protocol.ResultItem {
	text, hits := clipLine(line.text, line.hits)
	first := line.hits[0]
	ref := Ref{
		PlanID: s.planID, RepoID: repo.ID, Path: doc.Path, Line: line.number,
		Column: utf16Len(line.text[:first.start]), Length: utf16Len(line.text[first.start:first.end]),
	}
	return protocol.ResultItem{
		Kind: query.KindLine, Ref: ref.String(), RepoID: repo.ID, Path: doc.Path,
		Line: line.number, Text: text, Hits: hits,
	}
}

// byteHit is a match in a line, in byte offsets.
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
	lowered []byte // content with ASCII lowercased, made on first use
	anchors anchorCache
	cache   map[*query.Content][]matchedLine
}

func newLineMatcher(content []byte, anchors anchorCache) *lineMatcher {
	return &lineMatcher{content: content, anchors: anchors, cache: map[*query.Content][]matchedLine{}}
}

// anchorCache remembers, per term, whether its regex uses ^, $, \A or \z.
// Shared by every file of one search.
type anchorCache map[*query.Content]bool

// wholeTextCheck reports whether matching term against the whole file finds
// every line that matches on its own. Anchors break that (^ means the start
// of the file, not of a line), so anchored terms are matched line by line.
func (a anchorCache) wholeTextCheck(term *query.Content) bool {
	anchored, ok := a[term]
	if !ok {
		anchored = hasAnchors(term.Re)
		a[term] = anchored
	}
	return !anchored
}

// hasAnchors reports whether re asserts a line or text boundary.
func hasAnchors(re *regexp.Regexp) bool {
	parsed, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return true
	}
	var visit func(*syntax.Regexp) bool
	visit = func(r *syntax.Regexp) bool {
		switch r.Op {
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
	if m.anchors.wholeTextCheck(term) {
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
	number, lineStart := 1, 0 // lineStart begins line number
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
	if literal, ok := newLiteralFinder(term); ok && literal.usableOn(m.content) {
		if literal.folded && m.lowered == nil {
			m.lowered = lowerASCII(m.content)
		}
		return func(from int) int { return literal.index(m.content, m.lowered, from) }
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

// matchLine returns the non-empty matches of term in one line, without
// its trailing "\r".
func matchLine(term *query.Content, line []byte, number int) (matchedLine, bool) {
	line = bytes.TrimSuffix(line, []byte("\r"))
	var hits []byteHit
	for _, loc := range term.Re.FindAllIndex(line, -1) {
		if loc[1] > loc[0] { // empty matches highlight nothing
			hits = append(hits, byteHit{start: loc[0], end: loc[1], termIndex: term.TermIndex})
		}
	}
	if len(hits) == 0 {
		return matchedLine{}, false
	}
	return matchedLine{number: number, text: string(line), hits: hits}, true
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
	sortInts(order)
	merged := make([]matchedLine, len(order))
	for i, number := range order {
		line := byNumber[number]
		sortHits(line.hits)
		merged[i] = *line
	}
	return merged
}

func sortInts(values []int) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func sortHits(hits []byteHit) {
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].start < hits[j-1].start; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
}

// maxResultLineRunes is how much of a line a result shows; longer lines
// are clipped to a window around the first match.
const (
	maxResultLineRunes = 400
	clipLeadRunes      = 80
	ellipsis           = "…"
)

// clipLine converts byte hits to UTF-16 hits on the text shown, clipping
// long lines (such as minified code) to a window around the first match.
func clipLine(text string, hits []byteHit) (string, []protocol.Hit) {
	start, end := 0, len(text)
	prefix, suffix := "", ""
	if utf8.RuneCountInString(text) > maxResultLineRunes {
		start = backRunes(text, hits[0].start, clipLeadRunes)
		end = forwardRunes(text, start, maxResultLineRunes)
		if start > 0 {
			prefix = ellipsis
		}
		if end < len(text) {
			suffix = ellipsis
		}
	}
	shown := prefix + text[start:end] + suffix
	shift := utf16Len(prefix)
	var out []protocol.Hit
	for _, h := range hits {
		if h.start < start || h.end > end {
			continue // outside the clipped window
		}
		out = append(out, protocol.Hit{
			Start:     shift + utf16Len(text[start:h.start]),
			End:       shift + utf16Len(text[start:h.end]),
			TermIndex: h.termIndex,
		})
	}
	return shown, out
}

// backRunes moves n runes left from byte offset i.
func backRunes(text string, i, n int) int {
	for ; n > 0 && i > 0; n-- {
		_, size := utf8.DecodeLastRuneInString(text[:i])
		i -= size
	}
	return i
}

// forwardRunes moves n runes right from byte offset i.
func forwardRunes(text string, i, n int) int {
	for ; n > 0 && i < len(text); n-- {
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
	}
	return i
}

// utf16Len is the length of text in UTF-16 code units.
func utf16Len(text string) int {
	n := 0
	for _, r := range text {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// utf16Range converts byte offsets in text to a UTF-16 Range.
func utf16Range(text string, start, end int) protocol.Range {
	return protocol.Range{Start: utf16Len(text[:start]), End: utf16Len(text[:end])}
}
