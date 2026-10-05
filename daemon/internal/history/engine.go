package history

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// Repo is one repo's published history store plus what results need to
// name it.
type Repo struct {
	ID    string
	Name  string
	Root  string // absolute path
	Store *Store
}

// maxResultFiles is how many changed files one commit result lists; the
// preview lists them all.
const maxResultFiles = 20

// Search runs a history plan over repos and calls emit for each commit on
// the requested page as soon as it is found, newest first across all repos.
// It counts every match (for Total and Load more) and what each filter hid,
// like trigram.Search.
func Search(ctx context.Context, plan *query.Plan, repos []Repo, planID int, emit func(protocol.ResultItem)) (engine.Stats, error) {
	if plan.Mode != protocol.ModeHistory {
		return engine.Stats{}, errors.New("history engine got a working-tree plan")
	}
	ctx, cancel := context.WithTimeout(ctx, engine.Budget)
	defer cancel()
	s := &searcher{ctx: ctx, plan: plan, planID: planID, emit: emit, lines: trigram.NewLineFinder(), hidden: map[int]int{}}
	s.searchNewestFirst(repos)
	if plan.CaseFilter != nil {
		s.hiddenByCase = engine.CountIgnoringCase(ctx, plan, repos, Search) - s.counted
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return engine.Stats{}, context.Cause(ctx)
	}
	return s.stats(), nil
}

// searcher holds one search's progress.
type searcher struct {
	ctx    context.Context
	plan   *query.Plan
	planID int
	emit   func(protocol.ResultItem)

	lines     *trigram.LineFinder
	counted   int // matching commits so far
	truncated bool
	// hidden counts, per filter index, commits that only that filter removed.
	hidden map[int]int
	// hiddenByCase counts commits that differ only in case from case:yes.
	hiddenByCase int
}

// stats summarizes the search.
func (s *searcher) stats() engine.Stats {
	stats := engine.NewStats(s.plan, s.counted, s.truncated)
	for i := range s.plan.Filters {
		if f := &s.plan.Filters[i]; s.hidden[f.Index] > 0 {
			stats.Hidden = append(stats.Hidden, f.Note(s.hidden[f.Index], "commits"))
		}
	}
	if c := s.plan.CaseFilter; c != nil && s.hiddenByCase > 0 {
		stats.Hidden = append(stats.Hidden, c.Note(s.hiddenByCase, "commits"))
	}
	return stats
}

// stopped reports whether the search should stop: out of time, cancelled,
// or at query.MaxResults. Any of these marks the results truncated.
func (s *searcher) stopped() bool {
	if s.ctx.Err() != nil || s.counted >= query.MaxResults {
		s.truncated = true
	}
	return s.truncated
}

// searchNewestFirst visits the candidate commits of every repo, newest
// first across repos, so the first page goes out as soon as it is found.
func (s *searcher) searchNewestFirst(repos []Repo) {
	var cursors []*cursor
	for i := range repos {
		if repos[i].Store != nil && !s.plan.ExcludesRepo(repos[i].Name) {
			cursors = append(cursors, &cursor{repo: &repos[i]})
		}
	}
	for !s.stopped() {
		var next *cursor
		var newest *Commit
		for _, c := range cursors {
			if commit := s.peek(c); commit != nil && (newest == nil || commit.At.After(newest.At)) {
				next, newest = c, commit
			}
		}
		if next == nil {
			return
		}
		next.ids = next.ids[1:]
		s.visit(next.repo, newest)
	}
}

// cursor walks one repo's candidate commits, newest first.
type cursor struct {
	repo *Repo
	seg  int      // the next segment to narrow
	ids  []uint32 // the candidates of the segment before it not yet visited
}

// peek returns the cursor's next candidate commit, or nil when it has none
// left. Each segment is narrowed only when the walk reaches it.
func (s *searcher) peek(c *cursor) *Commit {
	segments := c.repo.Store.segments
	for len(c.ids) == 0 {
		if c.seg == len(segments) {
			return nil
		}
		c.ids = s.candidates(segments[c.seg])
		c.seg++
	}
	return &segments[c.seg-1].commits[c.ids[0]]
}

