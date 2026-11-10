package structural

import (
	"bytes"
	"strings"
)

// blockCommentEnd includes nested openers only in languages that permit
// them. An unterminated comment consumes the remaining input.
func blockCommentEnd(text []byte, pair [2]string, nested bool) int {
	if !nested {
		end := bytes.Index(text[len(pair[0]):], []byte(pair[1]))
		if end < 0 {
			return len(text)
		}
		return len(pair[0]) + end + len(pair[1])
	}
	depth := 1
	for pos := len(pair[0]); pos < len(text); {
		switch {
		case bytes.HasPrefix(text[pos:], []byte(pair[1])):
			pos += len(pair[1])
			depth--
			if depth == 0 {
				return pos
			}
		case nested && bytes.HasPrefix(text[pos:], []byte(pair[0])):
			pos += len(pair[0])
			depth++
		default:
			pos++
		}
	}
	return len(text)
}

func formattedPrefix(prefix string) bool {
	switch strings.ToLower(prefix) {
	case "f", "fr", "rf":
		return true
	default:
		return false
	}
}

func (l *lexer) formattedString(start int) bool {
	if l.pos == len(l.text) || (l.text[l.pos] != '\'' && l.text[l.pos] != '"') {
		return false
	}
	quote := l.text[l.pos]
	closer := string(quote)
	if bytes.HasPrefix(l.text[l.pos:], []byte{quote, quote, quote}) {
		closer = strings.Repeat(closer, 3)
	}
	return l.interpolated(start, l.pos+len(closer), closer, true)
}

// interpolated retains a whole string token for structural matching, but
// records only literal segments as String places. Python retains its
// separate prefix token. Expressions are read by
// the normal lexer, so their strings, comments and nested templates keep
// their own contexts. Failed attempts preserve the source position and places;
// their work still counts against the shared parsing budget.
func (l *lexer) interpolated(start, body int, closer string, python bool) bool {
	if l.interpolationWork == nil {
		remaining := len(l.text)
		l.interpolationWork = &remaining
	}
	if l.interpolationDepth >= 128 {
		*l.interpolationWork = 0
		return false
	}
	if !l.takeInterpolationWork() {
		return false
	}
	var places []Place
	trial := *l
	trial.places, trial.tokens = &places, nil
	trial.interpolationDepth++
	trial.skip(body - trial.pos)
	r := interpolationReader{lexer: &trial, closer: closer, python: python}
	if !r.literal(start) {
		return false
	}
	if l.places != nil {
		*l.places = append(*l.places, places...)
	}
	line, quote := l.line, l.pos
	l.skip(trial.pos - l.pos)
	if python && l.places == nil {
		l.emit(word, start, quote, line)
		start = quote
	}
	l.emit(str, start, l.pos, line)
	return true
}

type interpolationReader struct {
	*lexer
	closer string
	python bool
}

// literal reads a template's text through its final quote. Delimiters
// belong to string segments; only expression bodies remain code.
func (r *interpolationReader) literal(start int) bool {
	for r.pos < len(r.text) {
		if !r.takeInterpolationWork() {
			return false
		}
		if bytes.HasPrefix(r.text[r.pos:], []byte(r.closer)) {
			r.skip(len(r.closer))
			r.place(String, start, r.pos)
			return true
		}
		if r.python && len(r.closer) == 1 && r.text[r.pos] == '\n' {
			return false
		}
		if r.skipEscapeOrBrace() {
			continue
		}
		width := r.openerWidth()
		if width == 0 {
			r.skip(1)
			continue
		}
		r.skip(width)
		r.place(String, start, r.pos)
		if !r.expression() {
			return false
		}
		start = r.pos
		r.skip(1) // the closing brace starts the next string segment
	}
	return false
}

// skipEscapeOrBrace leaves Python's \{ as a real interpolation: only
// doubled braces escape interpolation, even in raw f-strings.
func (r *interpolationReader) skipEscapeOrBrace() bool {
	if r.pos+1 >= len(r.text) {
		return false
	}
	c, next := r.text[r.pos], r.text[r.pos+1]
	if c == '\\' && (!r.python || (next != '{' && next != '}')) {
		r.skip(2)
		return true
	}
	if r.python && (c == '{' || c == '}') && c == next {
		r.skip(2)
		return true
	}
	return false
}

func (r *interpolationReader) openerWidth() int {
	if r.python && r.text[r.pos] == '{' {
		return 1
	}
	if !r.python && bytes.HasPrefix(r.text[r.pos:], []byte("${")) {
		return 2
	}
	return 0
}

// expression leaves pos at the matching closing brace. Braces inside
// quoted literals and comments are consumed by next, not counted here.
func (r *interpolationReader) expression() bool {
	if r.interpolationDepth >= 128 {
		*r.interpolationWork = 0
		return false
	}
	r.interpolationDepth++
	defer func() { r.interpolationDepth-- }()
	var brackets []byte
	regexAllowed := true
	for r.pos < len(r.text) {
		if !r.takeInterpolationWork() {
			return false
		}
		c := r.text[r.pos]
		if len(brackets) == 0 {
			if c == '}' {
				return true
			}
			if r.python && c == ':' {
				return r.formatSpec()
			}
		}
		switch c {
		case '(', '[', '{':
			brackets = append(brackets, c)
		case ')', ']', '}':
			if len(brackets) > 0 && closes(brackets[len(brackets)-1], c) {
				brackets = brackets[:len(brackets)-1]
			}
		default:
		}
		regexAllowed = r.expressionToken(regexAllowed)
	}
	return false
}

// formatSpec is literal formatting text except for its nested fields.
func (r *interpolationReader) formatSpec() bool {
	start := r.pos
	for r.pos < len(r.text) {
		if !r.takeInterpolationWork() {
			return false
		}
		switch r.text[r.pos] {
		case '}':
			r.place(String, start, r.pos)
			return true
		case '{':
			r.skip(1)
			r.place(String, start, r.pos)
			if !r.expression() {
				return false
			}
			start = r.pos
		default:
		}
		r.skip(1)
	}
	return false
}

// takeInterpolationWork bounds speculative parsing across all nested attempts.
// After exhaustion the lexer treats remaining quotes as ambiguous code rather
// than allowing a string-only replacement to edit an executable expression.
func (l *lexer) takeInterpolationWork() bool {
	if l.interpolationExhausted() {
		return false
	}
	*l.interpolationWork--
	return true
}

// interpolationExhausted is false until a source has needed interpolation.
func (l *lexer) interpolationExhausted() bool {
	return l.interpolationWork != nil && *l.interpolationWork == 0
}
