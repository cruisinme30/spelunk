package query

import (
	"strings"
)

type tokenKind int

const (
	tokenEnd tokenKind = iota
	tokenOpenParen
	tokenCloseParen
	tokenMinus // "-" directly in front of something: negation
	tokenAnd   // the keyword AND
	tokenOr    // the keyword OR
	tokenTerm  // a bare, quoted or /regex/ text term
	tokenOperator
)

// valueForm is how a term or operator value was written.
type valueForm int

const (
	formBare valueForm = iota
	formQuoted
	formRegex
)

// token is one lexical token. Offsets are UTF-16.
type token struct {
	kind       tokenKind
	start, end int

	// Terms and operators.
	value string
	form  valueForm
	// unclosed is the delimiter (" or /) of a quote or regex that never closed.
	unclosed rune

	// Operators only.
	name       string
	valueStart int // where the value begins, after the colon
}

// isOperatorName reports whether word looks like the name part of name:value.
func isOperatorName(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// splitOperator splits a bare word shaped like name:value. Words such as
// std::vector and http://example.com are not operators.
func splitOperator(word string) (name, rest string, ok bool) {
	name, rest, found := strings.Cut(word, ":")
	if !found || !isOperatorName(name) || strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "//") {
		return "", "", false
	}
	return name, rest, true
}

// lexer turns query text into tokens.
type lexer struct {
	src    *source
	pos    int // rune index
	tokens []token
}

func lex(src *source) []token {
	l := &lexer{src: src}
	for l.pos < len(src.runes) {
		l.next()
	}
	l.tokens = append(l.tokens, token{kind: tokenEnd, start: src.length(), end: src.length()})
	return l.tokens
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

// endsBareWord reports whether r ends a bare word.
func endsBareWord(r rune) bool { return isSpace(r) || r == '(' || r == ')' || r == '"' }

func (l *lexer) emit(kind tokenKind, startRune, endRune int) *token {
	l.tokens = append(l.tokens, token{kind: kind, start: l.src.offsets[startRune], end: l.src.offsets[endRune]})
	return &l.tokens[len(l.tokens)-1]
}

func (l *lexer) next() {
	runes := l.src.runes
	r := runes[l.pos]
	switch {
	case isSpace(r):
		l.pos++
	case r == '(':
		l.emit(tokenOpenParen, l.pos, l.pos+1)
		l.pos++
	case r == ')':
		l.emit(tokenCloseParen, l.pos, l.pos+1)
		l.pos++
	case r == '-' && l.pos+1 < len(runes) && !isSpace(runes[l.pos+1]) && runes[l.pos+1] != ')':
		l.emit(tokenMinus, l.pos, l.pos+1)
		l.pos++
	case r == '"' || r == '/':
		l.delimitedTerm(r)
	default:
		l.word()
	}
}

// delimitedTerm lexes a "quoted phrase" or /regex/ term.
func (l *lexer) delimitedTerm(delimiter rune) {
	start := l.pos
	value, end, closed := readDelimited(l.src.runes, start, delimiter)
	t := l.emit(tokenTerm, start, end)
	t.value = value
	t.form = formQuoted
	if delimiter == '/' {
		t.form = formRegex
	}
	if !closed {
		t.unclosed = delimiter
	}
	l.pos = end
}

// word lexes a bare word: a term, an operator, or AND / OR.
func (l *lexer) word() {
	runes := l.src.runes
	start := l.pos
	end := start
	for end < len(runes) && !endsBareWord(runes[end]) {
		end++
	}
	word := string(runes[start:end])
	if name, rest, ok := splitOperator(word); ok {
		l.operator(start, end, name, rest)
		return
	}
	switch word {
	case "AND":
		l.emit(tokenAnd, start, end)
	case "OR":
		l.emit(tokenOr, start, end)
	default:
		t := l.emit(tokenTerm, start, end)
		t.value = word
		t.form = formBare
	}
	l.pos = end
}

// operator lexes name:value, where the value may be bare, "quoted" or /regex/.
func (l *lexer) operator(start, bareEnd int, name, rest string) {
	runes := l.src.runes
	valueStart := start + len([]rune(name)) + 1
	end := bareEnd
	value, form, unclosed := rest, formBare, rune(0)
	switch {
	case rest == "" && bareEnd < len(runes) && runes[bareEnd] == '"':
		var closed bool
		value, end, closed = readDelimited(runes, bareEnd, '"')
		form = formQuoted
		if !closed {
			unclosed = '"'
		}
	case strings.HasPrefix(rest, "/"):
		// name:/regex/ may contain spaces, so read again from the slash.
		regex, regexEnd, closed := readDelimited(runes, valueStart, '/')
		switch {
		case closed && regexEnd >= bareEnd:
			value, end, form = regex, regexEnd, formRegex
		case closed:
			// Closes inside the word, like f:/src/main: a bare path, not a regex.
		case rest == "/" || strings.HasSuffix(rest, "/"):
			value, end, form, unclosed = regex, regexEnd, formRegex, '/'
		default:
			// A bare value that merely starts with a slash, like f:/src.
		}
	}
	t := l.emit(tokenOperator, start, end)
	t.name = name
	t.value = value
	t.form = form
	t.unclosed = unclosed
	t.valueStart = l.src.offsets[valueStart]
	l.pos = end
}

// readDelimited reads a "…" or /…/ that opens at runes[start]. A backslash
// escapes the delimiter. Inside quotes \\ is a backslash; inside regexes
// every other escape is kept as written for RE2. It returns the content,
// the rune index after the closing delimiter, and whether it closed.
func readDelimited(runes []rune, start int, delimiter rune) (content string, end int, closed bool) {
	var b strings.Builder
	i := start + 1
	for i < len(runes) {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) {
			escaped := runes[i+1]
			switch {
			case escaped == delimiter:
				b.WriteRune(delimiter)
			case delimiter == '"' && escaped == '\\':
				b.WriteRune('\\')
			default:
				b.WriteRune('\\')
				b.WriteRune(escaped)
			}
			i += 2
			continue
		}
		if r == delimiter {
			return b.String(), i + 1, true
		}
		b.WriteRune(r)
		i++
	}
	return b.String(), len(runes), false
}
