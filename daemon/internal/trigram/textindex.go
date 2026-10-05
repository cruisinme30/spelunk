package trigram

import "github.com/cruisinme30/spelunk/daemon/internal/query"

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