// visit counts a commit that matches, emitting it if it is on the page, or
// credits the filter that hid it. Top-level conditions on the commit itself
// (author:, since:, msg:, repo:) are checked first: they are cheap, and a
// commit that fails one needs no diff matching unless that one is a filter.
func (s *searcher) visit(repo *Repo, c *Commit) {
	failed := -1 // the index of the one top-level commit condition that fails
	for i, kid := range s.plan.Pred.Kids {
		if matched, ok := topLevelCommitLeaf(repo, c, kid); ok && !matched {
			if failed >= 0 {
				return // two fail: no single filter hid it
			}
			failed = i
		}
	}
	files := views(c, s.lines)
	switch {
	case failed >= 0:
		s.creditFilter(repo, c, files, failed)
	case s.matches(repo, c, files):
		if s.plan.OnPage(s.counted) {
			s.emit(s.result(repo, c, files))
		}
		s.counted++
	default:
		s.countHidden(repo, c, files)
	}
}

// topLevelCommitLeaf evaluates a top-level conjunct that depends on the
// commit alone, possibly negated. ok is false for any other conjunct.
func topLevelCommitLeaf(repo *Repo, c *Commit, kid query.Pred) (matched, ok bool) {
	if not, negated := kid.(*query.Not); negated {
		matched, ok = commitLeaf(repo, c, not.Kid)
		return !matched, ok
	}
	return commitLeaf(repo, c, kid)
}

// candidates narrows a segment's commits with its trigram indexes: text
// terms through the changed lines and the messages, msg: through the
// messages alone.
func (s *searcher) candidates(seg *segment) []uint32 {
	ids := engine.Narrow(s.plan.Pred, func(leaf query.Pred) []uint32 {
		switch leaf := leaf.(type) {
		case *query.Content:
			literal := engine.ContentLiteral(leaf)
			inDiffs := seg.diffs.Candidates(literal, s.plan.CaseSensitive)
			inMessages := seg.messages.Candidates(literal, s.plan.CaseSensitive)
			if inDiffs == nil || inMessages == nil {
				return nil
			}
			return engine.Union(inDiffs, inMessages)
		case *query.Message:
			return seg.messages.Candidates(query.RequiredLiteral(leaf.Re), s.plan.CaseSensitive)
		default:
			return nil
		}
	})
	if ids != nil {
		return ids
	}
	return engine.AllIDs(len(seg.commits))
}

// fileView is one changed file, as the predicate's leaves see it. A commit
// without files (an empty commit) is seen as one file with no path.
type fileView struct {
	file *FileChange
	termLines
	message *messageView // the commit's message, shared by its files
}

// messageView is a commit's message, as text terms see it: the subject on
// line 1 and the body after it.
type messageView struct{ termLines }

// termLines is a text, a file's changed lines or a message, and which of
// its lines each text term matches, worked out once per term.
type termLines struct {
	text    []byte
	finder  *trigram.LineFinder
	matches map[*query.Content][]int
}

func newTermLines(text []byte, finder *trigram.LineFinder) termLines {
	return termLines{text: text, finder: finder, matches: map[*query.Content][]int{}}
}

// views returns a commit's files for matching.
func views(c *Commit, finder *trigram.LineFinder) []*fileView {
	message := newMessageView(c, finder)
	if len(c.Files) == 0 {
		return []*fileView{newFileView(&FileChange{}, finder, message)}
	}
	out := make([]*fileView, len(c.Files))
	for i := range c.Files {
		out[i] = newFileView(&c.Files[i], finder, message)
	}
	return out
}

func newFileView(file *FileChange, finder *trigram.LineFinder, message *messageView) *fileView {
	return &fileView{file: file, termLines: newTermLines(file.Text, finder), message: message}
}

func newMessageView(c *Commit, finder *trigram.LineFinder) *messageView {
	return &messageView{newTermLines([]byte(c.message()), finder)}
}

