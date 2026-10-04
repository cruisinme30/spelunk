package trigram

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
	"github.com/cruisinme30/unified-search/daemon/internal/symbols"
)

// SearchBudget is how long one search may run before it returns what it
// has, marked truncated.
const SearchBudget = 2 * time.Second

// Repo is a repo's published index plus what results need to name it.
type Repo struct {
	ID    string
	Name  string
	Root  string // absolute path
	Shard *Shard
	// Overlay holds the files saved, created or deleted since Shard was
	// built, read again: Shard's docs at Masked paths are out of date or
	// gone, and Overlay has the current version of those that still exist.
	// Both are nil when nothing changed.
	Overlay *Shard
	Masked  map[string]bool
	// History says when each file last changed in Git; nil outside Git or
	// before the history index is read.
	History FileHistory
}

// FileHistory is what the working-tree engine needs from a repo's history:
// for since: on current files and for the "Uncommitted changes" badge.
type FileHistory interface {
	// LastCommit returns the newest commit in the history window that
	// changed path; ok is false when there is none.
	LastCommit(path string) (commit protocol.LastCommit, at time.Time, ok bool)
	// Dirty reports whether path has uncommitted changes.
	Dirty(path string) bool
}

// shards returns the repo's shards: the built one, then the overlay.
func (r *Repo) shards() []*Shard {
	if r.Overlay == nil {
		return []*Shard{r.Shard}
	}
	return []*Shard{r.Shard, r.Overlay}
}

// current reports whether a doc of shard is the file as it is now: the
// overlay's docs always are, the built shard's unless masked.
func (r *Repo) current(shard *Shard, doc *Doc) bool {
	return shard != r.Shard || !r.Masked[doc.Path]
}

