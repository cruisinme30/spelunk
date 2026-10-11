package structural

import "bytes"

// PlaceKind is the part of a file some text is in, besides code.
type PlaceKind uint8

const (
	// Comment is a line or block comment, its markers included, or a
	// Python docstring.
	Comment PlaceKind = iota + 1
	// String is a string or character literal, its quotes included.
	String
)

// Place is a comment or a string: its kind and where it is in the text,
// end exclusive.
type Place struct {
	Kind       PlaceKind
	Start, End int
}

// IsProse reports whether language (a lang package name) is people's
// writing rather than code, plain text or Markdown, whose files have no
// comments, strings or code to tell apart.
func IsProse(language string) bool {
	return language == "text" || language == "markdown"
}

// Places lists the comments and strings of text, read as language (a lang
// package name), in order and apart. Everything else is code: all of a
// file in a language without rules for comments and strings, and of prose
// (see IsProse), which callers tell apart.
func Places(text []byte, language string) []Place {
	if IsProse(language) {
		return nil
	}
	s, ok := languages[language]
	if !ok {
		return nil
	}
	var places []Place
	l := &lexer{syntax: s, text: text, gap: true, places: &places}
	for l.pos < len(text) {
		l.next()
	}
	if language == "python" {
		markDocstrings(text, places)
	}
	return places
}

// place records a comment or a string when the lexer is collecting them.
func (l *lexer) place(kind PlaceKind, start, end int) {
	if l.places != nil && end > start {
		*l.places = append(*l.places, Place{Kind: kind, Start: start, End: end})
	}
}

// markDocstrings makes each Python string in triple quotes that starts its
// line a comment: a docstring, or a block of text set off as one, which
// people read as a comment though Python stores it as a string.
func markDocstrings(text []byte, places []Place) {
	for i := range places {
		p := &places[i]
		if p.Kind != String || !tripleQuoted(text[p.Start:p.End]) {
			continue
		}
		lineStart := bytes.LastIndexByte(text[:p.Start], '\n') + 1
		if len(bytes.TrimLeft(text[lineStart:p.Start], " \t")) == 0 {
			p.Kind = Comment
		}
	}
}

// tripleQuoted reports whether a string literal opens with three quotes.
func tripleQuoted(literal []byte) bool {
	return bytes.HasPrefix(literal, []byte(`"""`)) || bytes.HasPrefix(literal, []byte(`'''`))
}
