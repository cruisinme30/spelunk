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
	// folded means needle is lowercase ASCII and is searched for in the
	// file's ASCII-lowercased text.
	folded bool
}

// newLiteralFinder returns a finder for term, or ok false when the regex
// must be used instead.
func newLiteralFinder(term *query.Content) (literalFinder, bool) {
	if term.Literal == "" {
		return literalFinder{}, false
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
// start, or -1. lowered is content with ASCII letters lowercased, needed
// when the finder is folded.
func (f literalFinder) index(content, lowered []byte, from int) int {
	text := content
	if f.folded {
		text = lowered
	}
	i := bytes.Index(text[from:], f.needle)
	if i < 0 {
		return -1
	}
	return from + i
}

// lowerASCII returns a copy of content with ASCII letters lowercased.
func lowerASCII(content []byte) []byte {
	lowered := make([]byte, len(content))
	for i, b := range content {
		lowered[i] = foldASCII(b)
	}
	return lowered
}
