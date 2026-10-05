package trigram

// Fuzzy file names: a bare word also finds a file whose name has its
// characters in order, the way Quick Open does, so usrsvc finds
// UserService.ts and rtrypol finds retry_policy.py. Only the file-name
// section matches this way; code lines stay exact.
//
// The match must start where a word of the name starts, which keeps a word
// from matching letters scattered through unrelated names. A word without
// a / matches the file's name; one with a / matches its whole path, so
// pay/rtry finds payments/retry_policy.py. Trigrams can't narrow a
// subsequence, but every path is in memory, and one pass over a name
// decides whether it matches; only names that do are scored, to rank and
// highlight them.

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// What a fuzzy match scores per matched character. Characters at the start
// of a word, or right after the previous one, make a better match; each
// character skipped between two matches costs fuzzyGap.
const (
	fuzzyChar        = 1
	fuzzyWordStart   = 8
	fuzzyConsecutive = 4
	fuzzyGap         = 1
)

// fuzzyTarget is the part of filePath a fuzzy term matches, and where it
// starts: the file's name, or the whole path for a term with a /.
func fuzzyTarget(term *query.Content, filePath string) (string, int) {
	if strings.Contains(term.Literal, "/") {
		return filePath, 0
	}
	name := path.Base(filePath)
	return name, len(filePath) - len(name)
}

// foldFor is how a term compares characters: lower-cased when it ignores case.
func foldFor(term *query.Content) func(rune) rune {
	if term.IgnoreCase {
		return unicode.ToLower
	}
	return func(r rune) rune { return r }
}

// fuzzyMatches reports whether a fuzzy term matches a file's path: its
// first character starts a word of the name and the rest follow in order.
// The earliest such start leaves the most room for the rest, so one pass
// decides it, without allocating.
func fuzzyMatches(term *query.Content, filePath string) bool {
	if !term.Fuzzy || term.Literal == "" {
		return false
	}
	target, _ := fuzzyTarget(term, filePath)
	fold := foldFor(term)
	want := term.Literal
	prev, started := rune(-1), false
	for _, r := range target {
		next, size := utf8.DecodeRuneInString(want)
		if fold(r) == fold(next) && (started || wordStartsAfter(prev, r)) {
			started = true
			if want = want[size:]; want == "" {
				return true
			}
		}
		prev = r
	}
	return false
}

// fuzzyMatch matches a fuzzy term against a file's path at its best. It
// returns the byte offsets in filePath of the characters that matched, the
// match's score, and whether the term matched at all.
func fuzzyMatch(term *query.Content, filePath string) (offsets []int, score int, ok bool) {
	if !fuzzyMatches(term, filePath) {
		return nil, 0, false
	}
	target, start := fuzzyTarget(term, filePath)
	fold := foldFor(term)
	want := []rune(term.Literal)
	for i, r := range want {
		want[i] = fold(r)
	}
	name := make([]rune, 0, len(target))
	at := make([]int, 0, len(target)) // the byte offset of each rune of name in target
	for i, r := range target {
		name = append(name, r)
		at = append(at, i)
	}
	matched, score := bestAlignment(want, name, fold)
	offsets = make([]int, len(matched))
	for i, j := range matched {
		offsets[i] = start + at[j]
	}
	return offsets, score, true
}

// bestAlignment finds where in name each rune of want best matches, in
// order, with the first at the start of a word; fuzzyMatches has already
// found that one exists. It returns the index in name of each matched rune
// and the match's score.
//
// best[i·n+j] is the best score of matching want[:i+1] with want[i] at
// name[j]. A gap from the previous match at k costs (j-k-1)·fuzzyGap, so
// the best previous match is the running maximum of best[(i-1)·n+k] +
// k·fuzzyGap, which keeps the search to len(want)·len(name) steps.
func bestAlignment(want, name []rune, fold func(rune) rune) ([]int, int) {
	m, n := len(want), len(name)
	best := make([]int, m*n)
	from := make([]int, m*n) // the index of the previous match, for the way back
	for j := range n {
		best[j] = noAlignment
		if fold(name[j]) == want[0] && wordStartsAt(name, j) {
			best[j] = fuzzyChar + fuzzyWordStart
		}
	}
	for i := 1; i < m; i++ {
		alignNext(want[i], name, fold, best[(i-1)*n:i*n], best[i*n:(i+1)*n], from[i*n:(i+1)*n])
	}
	end, score := -1, noAlignment
	for j, s := range best[(m-1)*n:] {
		if s > score {
			end, score = j, s
		}
	}
	matched := make([]int, m)
	for i := m - 1; i >= 0; i-- {
		matched[i] = end
		end = from[i*n+end]
	}
	return matched, score
}

// noAlignment marks a place in bestAlignment's table the term can't reach.
const noAlignment = -1 << 30

// alignNext fills one row of bestAlignment's table: the best score of
// matching r at each index of name, given the previous rune's row prev,
// and in from the previous rune's index for each.
func alignNext(r rune, name []rune, fold func(rune) rune, prev, row, from []int) {
	row[0] = noAlignment
	runMax, runAt := noAlignment, -1 // the best prev[k] + k·fuzzyGap for k < j-1
	for j := 1; j < len(name); j++ {
		row[j] = noAlignment
		if k := j - 2; k >= 0 && prev[k] != noAlignment && prev[k]+k*fuzzyGap > runMax {
			runMax, runAt = prev[k]+k*fuzzyGap, k
		}
		if fold(name[j]) != r {
			continue
		}
		char := fuzzyChar
		if wordStartsAt(name, j) {
			char += fuzzyWordStart
		}
		if prev[j-1] != noAlignment {
			row[j], from[j] = prev[j-1]+char+fuzzyConsecutive, j-1
		}
		if gapped := runMax - (j-1)*fuzzyGap + char; runAt >= 0 && gapped > row[j] {
			row[j], from[j] = gapped, runAt
		}
	}
}

// wordStartsAt reports whether a word of name starts at name[j].
func wordStartsAt(name []rune, j int) bool {
	if j == 0 {
		return true
	}
	return wordStartsAfter(name[j-1], name[j])
}

// wordStartsAfter reports whether a word starts at cur when prev comes
// before it (-1 at the start of a name): after a character that isn't a
// letter or digit, at a capital after a lower-case letter, or at a digit
// after a letter.
func wordStartsAfter(prev, cur rune) bool {
	switch {
	case prev < 0 || !unicode.IsLetter(prev) && !unicode.IsDigit(prev):
		return true
	case unicode.IsUpper(cur) && unicode.IsLower(prev):
		return true
	default:
		return unicode.IsDigit(cur) && unicode.IsLetter(prev)
	}
}

// fuzzyRanges turns matched byte offsets into the ranges to highlight,
// joining characters that are next to each other.
func fuzzyRanges(s string, offsets []int) [][]int {
	var ranges [][]int
	for _, start := range offsets {
		_, size := utf8.DecodeRuneInString(s[start:])
		if n := len(ranges); n > 0 && ranges[n-1][1] == start {
			ranges[n-1][1] = start + size
			continue
		}
		ranges = append(ranges, []int{start, start + size})
	}
	return ranges
}

// fuzzyNameScore is the best score of a fuzzy term that matches a file's
// name only fuzzily, or 0 when none does.
func (s *searcher) fuzzyNameScore(filePath string) int {
	best := 0
	for _, term := range s.rankTerms {
		if term.MatchString(filePath) {
			continue
		}
		if _, score, ok := fuzzyMatch(term, filePath); ok {
			best = max(best, score)
		}
	}
	return best
}
