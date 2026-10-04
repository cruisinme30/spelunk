package trigram

import "github.com/cruisinme30/unified-search/daemon/internal/query"

// TextIndex is a trigram index over numbered texts with no files behind
// them. The history index keeps one over each commit's changed lines and
// one over each commit's message. It is immutable once built.
type TextIndex struct {
	postings map[uint32][]uint32
	size     int
}

// NewTextIndex indexes texts; each text's id is its position in texts.
func NewTextIndex(texts [][]byte) *TextIndex {
	ix := &TextIndex{postings: map[uint32][]uint32{}, size: len(texts)}
	for i, text := range texts {
		addTrigrams(ix.postings, uint32(i), text) //nolint:gosec // G115: callers index far fewer than 2^32 texts
	}
	return ix
}

// Candidates returns the ids of the texts that may contain literal, or nil
// when every text may. See Shard.Candidates for how case is handled.
func (ix *TextIndex) Candidates(literal string, caseSensitive bool) []uint32 {
	return candidates(ix.postings, literal, caseSensitive)
}

// Size is how many texts the index holds.
func (ix *TextIndex) Size() int { return ix.size }

// Narrow returns the ids that can satisfy p in ascending order, or nil for
// "any id". leaf narrows one leaf predicate, returning nil when it can't
// (paths, authors and the like). AND intersects, OR unites, and NOT never
// narrows: the ids that fail its kid are not known.
func Narrow(p query.Pred, leaf func(query.Pred) []uint32) []uint32 {
	switch p := p.(type) {
	case *query.And:
		var result []uint32
		narrowed := false
		for _, kid := range p.Kids {
			ids := Narrow(kid, leaf)
			switch {
			case ids == nil:
				continue
			case !narrowed:
				result, narrowed = ids, true
			default:
				result = intersect(result, ids)
			}
		}
		return result
	case *query.Or:
		result := []uint32{}
		for _, kid := range p.Kids {
			ids := Narrow(kid, leaf)
			if ids == nil {
				return nil
			}
			result = union(result, ids)
		}
		return result
	case *query.Not:
		return nil
	default:
		return leaf(p)
	}
}

// ContentLiteral is the literal every match of a text term contains, the
// one to narrow candidates with: the term itself, or what its regex requires.
func ContentLiteral(term *query.Content) string {
	if term.Literal != "" {
		return term.Literal
	}
	return query.RequiredLiteral(term.Re)
}

// LineFinder finds the lines of texts that text terms match, exactly as
// searches match the lines of files. Share one across a search's texts:
// what it works out about each term is kept. It is not safe for
// concurrent use.
type LineFinder struct{ terms termCache }

// NewLineFinder returns a LineFinder for one search.
func NewLineFinder() *LineFinder { return &LineFinder{terms: termCache{}} }

// Lines returns the 1-based numbers of the lines of text that term matches.
func (f *LineFinder) Lines(text []byte, term *query.Content) []int {
	matched := newLineMatcher(text, f.terms).matches(term)
	numbers := make([]int, len(matched))
	for i, line := range matched {
		numbers[i] = line.number
	}
	return numbers
}