// leafFor evaluates predicate leaves against one commit and one of its files.
func leafFor(repo *Repo, c *Commit, view *fileView) func(query.Pred) bool {
	return func(p query.Pred) bool {
		switch p := p.(type) {
		case *query.Content:
			return len(view.lines(p)) > 0 || len(view.message.lines(p)) > 0
		case *query.Path:
			return p.Re.MatchString(view.file.Path)
		case *query.Lang:
			return view.file.Path != "" && lang.Detect(view.file.Path, nil) == p.Name
		default:
			matched, _ := commitLeaf(repo, c, p)
			return matched
		}
	}
}

// commitLeaf evaluates a leaf that depends on the commit alone, not on one
// of its files. ok is false for any other leaf.
func commitLeaf(repo *Repo, c *Commit, p query.Pred) (matched, ok bool) {
	switch p := p.(type) {
	case *query.Author:
		return authorMatches(p, c), true
	case *query.Message:
		return p.Re.MatchString(c.message()), true
	case *query.Since:
		return !c.At.Before(p.After), true
	case *query.Repo:
		return p.Re.MatchString(repo.Name), true
	default:
		return false, false // sym: is a working-tree operator; the parser keeps it out
	}
}

// lines returns which lines of the text term matches, counted from 1 (for
// a file, in diff order).
func (t *termLines) lines(term *query.Content) []int {
	if found, ok := t.matches[term]; ok {
		return found
	}
	found := t.finder.Lines(t.text, term)
	t.matches[term] = found
	return found
}

// authorMatches compares author: with a commit's author, after .mailmap.
func authorMatches(a *query.Author, c *Commit) bool {
	name := strings.ToLower(c.AuthorName)
	if a.Exact {
		return name == a.Fragment
	}
	return strings.Contains(name, a.Fragment) || strings.Contains(strings.ToLower(c.AuthorEmail), a.Fragment)
}

// matches reports whether one of the commit's files satisfies the plan.
func (s *searcher) matches(repo *Repo, c *Commit, views []*fileView) bool {
	for _, view := range views {
		if query.Eval(s.plan.Pred, leafFor(repo, c, view)) {
			return true
		}
	}
	return false
}

// result builds the result of a commit that matches: the files that
// satisfy the plan, and what matched in their diffs and in the message.
func (s *searcher) result(repo *Repo, c *Commit, views []*fileView) protocol.ResultItem {
	var files []protocol.FileStat
	diffHits := 0
	terms := map[int]bool{}
	var contributing []*query.Content
	for _, view := range views {
		leaf := leafFor(repo, c, view)
		if !query.Eval(s.plan.Pred, leaf) {
			continue
		}
		if view.file.Path != "" {
			files = append(files, protocol.FileStat{Path: view.file.Path, Added: view.file.Added, Removed: view.file.Removed})
		}
		matched := map[int]bool{}
		for _, term := range query.Contributing(s.plan.Pred, leaf) {
			terms[term.TermIndex] = true
			contributing = append(contributing, term)
			for _, line := range view.lines(term) {
				matched[line] = true
			}
		}
		diffHits += len(matched)
	}
	matchedTerms := make([]int, 0, len(terms))
	for index := range terms {
		matchedTerms = append(matchedTerms, index)
	}
	slices.Sort(matchedTerms)
	if len(files) > maxResultFiles {
		files = files[:maxResultFiles]
	}
	inMessage, bodyLine := messageMatch(views[0].message, contributing)
	return protocol.ResultItem{
		Kind: query.KindCommit, Ref: Ref{PlanID: s.planID, RepoID: repo.ID, SHA: c.SHA}.String(), RepoID: repo.ID,
		SHA: c.SHA, Subject: c.Subject, Author: protocol.Person{Name: c.AuthorName, Email: c.AuthorEmail},
		At: c.At.UTC().Format(time.RFC3339), Files: files, DiffHits: diffHits, MatchedTerms: matchedTerms,
		SubjectHits: messageHits(s.plan, c.Subject), InMessage: inMessage, BodyLine: bodyLine,
	}
}

