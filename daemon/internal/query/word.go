package query

import (
	"regexp"
	"regexp/syntax"
	"unicode"
	"unicode/utf8"
)

// This file is how a text term matches under word:yes. RE2 has no
// lookaround, and \b would wrap a term like "foo." or "->" wrongly, so the
// regex stays as written and each match is checked at its edges instead.

// FindAllIndex returns up to n matches of the term in b (all of them when n
// is negative), as regexp.FindAllIndex does, keeping only whole words when
// the term has WholeWord.
func (c *Content) FindAllIndex(b []byte, n int) [][]int {
	if !c.WholeWord {
		return c.Re.FindAllIndex(b, n)
	}
	if !c.resumable {
		return keepWholeWords(b, c.Re.FindAllIndex(b, -1), n)
	}
	var found [][]int
	for from := 0; from <= len(b) && (n < 0 || len(found) < n); {
		loc := c.Re.FindIndex(b[from:])
		if loc == nil {
			break
		}
		start, end := from+loc[0], from+loc[1]
		if !isWholeWord(b, start, end) {
			// A whole word may start inside this match: "a-a" in "ba-a-a".
			from = start + runeLen(b, start)
			continue
		}
		found = append(found, []int{start, end})
		from = end
		if end == start {
			from += runeLen(b, start)
		}
	}
	return found
}

// FindAllStringIndex is FindAllIndex for a string.
func (c *Content) FindAllStringIndex(s string, n int) [][]int {
	if !c.WholeWord {
		return c.Re.FindAllStringIndex(s, n)
	}
	return c.FindAllIndex([]byte(s), n)
}

// FindStringIndex returns the first match of the term in s, or nil.
func (c *Content) FindStringIndex(s string) []int {
	if !c.WholeWord {
		return c.Re.FindStringIndex(s)
	}
	if found := c.FindAllIndex([]byte(s), 1); len(found) == 1 {
		return found[0]
	}
	return nil
}

// MatchString reports whether the term matches s.
func (c *Content) MatchString(s string) bool {
	return c.FindStringIndex(s) != nil
}

// keepWholeWords keeps up to n of locs (all when n is negative) that are
// whole words of b.
func keepWholeWords(b []byte, locs [][]int, n int) [][]int {
	var kept [][]int
	for _, loc := range locs {
		if n >= 0 && len(kept) == n {
			break
		}
		if isWholeWord(b, loc[0], loc[1]) {
			kept = append(kept, loc)
		}
	}
	return kept
}

// isWholeWord reports whether b[start:end] is a whole word, as VS Code's
// Match Whole Word decides: each edge of the match must sit between a word
// character and something that isn't one. An edge where the match itself
// begins or ends with a non-word character always qualifies, so "foo." and
// "->" can be searched as words.
func isWholeWord(b []byte, start, end int) bool {
	if start > 0 {
		before, _ := utf8.DecodeLastRune(b[:start])
		first, _ := utf8.DecodeRune(b[start:])
		if isWordRune(before) && (start == end || isWordRune(first)) {
			return false
		}
	}
	if end < len(b) {
		after, _ := utf8.DecodeRune(b[end:])
		last, _ := utf8.DecodeLastRune(b[:end])
		if isWordRune(after) && (start == end || isWordRune(last)) {
			return false
		}
	}
	return true
}

// isWordRune reports whether r is part of a word: a letter, a digit or _.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// runeLen is the length of the rune at b[i:], at least 1.
func runeLen(b []byte, i int) int {
	if i >= len(b) {
		return 1
	}
	_, size := utf8.DecodeRune(b[i:])
	return size
}

// canResume reports whether re matches b[i:] exactly as it matches b from
// offset i: true unless it has an assertion (^, $, \b …) that would read the
// cut as the start of the text.
func canResume(re *regexp.Regexp) bool {
	parsed, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return false
	}
	var visit func(*syntax.Regexp) bool
	visit = func(r *syntax.Regexp) bool {
		switch r.Op { //nolint:exhaustive // only empty-width assertions matter; every other op recurses below
		case syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText,
			syntax.OpWordBoundary, syntax.OpNoWordBoundary:
			return false
		}
		for _, sub := range r.Sub {
			if !visit(sub) {
				return false
			}
		}
		return true
	}
	return visit(parsed)
}
