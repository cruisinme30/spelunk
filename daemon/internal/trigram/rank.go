package trigram

// Best-match order (order:best): files that define a searched name, are
// named after it or are open in the editor come first, and tests, vendored and generated files last.
// Recently-changed order (order:recent) sorts by when each file last
// changed instead, with best match breaking ties (see compareChanged).
// Ranking moves whole files: a file's lines stay together, in line order.
//
// It works in two steps so results still stream and pages never overlap:
//
//  1. Before any line is read, the candidate files are sorted by what is
//     known about them up front (rankDoc). The scan then visits them in that
//     order, so the Nth result is the same whichever page asks for it.
//  2. How well the lines match (whole words, exact case) is only known after
//     the scan, so it reorders files within the requested page, never across
//     pages: the page is held until it is full, then sent sorted. A page that
//     takes longer than rankWindow to fill is sent as it stands.

import (
	"path"
	"slices"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// What each signal adds to a file's score. A definition outranks a file
// name, which outranks recency. Demotions are tiers: tests sink below every
// other file, vendored and generated files below tests, and files VS
// Code's excludes hide (shown by excludes:no) below those, whatever else
// they have going for them.
const (
	scoreDefinition  = 100
	scoreNameIsTerm  = 40 // retry.py for retry
	scoreNameHasTerm = 20 // retry_policy.py for retry
	scorePathHasTerm = 5  // retry/client.py for retry
	scoreNameFuzzy   = 1  // retry_policy.py for rtrypol, in the file-name section only
	scoreOpen        = 30 // open in the editor
	scoreUncommitted = 15
	scoreRecent      = 10
	scoreTest        = -1000
	scoreVendored    = -2000
	scoreGenerated   = -2000
	scoreExcluded    = -3000 // shown by excludes:no, below everything else

	// Each matched line adds up to wholeWordLine + exactCaseLine, and a file
	// at most maxLineQuality: enough to reorder files that are otherwise
	// alike, never enough to lift one past a definition or a file name.
	wholeWordLine  = 3
	exactCaseLine  = 1
	maxLineQuality = 15
)

const (
	// recentWindow is how recently a file's last commit must be to count as
	// recently changed.
	recentWindow = 14 * 24 * time.Hour
	// rankWindow is the longest the first page is held to sort it by match
	// quality, well inside the first-result budget.
	rankWindow = 50 * time.Millisecond
)

// docRank is what is known about a file before its lines are read: its
// score, and the reason the panel shows for where it landed. fuzzy is how
// well a fuzzy term matched its name, which orders file names that score
// the same.
type docRank struct {
	score  int
	fuzzy  int
	reason protocol.RankReason
	// changed is when the file last changed, for order:recent (see
	// changeOf); the zero time when that isn't known.
	changed time.Time
}

// ranking reports whether this search sorts by best match or by when files
// changed. A count-only search (Limit 0) emits nothing, so it never sorts.
func (s *searcher) ranking() bool {
	return s.plan.Order != protocol.ResultOrderPath && s.plan.Limit > 0
}

// byChange reports whether this search sorts by when files last changed.
func (s *searcher) byChange() bool {
	return s.plan.Order == protocol.ResultOrderRecent
}

// changeOf is when a file last changed, for order:recent: when it was
// saved if it has uncommitted changes or is outside Git, otherwise its
// newest commit; the zero time when it has no commit in the history window.
func changeOf(repo *Repo, doc *Doc) time.Time {
	if at, uncommitted := repo.LastChanged(doc); !uncommitted {
		return at
	}
	return doc.ModTime
}

// rankDoc scores a file by what is known before reading its lines. In
// order:recent it also notes when the file changed, and neither demotes
// tests, vendored and generated files nor gives a reason: the newest file
// comes first whatever it is.
func (s *searcher) rankDoc(repo *Repo, doc *Doc) docRank {
	var r docRank
	if s.byChange() {
		r.changed = changeOf(repo, doc)
		r.score = s.nameScore(doc.Path)
		if s.defines(doc) {
			r.score += scoreDefinition
		}
		return r
	}
	if s.defines(doc) {
		r.score += scoreDefinition
		r.reason = protocol.RankReasonDefinition
	}
	r.score += s.nameScore(doc.Path)
	if repo.Open[doc.Path] {
		r.score += scoreOpen
		if r.reason == "" {
			r.reason = protocol.RankReasonOpen
		}
	}
	if repo.History != nil {
		if repo.History.Dirty(doc.Path) {
			r.score += scoreUncommitted
		} else if _, at, ok := repo.History.LastCommit(doc.Path); ok && s.started.Sub(at) < recentWindow {
			r.score += scoreRecent
		}
	}
	switch {
	case s.plan.ShownFilter != nil && s.plan.Excludes.Hiding(repo.ID, repo.Root, doc.Path) != "":
		r.score += scoreExcluded
		r.reason = protocol.RankReasonExcluded
	case s.plan.FileKinds.IsGenerated(doc.Path, doc.Content):
		r.score += scoreGenerated
		r.reason = protocol.RankReasonGenerated
	case s.plan.FileKinds.IsVendored(doc.Path):
		r.score += scoreVendored
		r.reason = protocol.RankReasonVendored
	case lang.IsTest(doc.Path):
		r.score += scoreTest
		r.reason = protocol.RankReasonTest
	}
	return r
}

// defines reports whether one of the file's definitions is named exactly
// what a searched term matches: RetryPolicy for RetryPolicy, not for Retry.
func (s *searcher) defines(doc *Doc) bool {
	for i := range doc.Symbols {
		name := doc.Symbols[i].Name
		for _, term := range s.rankTerms {
			if term.Literal != "" {
				if len(name) == len(term.Literal) && (strings.EqualFold(name, term.Literal) && term.IgnoreCase || name == term.Literal) {
					return true
				}
			} else if term.IsDefinitionOf(name) {
				return true
			}
		}
	}
	return false
}

// nameScore scores how a file's path matches the searched terms (not
// content: ones): its name
// (without the extension) is a term, has one as a word, or the term is
// elsewhere in the path.
func (s *searcher) nameScore(filePath string) int {
	base := path.Base(filePath)
	stem := strings.TrimSuffix(base, path.Ext(base))
	best := 0
	for _, term := range s.rankTerms {
		if term.ContentOnly {
			continue // content: never matches names
		}
		score := 0
		if loc := term.FindStringIndex(stem); len(loc) == 2 {
			switch {
			case loc[0] == 0 && loc[1] == len(stem):
				score = scoreNameIsTerm
			case nameWordAt(stem, loc[0], loc[1]):
				score = scoreNameHasTerm
			default:
				score = scorePathHasTerm
			}
		} else if term.MatchString(filePath) {
			score = scorePathHasTerm
		}
		best = max(best, score)
	}
	return best
}

// nameWordAt reports whether name[start:end] is a whole word of a file
// name, where _, -, . and a change to upper case separate words:
// retry_policy, retry-policy and RetryPolicy all have the word retry.
func nameWordAt(name string, start, end int) bool {
	before := start == 0 || !isLetterOrDigit(name[start-1]) || isUpper(name[start])
	after := end == len(name) || !isLetterOrDigit(name[end]) || isUpper(name[end])
	return before && after
}

func isLetterOrDigit(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }

func isWordByte(b byte) bool { return isLetterOrDigit(b) || b == '_' }

// lineQuality scores how well a matched line matches: a hit that is a
// whole word, and one in the searched term's own case, each count.
func (s *searcher) lineQuality(line matchedLine) int {
	quality := 0
	for _, hit := range line.hits {
		if (hit.start == 0 || !isWordByte(line.text[hit.start-1])) &&
			(hit.end == len(line.text) || !isWordByte(line.text[hit.end])) {
			quality += wholeWordLine
			break
		}
	}
	for _, hit := range line.hits {
		if literal := s.literals[hit.termIndex]; literal != "" && line.text[hit.start:hit.end] == literal {
			quality += exactCaseLine
			break
		}
	}
	return quality
}

// rankedCandidate is a file the trigram index says may match, with its
// up-front rank.
type rankedCandidate struct {
	repo *Repo
	doc  *Doc
	rank docRank
}

// compareChanged orders two files for order:recent: newest first when
// descending, oldest first otherwise. A file with no known change goes last
// either way, its date being unknown rather than old.
func compareChanged(a, b time.Time, descending bool) int {
	switch {
	case a.IsZero() || b.IsZero():
		return boolOrder(a.IsZero()) - boolOrder(b.IsZero())
	case descending:
		return b.Compare(a)
	default:
		return a.Compare(b)
	}
}

func boolOrder(b bool) int {
	if b {
		return 1
	}
	return 0
}

// sortByRank puts candidates best first, or in order:recent by when they
// changed with best match breaking ties. The sort is stable, so files that
// rank the same keep path order and every page sees the same order.
func (s *searcher) sortByRank(candidates []rankedCandidate) {
	byChange := s.byChange()
	slices.SortStableFunc(candidates, func(a, b rankedCandidate) int {
		if byChange {
			if c := compareChanged(a.rank.changed, b.rank.changed, s.plan.Descending); c != 0 {
				return c
			}
		}
		if a.rank.score != b.rank.score {
			return b.rank.score - a.rank.score
		}
		return b.rank.fuzzy - a.rank.fuzzy
	})
}

// pageGroup is one file's results on the held page.
type pageGroup struct {
	section int // definitions, file names, code: groups never leave their section
	key     string
	score   int
	quality int
	items   []protocol.ResultItem
}

// sectionOf orders a result kind's section as the panel shows it.
func sectionOf(kind string) int {
	switch kind {
	case query.KindSymbol:
		return 0
	case query.KindFile:
		return 1
	default:
		return 2
	}
}

// hold adds a result of the page to its file's group.
func (s *searcher) hold(item protocol.ResultItem, score, quality int) {
	key := item.Kind + "\x00" + item.RepoID + "\x00" + item.Path
	if n := len(s.held); n > 0 && s.held[n-1].key == key {
		group := s.held[n-1]
		group.quality += quality
		group.items = append(group.items, item)
		return
	}
	s.held = append(s.held, &pageGroup{
		section: sectionOf(item.Kind), key: key, score: score, quality: quality, items: []protocol.ResultItem{item},
	})
}

// sendHeld sends the held page, best first within each section, and stops
// holding: the rest of the search emits as it goes. In order:recent the
// files keep the order they were found in, by when they changed: how well
// their lines match never moves one past a newer file.
func (s *searcher) sendHeld() {
	s.holding = false
	byChange := s.byChange()
	slices.SortStableFunc(s.held, func(a, b *pageGroup) int {
		if a.section != b.section {
			return a.section - b.section
		}
		if byChange {
			return 0
		}
		return b.score + min(b.quality, maxLineQuality) - a.score - min(a.quality, maxLineQuality)
	})
	for _, group := range s.held {
		for i := range group.items {
			s.emit(group.items[i])
		}
	}
	s.held = nil
}