// changedSince reports whether a file changed after a time, for since: on
// current files. In Git that is its newest commit, or now when it has
// uncommitted changes; elsewhere it is the file's modification time.
func (r *Repo) changedSince(doc *Doc, after time.Time) bool {
	if r.History == nil {
		return !doc.ModTime.Before(after)
	}
	if r.History.Dirty(doc.Path) {
		return true
	}
	_, at, ok := r.History.LastCommit(doc.Path)
	return ok && !at.Before(after)
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
//
// A search that runs past SearchBudget returns what it found so far,
// marked truncated; only a cancelled ctx makes it return an error.
func Search(ctx context.Context, plan *query.Plan, repos []Repo, planID int, emit func(protocol.ResultItem)) (Stats, error) {
	ctx, cancel := context.WithTimeout(ctx, SearchBudget)
	defer cancel()
	s := &searcher{ctx: ctx, plan: plan, planID: planID, emit: emit, hidden: map[int]int{}, terms: termCache{}}
	onlyFiles := plan.Kinds[query.KindFile] && !plan.Kinds[query.KindLine]

	if plan.Kinds[query.KindSymbol] {
		s.addSymbols(repos)
	}
	if plan.Kinds[query.KindFile] {
		s.addFileNames(repos, onlyFiles)
	}
	if len(plan.Terms) > 0 { // a code line needs a text term to match
		switch {
		case plan.Kinds[query.KindLine]:
			s.addCodeLines(repos)
		case onlyFiles && plan.KindFilter != nil:
			s.countCodeLinesHiddenByType(repos)
		}
	}
	if plan.CaseFilter != nil {
		s.hiddenByCase = countIgnoringCase(ctx, plan, repos) - s.counted
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return Stats{}, context.Cause(ctx)
	}
	return s.stats(resultUnit(plan, onlyFiles)), nil
}

// resultUnit is what a hidden-results note counts: definitions for sym:,
// files for file names alone, and matches otherwise.
func resultUnit(plan *query.Plan, onlyFiles bool) string {
	switch {
	case plan.Kinds[query.KindSymbol]:
		return "definitions"
	case onlyFiles:
		return "files"
	default:
		return "matches"
	}
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
	terms        termCache
}

// stopped reports whether the search should stop adding results: it ran
// out of time, was cancelled, or reached query.MaxResults. Any of these
// marks the results truncated.
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

// stats summarizes the search, with one hidden-results note per filter
// that hid something, counting unit.
func (s *searcher) stats(unit string) Stats {
	stats := Stats{Total: s.counted, Truncated: s.truncated, Hidden: []protocol.HiddenNote{}}
	if next := s.plan.Offset + s.plan.Limit; next < s.counted {
		stats.NextOffset = next
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

// docLeaf evaluates a leaf that doesn't depend on the file's text: its
// path, repo, language or modification time. Text terms are evaluated by
// the caller, so they never reach here.
func docLeaf(p query.Pred, repo *Repo, doc *Doc) bool {
	switch p := p.(type) {
	case *query.Path:
		return p.Re.MatchString(doc.Path)
	case *query.Repo:
		return p.Re.MatchString(repo.Name)
	case *query.Lang:
		return doc.Lang == p.Name
	case *query.Since:
		return repo.changedSince(doc, p.After)
	default:
		// sym: needs the symbol index; author: and msg: are history-only, and
		// the parser keeps them out of working-tree plans.
		return false
	}
}

// addFileNames adds a result for each file whose path satisfies the query,
// with text terms matched against the path (the "File names" section).
// countHidden credits each file that only one filter removed to that
// filter. It is set only when file names are the only results: otherwise
// the hidden-results note counts code matches, which addCodeLines credits.
func (s *searcher) addFileNames(repos []Repo, countHidden bool) {
	for i := range repos {
		s.addRepoFileNames(&repos[i], countHidden)
	}
}

// addRepoFileNames does addFileNames for one repo.
func (s *searcher) addRepoFileNames(repo *Repo, countHidden bool) {
	if s.repoExcluded(repo) {
		return
	}
	for _, shard := range repo.shards() {
		for i := range shard.Docs {
			if s.stopped() {
				return
			}
			if doc := &shard.Docs[i]; repo.current(shard, doc) {
				s.addFileName(repo, doc, countHidden)
			}
		}
	}
}

// addFileName adds a file-name result for doc if its path satisfies the query.
func (s *searcher) addFileName(repo *Repo, doc *Doc, countHidden bool) {
	leaf := func(p query.Pred) bool {
		if c, ok := p.(*query.Content); ok {
			return c.Re.MatchString(doc.Path)
		}
		return docLeaf(p, repo, doc)
	}
	if !query.Eval(s.plan.Pred, leaf) {
		if countHidden {
			s.countHidden(leaf, func([]*query.Content) int { return 1 })
		}
		return
	}
	item := protocol.ResultItem{
		Kind: query.KindFile, Ref: Ref{PlanID: s.planID, RepoID: repo.ID, Path: doc.Path}.String(),
		RepoID: repo.ID, Path: doc.Path, NameHits: s.nameHits(doc.Path, query.Contributing(s.plan.Pred, leaf)),
	}
	if repo.History != nil {
		item.Dirty = repo.History.Dirty(doc.Path)
		if commit, _, ok := repo.History.LastCommit(doc.Path); ok {
			item.LastCommit = &commit
		}
	}
	s.add(item)
}

// nameHits highlights the matching text terms in a path; with no text
// terms (a query of only f:, lang: …) it highlights the f: match.
func (s *searcher) nameHits(path string, terms []*query.Content) []protocol.Range {
	var ranges []protocol.Range
	for _, term := range terms {
		for _, loc := range term.Re.FindAllStringIndex(path, -1) {
			ranges = append(ranges, UTF16Range(path, loc[0], loc[1]))
		}
	}
	if len(s.plan.Terms) == 0 {
		for _, kid := range s.plan.Pred.Kids {
			if p, ok := kid.(*query.Path); ok {
				if hit, found := pathFilterHit(p, path); found {
					ranges = append(ranges, hit)
				}
			}
		}
	}
	return ranges
}

// pathFilterHit is the part of path an f: filter matched, for highlighting.
// A glob matches from a folder boundary, so a leading slash is left out.
func pathFilterHit(filter *query.Path, path string) (protocol.Range, bool) {
	loc := filter.Re.FindStringIndex(path)
	if len(loc) != 2 {
		return protocol.Range{}, false
	}
	start, end := loc[0], loc[1]
	if start < end && path[start] == '/' {
		start++
	}
	return UTF16Range(path, start, end), start < end
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

// addCodeLines adds a result for each matching line of each file that
// satisfies the query (the "Code" section), and credits the lines of a
// file that only one filter removed to that filter.
func (s *searcher) addCodeLines(repos []Repo) {
	for i := range repos {
		repo := &repos[i]
		s.forEachCandidate(repo, func(doc *Doc, matcher *lineMatcher, leaf func(query.Pred) bool) {
			if !query.Eval(s.plan.Pred, leaf) {
				s.countHidden(leaf, func(terms []*query.Content) int { return len(matcher.lines(terms)) })
				return
			}
			for _, line := range matcher.lines(query.Contributing(s.plan.Pred, leaf)) {
				s.add(s.lineResult(repo, doc, line))
			}
		})
	}
}

// addSymbols adds a result for each definition whose name sym: matches, in
// a file that satisfies the rest of the query (the "Definitions" section),
// and credits a definition that only one filter removed to that filter.
func (s *searcher) addSymbols(repos []Repo) {
	for i := range repos {
		repo := &repos[i]
		s.forEachCandidate(repo, func(doc *Doc, _ *lineMatcher, leaf func(query.Pred) bool) {
			for j := range doc.Symbols {
				symbol := &doc.Symbols[j]
				symbolLeaf := func(p query.Pred) bool {
					if sym, ok := p.(*query.Symbol); ok {
						return sym.Re.MatchString(symbol.Name)
					}
					return leaf(p)
				}
				if query.Eval(s.plan.Pred, symbolLeaf) {
					s.add(s.symbolResult(repo, doc, symbol))
				} else {
					s.countHidden(symbolLeaf, func([]*query.Content) int { return 1 })
				}
			}
		})
	}
}

// symbolResult builds a definition result. Its ref points at the name in
// the definition's line, so opening it selects the name.
func (s *searcher) symbolResult(repo *Repo, doc *Doc, symbol *symbols.Symbol) protocol.ResultItem {
	hits := []protocol.Hit{}
	for _, kid := range s.plan.Pred.Kids {
		walkSymbols(kid, func(sym *query.Symbol) {
			for _, loc := range sym.Re.FindAllStringIndex(symbol.Name, -1) {
				if loc[1] > loc[0] {
					r := UTF16Range(symbol.Name, loc[0], loc[1])
					hits = append(hits, protocol.Hit{Start: r.Start, End: r.End})
				}
			}
		})
	}
	ref := Ref{PlanID: s.planID, RepoID: repo.ID, Path: doc.Path, Line: symbol.Line, Length: utf16Len(symbol.Name)}
	if line, ok := lineOf(doc.Content, symbol.Line); ok {
		if at := strings.Index(line, symbol.Name); at >= 0 {
			ref.Column = utf16Len(line[:at])
		}
	}
	return protocol.ResultItem{
		Kind: query.KindSymbol, Ref: ref.String(), RepoID: repo.ID, Path: doc.Path, Line: symbol.Line,
		Name: symbol.Name, SymbolKind: symbol.Kind, Hits: hits,
	}
}

// walkSymbols calls visit for every sym: leaf of p that isn't negated.
func walkSymbols(p query.Pred, visit func(*query.Symbol)) {
	switch p := p.(type) {
	case *query.And:
		for _, kid := range p.Kids {
			walkSymbols(kid, visit)
		}
	case *query.Or:
		for _, kid := range p.Kids {
			walkSymbols(kid, visit)
		}
	case *query.Symbol:
		visit(p)
	}
}

// lineOf returns line number (1-based) of content, without its line end.
func lineOf(content []byte, number int) (string, bool) {
	for rest := content; number > 0; number-- {
		end := bytes.IndexByte(rest, '\n')
		if number == 1 {
			if end < 0 {
				end = len(rest)
			}
			return strings.TrimSuffix(string(rest[:end]), "\r"), true
		}
		if end < 0 {
			return "", false
		}
		rest = rest[end+1:]
	}
	return "", false
}

// countCodeLinesHiddenByType counts the code lines a type:file query
// leaves out, for its "N code matches hidden by type:file" note. It adds
// no results.
func (s *searcher) countCodeLinesHiddenByType(repos []Repo) {
	for i := range repos {
		s.forEachCandidate(&repos[i], func(_ *Doc, matcher *lineMatcher, leaf func(query.Pred) bool) {
			if query.Eval(s.plan.Pred, leaf) {
				s.hiddenByKind += len(matcher.lines(query.Contributing(s.plan.Pred, leaf)))
			}
		})
	}
}

// forEachCandidate calls visit for each file of repo that the trigram index
// says may match, in path order, with a matcher for the file's lines and
// leaf, which evaluates the query's leaves against the file. It stops when
// the search does.
func (s *searcher) forEachCandidate(repo *Repo, visit func(doc *Doc, matcher *lineMatcher, leaf func(query.Pred) bool)) {
	if s.repoExcluded(repo) {
		return
	}
	for _, shard := range repo.shards() {
		for _, id := range s.candidateDocs(shard) {
			if s.stopped() {
				return
			}
			doc := &shard.Docs[id]
			if !repo.current(shard, doc) {
				continue
			}
			matcher := newLineMatcher(doc.Content, s.terms)
			visit(doc, matcher, contentLeaf(repo, doc, matcher))
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
		return docLeaf(p, repo, doc)
	}
}

// candidateDocs narrows the docs to search using trigrams, in path order.
func (s *searcher) candidateDocs(shard *Shard) []uint32 {
	if ids := narrowShard(shard, s.plan.Pred, s.plan.CaseSensitive); ids != nil {
		return ids
	}
	return shard.allDocIDs()
}

// narrowShard returns the docs that can satisfy p, or nil for "any doc".
func narrowShard(shard *Shard, p query.Pred, caseSensitive bool) []uint32 {
	return Narrow(p, func(leaf query.Pred) []uint32 {
		switch leaf := leaf.(type) {
		case *query.Content:
			return shard.Candidates(ContentLiteral(leaf), caseSensitive)
		case *query.Symbol:
			// A definition's name is in the file's text.
			return shard.Candidates(query.RequiredLiteral(leaf.Re), caseSensitive)
		default:
			return nil // paths, languages…: any doc may qualify
		}
	})
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

const (
	// maxResultLineRunes is how much of a line a result shows; longer lines
	// (minified code, for example) are clipped to a window around the first
	// match.
	maxResultLineRunes = 400
	// clipLeadRunes is how many runes of a clipped line are kept before its
	// first match, so the match is shown with some of what leads up to it.
	clipLeadRunes = 80
	// ellipsis marks where a clipped line was cut.
	ellipsis = "…"
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

// UTF16Range converts byte offsets in text to a UTF-16 Range, the offsets
// JavaScript and VS Code count in.
func UTF16Range(text string, start, end int) protocol.Range {
	return protocol.Range{Start: utf16Len(text[:start]), End: utf16Len(text[:end])}
}