// messageMatch reports whether terms match the commit's message, and when
// none matches the subject, the first body line one matches, marked.
func messageMatch(message *messageView, terms []*query.Content) (inMessage bool, bodyLine *protocol.MessageLine) {
	first := 0 // the first matched message line; the subject is line 1
	for _, term := range terms {
		for _, line := range message.lines(term) {
			if first == 0 || line < first {
				first = line
			}
		}
	}
	if first <= 1 {
		return first == 1, nil
	}
	text, hits := trigram.PreviewLine(strings.Split(string(message.text), "\n")[first-1], terms)
	ranges := make([]protocol.Range, len(hits))
	for i, hit := range hits {
		ranges[i] = protocol.Range{Start: hit.Start, End: hit.End}
	}
	return true, &protocol.MessageLine{Text: text, Hits: ranges}
}

// messageHits marks what msg: terms, and the query's text terms, match in a
// commit's subject or body. A nil plan marks nothing.
func messageHits(plan *query.Plan, text string) []protocol.Range {
	ranges := []protocol.Range{}
	if plan == nil {
		return ranges
	}
	mark := func(re interface{ FindAllStringIndex(string, int) [][]int }) {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			if loc[1] > loc[0] {
				ranges = append(ranges, trigram.UTF16Range(text, loc[0], loc[1]))
			}
		}
	}
	for _, term := range plan.Terms {
		mark(term.Re)
	}
	query.VisitPositive(plan.Pred, func(m *query.Message) { mark(m.Re) })
	slices.SortFunc(ranges, func(a, b protocol.Range) int { return a.Start - b.Start })
	return ranges
}

// countHidden credits a commit that doesn't match to the first top-level
// filter that alone removed it ("3 commits hidden by -f:vendor/").
func (s *searcher) countHidden(repo *Repo, c *Commit, views []*fileView) {
	for _, f := range s.plan.Filters {
		if s.creditFilter(repo, c, views, f.Index) {
			return
		}
	}
}

// creditFilter counts the commit as hidden by the top-level conjunct at
// index if that conjunct is a filter and the commit matches without it.
func (s *searcher) creditFilter(repo *Repo, c *Commit, views []*fileView, index int) bool {
	if !slices.ContainsFunc(s.plan.Filters, func(f query.Filter) bool { return f.Index == index }) {
		return false
	}
	without := &query.And{Kids: slices.Delete(slices.Clone(s.plan.Pred.Kids), index, index+1)}
	for _, view := range views {
		if query.Eval(without, leafFor(repo, c, view)) {
			s.hidden[index]++
			return true
		}
	}
	return false
}

// ------------------------------------------------------------ refs

// refPrefix starts every history ref; working-tree refs use another.
const refPrefix = "hist"

// Ref locates one commit result. Its string form is the opaque ref handed
// to clients.
type Ref struct {
	// PlanID names the search that produced the result, so its preview can
	// mark the query's terms and the files its filters hid.
	PlanID int
	RepoID string
	SHA    string
}

// String encodes the ref.
func (r Ref) String() string {
	return strings.Join([]string{refPrefix, strconv.Itoa(r.PlanID), r.RepoID, r.SHA}, "|")
}

// ParseRef decodes a ref built by String. ok is false for anything else,
// including a sha that isn't hexadecimal (it is passed to git).
func ParseRef(text string) (ref Ref, ok bool) {
	parts := strings.Split(text, "|")
	if len(parts) != 4 || parts[0] != refPrefix || parts[2] == "" || !isHex(parts[3]) {
		return Ref{}, false
	}
	planID, err := strconv.Atoi(parts[1])
	if err != nil || planID < 0 {
		return Ref{}, false
	}
	return Ref{PlanID: planID, RepoID: parts[2], SHA: parts[3]}, true
}

// isHex reports whether text is a plausible commit sha.
func isHex(text string) bool {
	if len(text) < 7 || len(text) > 64 {
		return false
	}
	for _, r := range text {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
