package query

// source is the query text indexed by rune, with each rune's UTF-16 offset,
// so the lexer can work in runes while spans are reported in UTF-16.
type source struct {
	text    string
	runes   []rune
	offsets []int // offsets[i] is the UTF-16 offset of runes[i]; offsets[len(runes)] is the total length
}

func newSource(text string) *source {
	runes := []rune(text)
	offsets := make([]int, len(runes)+1)
	offset := 0
	for i, r := range runes {
		offsets[i] = offset
		offset += utf16Width(r)
	}
	offsets[len(runes)] = offset
	return &source{text: text, runes: runes, offsets: offsets}
}

// utf16Width is how many UTF-16 units r takes: two above the Basic
// Multilingual Plane (a surrogate pair), one otherwise.
func utf16Width(r rune) int {
	if r >= 0x10000 && r <= 0x10FFFF {
		return 2
	}
	return 1
}

// length is the text's length in UTF-16 units.
func (s *source) length() int { return s.offsets[len(s.runes)] }

// slice returns the text between two UTF-16 offsets.
func (s *source) slice(start, end int) string {
	return string(s.runes[s.runeIndex(start):s.runeIndex(end)])
}

// runeIndex converts a UTF-16 offset to a rune index, clamped to the text.
func (s *source) runeIndex(offset int) int {
	for i, o := range s.offsets {
		if o >= offset {
			return i
		}
	}
	return len(s.runes)
}

// utf16Len is the length of text in UTF-16 units.
func utf16Len(text string) int { return newSource(text).length() }
