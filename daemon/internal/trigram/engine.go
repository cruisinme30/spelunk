package trigram

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/symbols"
)

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
	// Open holds the repo-relative paths of its files open in the editor,
	// for is:open; see WithOpenFiles.
	Open map[string]bool
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

// Search runs plan over repos, in order, and calls emit for each result on
// the requested page. File-name results come before code results. It also
// counts every result (for Total and Load more) and what each filter hid.
//
// A search that runs past engine.Budget returns what it found so far,
// marked truncated; only a cancelled ctx makes it return an error.
func Search(ctx context.Context, plan *query.Plan, repos []Repo, planID int, emit func(protocol.ResultItem)) (engine.Stats, error) {
	ctx, cancel := context.WithTimeout(ctx, engine.Budget)
	defer cancel()
	s := newSearcher(ctx, plan, planID, emit)
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
		case onlyFiles && plan.TypeFilter != nil:
			s.countCodeLinesHiddenByType(repos)
		}
	}
	if s.holding {
		s.sendHeld()
	}
	if plan.CaseFilter != nil {
		s.hiddenByCase = engine.CountIgnoringCase(ctx, plan, repos, Search) - s.counted
	}
	if plan.WordFilter != nil {
		s.hiddenByWord = engine.CountPartialWords(ctx, plan, repos, Search) - s.counted
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return engine.Stats{}, context.Cause(ctx)
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

// searcher holds one search's progress.
type searcher struct {
	ctx    context.Context
	plan   *query.Plan
	planID int
	emit   func(protocol.ResultItem)

	counted   int
	truncated bool
	// facets count the results by repo, language and top folder.
	facets *engine.Facets
	// hidden counts, per filter index, results that only that filter removed.
	hidden map[int]int
	// hiddenByType counts code matches a type:file query left out.
	hiddenByType int
	// hiddenByCase counts results that differ only in case from case:yes.
	hiddenByCase int
	// hiddenByWord counts results that are only parts of words under word:yes.
	hiddenByWord int
	terms        termCache

	// Best-match order (see rank.go): started is when the search began,
	// rankTerms the text terms outside a NOT, literals each term's text by
	// term index, and held the page's results while holding.
	started   time.Time
	rankTerms []*query.Content
	literals  map[int]string
	holding   bool
	held      []*pageGroup
}

func newSearcher(ctx context.Context, plan *query.Plan, planID int, emit func(protocol.ResultItem)) *searcher {
	s := &searcher{
		ctx: ctx, plan: plan, planID: planID, emit: emit, hidden: map[int]int{}, terms: termCache{},
		facets:  engine.NewFacets(engine.FacetRepo, engine.FacetLang, engine.FacetFolder),
		started: time.Now(), literals: map[int]string{},
	}
	s.holding = s.ranking()
	query.VisitPositive(plan.Pred, func(term *query.Content) {
		s.rankTerms = append(s.rankTerms, term)
		s.literals[term.TermIndex] = term.Literal
	})
	return s
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

// add counts a result in doc of repo, and in its facets, and emits it if
// it falls on the requested page. In best-match order the page is held and
// sent sorted once it is full, or once rankWindow has passed; score and
// quality rank the result's file.
func (s *searcher) add(repo *Repo, doc *Doc, item protocol.ResultItem, score, quality int) {
	if s.counted >= query.MaxResults {
		s.truncated = true
		return
	}
	if s.holding && time.Since(s.started) > rankWindow {
		s.sendHeld()
	}
	if s.plan.OnPage(s.counted) {
		if s.holding {
			s.hold(item, score, quality)
		} else {
			s.emit(item)
		}
	}
	s.counted++
	s.facets.CountRepo(repo.Name)
	s.facets.CountLang(doc.Lang)
	s.facets.CountFolder(doc.Path)
	if s.holding && s.counted == s.plan.Offset+s.plan.Limit {
		s.sendHeld()
	}
}

// stats summarizes the search, with one hidden-results note per filter
// that hid something, counting unit.
func (s *searcher) stats(unit string) engine.Stats {
	stats := engine.NewStats(s.plan, s.counted, s.truncated)
	stats.Facets = s.facets.List()
	hidden := engine.Hidden{ByFilter: s.hidden, ByCase: s.hiddenByCase, ByWord: s.hiddenByWord, ByType: s.hiddenByType}
	stats.AddHidden(s.plan, hidden, unit, "matches")
	return stats
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
	case *query.Is:
		return repo.inState(doc.Path, p.State)
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
//
// Matching a path is cheap, so in best-match order every matching file is
// found first and then added best first. A name that a term matches only
// fuzzily ranks after one whose path has the term, other things equal, and
// better fuzzy matches first.
func (s *searcher) addFileNames(repos []Repo, countHidden bool) {
	if !s.ranking() {
		s.forEachFileName(repos, countHidden, func(repo *Repo, doc *Doc) { s.addFileName(repo, doc, docRank{}) })
		return
	}
	var matched []rankedCandidate
	s.forEachFileName(repos, countHidden, func(repo *Repo, doc *Doc) {
		rank := s.rankDoc(repo, doc)
		if fuzzy := s.fuzzyNameScore(doc.Path); fuzzy > 0 {
			rank.score += scoreNameFuzzy
			rank.fuzzy = fuzzy
		}
		matched = append(matched, rankedCandidate{repo: repo, doc: doc, rank: rank})
	})
	sortByRank(matched)
	for _, c := range matched {
		s.addFileName(c.repo, c.doc, c.rank)
	}
}

// forEachFileName calls visit for each current file of repos whose path
// satisfies the query, in path order. It stops when the search does.
func (s *searcher) forEachFileName(repos []Repo, countHidden bool, visit func(repo *Repo, doc *Doc)) {
	for i := range repos {
		repo := &repos[i]
		if s.plan.ExcludesRepo(repo.Name) {
			continue
		}
		for _, shard := range repo.shards() {
			for j := range shard.Docs {
				if s.stopped() {
					return
				}
				if doc := &shard.Docs[j]; repo.current(shard, doc) && s.fileNameMatches(repo, doc, countHidden) {
					visit(repo, doc)
				}
			}
		}
	}
}

// fileNameMatches reports whether doc's path satisfies the query, and
// credits a file that one filter alone hid to that filter.
func (s *searcher) fileNameMatches(repo *Repo, doc *Doc, countHidden bool) bool {
	leaf := fileNameLeaf(repo, doc)
	if query.Eval(s.plan.Pred, leaf) {
		return true
	}
	if countHidden {
		s.countHidden(leaf, func([]*query.Content) int { return 1 })
	}
	return false
}

// fileNameLeaf evaluates a leaf against a file's path: text terms match
// the path instead of the file's text, and a fuzzy term also matches a
// name that has its characters in order (see fuzzy.go). content: terms
// never match a path.
func fileNameLeaf(repo *Repo, doc *Doc) func(query.Pred) bool {
	return func(p query.Pred) bool {
		if c, ok := p.(*query.Content); ok {
			if c.ContentOnly {
				return false
			}
			if c.MatchString(doc.Path) {
				return true
			}
			return fuzzyMatches(c, doc.Path)
		}
		return docLeaf(p, repo, doc)
	}
}

// addFileName adds the file-name result for doc, whose path satisfies the query.
func (s *searcher) addFileName(repo *Repo, doc *Doc, rank docRank) {
	leaf := fileNameLeaf(repo, doc)
	item := protocol.ResultItem{
		Kind: query.KindFile, Ref: Ref{PlanID: s.planID, RepoID: repo.ID, Path: doc.Path}.String(),
		RepoID: repo.ID, Path: doc.Path, NameHits: s.nameHits(doc.Path, query.Contributing(s.plan.Pred, leaf)),
		RankReason: rank.reason,
	}
	if repo.History != nil {
		item.Dirty = repo.History.Dirty(doc.Path)
		if commit, _, ok := repo.History.LastCommit(doc.Path); ok {
			item.LastCommit = &commit
		}
	}
	s.add(repo, doc, item, rank.score, 0)
}

// nameHits highlights the matching text terms in a path, or the characters
// a fuzzy term matched; with no text terms (a query of only f:, lang: …) it
// highlights the f: match.
func (s *searcher) nameHits(path string, terms []*query.Content) []protocol.Range {
	var ranges []protocol.Range
	for _, term := range terms {
		locs := term.FindAllStringIndex(path, -1)
		if len(locs) == 0 {
			if offsets, _, fuzzy := fuzzyMatch(term, path); fuzzy {
				locs = fuzzyRanges(path, offsets)
			}
		}
		for _, loc := range locs {
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
	s.forEachCandidate(repos, func(c rankedCandidate, matcher *lineMatcher, leaf func(query.Pred) bool) {
		if !query.Eval(s.plan.Pred, leaf) {
			s.countHidden(leaf, func(terms []*query.Content) int { return len(matcher.lines(terms)) })
			return
		}
		for _, line := range matcher.lines(query.Contributing(s.plan.Pred, leaf)) {
			item := s.lineResult(c.repo, c.doc, line)
			item.RankReason = c.rank.reason
			quality := 0
			if s.holding {
				quality = s.lineQuality(line)
			}
			s.add(c.repo, c.doc, item, c.rank.score, quality)
		}
	})
}

// addSymbols adds a result for each definition whose name sym: matches and
// whose kind kind: allows, in a file that satisfies the rest of the query
// (the "Definitions" section), and credits a definition that only one
// filter removed to that filter.
func (s *searcher) addSymbols(repos []Repo) {
	s.forEachCandidate(repos, func(c rankedCandidate, _ *lineMatcher, leaf func(query.Pred) bool) {
		for j := range c.doc.Symbols {
			symbol := &c.doc.Symbols[j]
			symbolLeaf := func(p query.Pred) bool {
				switch p := p.(type) {
				case *query.Symbol:
					return p.Re.MatchString(symbol.Name)
				case *query.Kind:
					return symbol.Kind == p.Name
				default:
					return leaf(p)
				}
			}
			if query.Eval(s.plan.Pred, symbolLeaf) {
				item := s.symbolResult(c.repo, c.doc, symbol)
				item.RankReason = c.rank.reason
				s.add(c.repo, c.doc, item, c.rank.score, 0)
			} else {
				s.countHidden(symbolLeaf, func([]*query.Content) int { return 1 })
			}
		}
	})
}

// symbolResult builds a definition result. Its ref points at the name in
// the definition's line, so opening it selects the name.
func (s *searcher) symbolResult(repo *Repo, doc *Doc, symbol *symbols.Symbol) protocol.ResultItem {
	hits := []protocol.Hit{}
	for _, kid := range s.plan.Pred.Kids {
		query.VisitPositive(kid, func(sym *query.Symbol) {
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
	s.forEachCandidate(repos, func(_ rankedCandidate, matcher *lineMatcher, leaf func(query.Pred) bool) {
		if query.Eval(s.plan.Pred, leaf) {
			s.hiddenByType += len(matcher.lines(query.Contributing(s.plan.Pred, leaf)))
		}
	})
}

// forEachCandidate calls visit for each file of repos that the trigram
// index says may match, with a matcher for the file's lines and leaf,
// which evaluates the query's leaves against the file. Files come in path
// order, repo by repo, or best first in best-match order. It stops when
// the search does.
func (s *searcher) forEachCandidate(repos []Repo, visit func(c rankedCandidate, matcher *lineMatcher, leaf func(query.Pred) bool)) {
	candidates := s.candidates(repos)
	for _, c := range candidates {
		if s.stopped() {
			return
		}
		matcher := newFileMatcher(c.doc, s.terms)
		visit(c, matcher, contentLeaf(c.repo, c.doc, matcher))
	}
}

// candidates lists the current files of repos that the trigram index says
// may match, in path order, or ranked best first in best-match order.
func (s *searcher) candidates(repos []Repo) []rankedCandidate {
	var candidates []rankedCandidate
	for i := range repos {
		repo := &repos[i]
		if s.plan.ExcludesRepo(repo.Name) {
			continue
		}
		for _, shard := range repo.shards() {
			for _, id := range s.candidateDocs(shard) {
				if doc := &shard.Docs[id]; repo.current(shard, doc) {
					candidates = append(candidates, rankedCandidate{repo: repo, doc: doc})
				}
			}
		}
	}
	if s.ranking() {
		for i := range candidates {
			candidates[i].rank = s.rankDoc(candidates[i].repo, candidates[i].doc)
		}
		sortByRank(candidates)
	}
	return candidates
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
	return engine.AllIDs(len(shard.Docs))
}

// narrowShard returns the docs that can satisfy p, or nil for "any doc".
func narrowShard(shard *Shard, p query.Pred, caseSensitive bool) []uint32 {
	return engine.Narrow(p, func(leaf query.Pred) []uint32 {
		switch leaf := leaf.(type) {
		case *query.Content:
			return shard.Candidates(engine.ContentLiteral(leaf), caseSensitive)
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
