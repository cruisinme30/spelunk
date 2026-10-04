package trigram

import (
	"bytes"
	"unicode/utf8"

	"github.com/cruisinme30/unified-search/daemon/internal/query"
)

// A literalFinder finds a literal term in file text much faster than its
// regex can. It only proposes positions; the engine still confirms every
// line with the regex, so a finder may report extra positions but must
// never skip one where the regex matches.
type literalFinder struct {
	needle []byte
	// folded means needle is lowercase ASCII and is searched for ignoring
	// ASCII case.
	folded bool
}

// minRegexLiteral is the shortest literal of a regex worth searching for.
const minRegexLiteral = 3

// newLiteralFinder returns a finder for term, or ok false when the regex
// must be used instead.
func newLiteralFinder(term *query.Content) (literalFinder, bool) {
	if term.Literal == "" {
		return regexLiteralFinder(term)
	}
	if !term.IgnoreCase {
		return literalFinder{needle: []byte(term.Literal)}, true
	}
	needle := []byte(term.Literal)
	for i, b := range needle {
		if b >= utf8.RuneSelf {
			return literalFinder{}, false // Unicode case folding: leave it to the regex
		}
		needle[i] = foldASCII(b)
	}
	return literalFinder{needle: needle, folded: true}, true
}

// regexLiteralFinder finds the literal every match of a regex term
// contains, ignoring case: the regex may ignore case in part of itself even
// when the term doesn't, and a finder may propose extra positions. ok is
// false when the literal is short or not ASCII.
func regexLiteralFinder(term *query.Content) (literalFinder, bool) {
	literal := query.RequiredLiteral(term.Re)
	if len(literal) < minRegexLiteral {
		return literalFinder{}, false
	}
	needle := []byte(literal)
	for i, b := range needle {
		if b >= utf8.RuneSelf {
			return literalFinder{}, false
		}
		needle[i] = foldASCII(b)
	}
	return literalFinder{needle: needle, folded: true}, true
}

// The only non-ASCII characters that ignore-case matching equates with
// ASCII letters. They are written as escapes because each one looks just
// like the ASCII letter it folds to.
const (
	kelvinSign = "\u212a" // U+212A KELVIN SIGN: looks like K, folds to k
	longS      = "\u017f" // U+017F LATIN SMALL LETTER LONG S (ſ): folds to s
)

// unicodeFoldsOfASCII lists those characters as bytes, to look for in file
// text.
var unicodeFoldsOfASCII = [][]byte{[]byte(kelvinSign), []byte(longS)}

// usableOn reports whether the finder is sound for content: a folded finder
// would miss a "k" or "s" spelled with its Unicode fold.
func (f literalFinder) usableOn(content []byte) bool {
	if !f.folded || !bytes.ContainsAny(f.needle, "ks") {
		return true
	}
	for _, special := range unicodeFoldsOfASCII {
		if bytes.Contains(content, special) {
			return false
		}
	}
	return true
}

// index returns the first position at or after from where the literal may
// start, or -1.
func (f literalFinder) index(content []byte, from int) int {
	if f.folded {
		return indexFold(content, f.needle, from)
	}
	i := bytes.Index(content[from:], f.needle)
	if i < 0 {
		return -1
	}
	return from + i
}

// indexFold returns the first position at or after from where needle, which
// is lowercase ASCII, occurs in text ignoring ASCII case, or -1. It finds
// the needle's first letter in either case with bytes.IndexByte, which is
// fast, and checks the rest there; it needs no lowercased copy of text.
func indexFold(text, needle []byte, from int) int {
	last := len(text) - len(needle) // the last position the needle can start at
	if last < 0 {
		return -1
	}
	candidates := text[:last+1]
	lower, upper := needle[0], upperASCII(needle[0])
	// The next position of each case of the first letter, found but not yet
	// checked; last+1 when there is none.
	nextLower, nextUpper := -1, -1
	for from <= last {
		if nextLower < from {
			nextLower = indexByteFrom(candidates, lower, from)
		}
		at := nextLower
		if upper != lower {
			if nextUpper < from {
				nextUpper = indexByteFrom(candidates, upper, from)
			}
			at = min(at, nextUpper)
		}
		if at > last {
			return -1
		}
		if equalFoldASCII(text[at:at+len(needle)], needle) {
			return at
		}
		from = at + 1
	}
	return -1
}

// indexByteFrom returns the first position of c at or after from, or
// len(text).
func indexByteFrom(text []byte, c byte, from int) int {
	if i := bytes.IndexByte(text[from:], c); i >= 0 {
		return from + i
	}
	return len(text)
}

// equalFoldASCII reports whether text equals needle, which is lowercase
// ASCII, ignoring ASCII case.
func equalFoldASCII(text, needle []byte) bool {
	for i, b := range needle {
		if foldASCII(text[i]) != b {
			return false
		}
	}
	return true
}

// upperASCII uppercases an ASCII letter and returns any other byte as is.
func upperASCII(b byte) byte {
	if 'a' <= b && b <= 'z' {
		return b - 'a' + 'A'
	}
	return b
}
