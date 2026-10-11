package structural

import (
	"bytes"
	"slices"
	"unicode"
	"unicode/utf8"
)

// kind is what a token is.
type kind uint8

const (
	word    kind = iota // letters, digits and _
	punct               // one character that is none of the others
	str                 // a whole string or character literal
	opening             // ( [ {
	closing             // ) ] }
	hole                // a template's :[name]; files have none
)

// token is one piece of code: its bytes, the 0-based line it starts on,
// and whether it touches the token before it, with no space or comment
// between them (== is two joined = tokens).
type token struct {
	kind       kind
	start, end int
	line       int
	joined     bool
}

// syntax is how a language writes comments and strings. Everything else
// (words, punctuation, brackets) reads the same in every language.
type syntax struct {
	// lineComments run to the end of the line; blockComments to their closer.
	lineComments  []string
	blockComments [][2]string
	// hashAtWordStart means # starts a comment only where a word could
	// start, as in shell, so ${#list} and a#b are code.
	hashAtWordStart bool
	// quotes open strings that end on the same line, with backslash
	// escapes (a backslash before a line end continues the string).
	quotes string
	// doubledQuotes are strings that escape their quote by doubling it, as
	// SQL does, with no backslash escapes. They may run over lines.
	doubledQuotes string
	// rawQuotes open strings that may run over lines; escapes says
	// whether a backslash escapes in them (a JS template string) or not
	// (a Go raw string).
	rawQuotes   string
	rawEscapes  bool
	tripleQuote bool // """ and ''' strings, which may run over lines
	// charQuote reads ' as a one-character literal ('a', '\n') and
	// otherwise as punctuation, so Rust's 'a lifetimes stay code.
	charQuote bool
	// rustRaw reads r"…" and r#"…"#; cppRaw R"delim(…)delim"; verbatim
	// C#'s @"…", whose "" is a quote.
	rustRaw, cppRaw, verbatim bool
}

var cLike = syntax{lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, quotes: `"`, charQuote: true}

// languages are the syntaxes of the languages Spelunk knows (see the lang
// package); any other file reads as plain text with "strings".
var languages = map[string]syntax{
	"go":         {lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, quotes: `"`, charQuote: true, rawQuotes: "`"},
	"c":          cLike,
	"c++":        withCppRaw(cLike),
	"java":       withTriple(cLike),
	"kotlin":     withTriple(cLike),
	"scala":      withTriple(cLike),
	"swift":      withTriple(cLike),
	"csharp":     withVerbatim(withTriple(cLike)),
	"rust":       withRustRaw(cLike),
	"protobuf":   {lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, quotes: `"'`},
	"javascript": {lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, quotes: `"'`, rawQuotes: "`", rawEscapes: true},
	"typescript": {lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, quotes: `"'`, rawQuotes: "`", rawEscapes: true},
	"php":        {lineComments: []string{"//", "#"}, blockComments: [][2]string{{"/*", "*/"}}, quotes: `"'`},
	"python":     {lineComments: []string{"#"}, quotes: `"'`, tripleQuote: true},
	"ruby":       {lineComments: []string{"#"}, quotes: `"'`},
	"shell":      {lineComments: []string{"#"}, hashAtWordStart: true, quotes: `"`, doubledQuotes: "'"},
	"sql":        {lineComments: []string{"--"}, blockComments: [][2]string{{"/*", "*/"}}, doubledQuotes: `'"`},
	"yaml":       {lineComments: []string{"#"}, quotes: `"'`},
	"toml":       {lineComments: []string{"#"}, quotes: `"'`, tripleQuote: true},
	"json":       {lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, quotes: `"`},
	"css":        {blockComments: [][2]string{{"/*", "*/"}}, quotes: `"'`},
	"html":       {blockComments: [][2]string{{"<!--", "-->"}}},
	"markdown":   {blockComments: [][2]string{{"<!--", "-->"}}},
	"make":       {lineComments: []string{"#"}},
	"docker":     {lineComments: []string{"#"}, quotes: `"'`},
	"cmake":      {lineComments: []string{"#"}, quotes: `"`},
	"lua":        {lineComments: []string{"--"}, blockComments: [][2]string{{"--[[", "]]"}}, quotes: `"'`},
	"perl":       {lineComments: []string{"#"}, quotes: `"'`},
	"r":          {lineComments: []string{"#"}, quotes: `"'`},
	"dart":       {lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, quotes: `"'`, tripleQuote: true},
	"elixir":     {lineComments: []string{"#"}, quotes: `"'`, tripleQuote: true},
	"haskell":    {lineComments: []string{"--"}, blockComments: [][2]string{{"{-", "-}"}}, quotes: `"`, charQuote: true},
}

// plainText is the syntax of a file in no known language.
var plainText = syntax{quotes: `"`}

func withTriple(s syntax) syntax   { s.tripleQuote = true; return s }
func withCppRaw(s syntax) syntax   { s.cppRaw = true; return s }
func withRustRaw(s syntax) syntax  { s.rustRaw = true; return s }
func withVerbatim(s syntax) syntax { s.verbatim = true; return s }

// syntaxOf returns the syntax of language, a lang package name.
func syntaxOf(language string) syntax {
	if s, ok := languages[language]; ok {
		return s
	}
	return plainText
}

// lexer reads text into tokens by one language's syntax.
type lexer struct {
	syntax
	text   []byte
	pos    int
	line   int
	tokens []token
	// gap means a space or a comment came after the last token.
	gap bool
	// places, when set, collects the comments and strings read instead of
	// the tokens (see Places).
	places *[]Place
}

// lex reads text into tokens. Comments and spaces become nothing; they
// only keep the next token from being joined to the one before.
func lex(text []byte, s syntax) []token {
	return lexInto(nil, text, s)
}

// lexInto is lex, appending the tokens to buf[:0] so a caller can reuse
// one buffer across texts.
func lexInto(buf []token, text []byte, s syntax) []token {
	l := &lexer{syntax: s, text: text, tokens: buf[:0], gap: true}
	for l.pos < len(text) {
		l.next()
	}
	return l.tokens
}

func (l *lexer) next() {
	r, size := utf8.DecodeRune(l.text[l.pos:])
	switch {
	case r == '\n':
		l.pos++
		l.line++
		l.gap = true
	case unicode.IsSpace(r):
		l.pos += size
		l.gap = true
	case l.comment():
		l.gap = true
	case isWordRune(r):
		start := l.pos
		for l.pos < len(l.text) {
			next, width := utf8.DecodeRune(l.text[l.pos:])
			if !isWordRune(next) {
				break
			}
			l.pos += width
		}
		// A word right before a quote may prefix a raw string: r"…", R"(…)".
		if !l.prefixedString(start) {
			l.emit(word, start, l.pos, l.line)
		}
	case l.literal():
	default:
		k := punct
		switch r {
		case '(', '[', '{':
			k = opening
		case ')', ']', '}':
			k = closing
		}
		l.emit(k, l.pos, l.pos+size, l.line)
		l.pos += size
	}
}

func (l *lexer) emit(k kind, start, end, line int) {
	if l.places != nil { // only reading where comments and strings are
		l.gap = false
		return
	}
	l.tokens = append(l.tokens, token{kind: k, start: start, end: end, line: line, joined: !l.gap && len(l.tokens) > 0})
	l.gap = false
}

// isWordRune reports whether r belongs in a word.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// comment skips a comment that starts at pos, reporting whether there was
// one. Block comments go first: Lua's --[[ starts with its line comment's --.
func (l *lexer) comment() bool {
	rest := l.text[l.pos:]
	for _, pair := range l.blockComments {
		if !bytes.HasPrefix(rest, []byte(pair[0])) {
			continue
		}
		end := bytes.Index(rest[len(pair[0]):], []byte(pair[1]))
		if end < 0 {
			end = len(rest) // an unclosed comment runs to the end
		} else {
			end += len(pair[0]) + len(pair[1])
		}
		l.place(Comment, l.pos, l.pos+end)
		l.skip(end)
		return true
	}
	for _, marker := range l.lineComments {
		if !bytes.HasPrefix(rest, []byte(marker)) {
			continue
		}
		if marker == "#" && l.hashAtWordStart && !l.gap {
			continue
		}
		end := bytes.IndexByte(rest, '\n')
		if end < 0 {
			end = len(rest)
		}
		l.place(Comment, l.pos, l.pos+end)
		l.pos += end
		return true
	}
	return false
}

// skip moves past n bytes, counting the lines they end.
func (l *lexer) skip(n int) {
	l.line += bytes.Count(l.text[l.pos:l.pos+n], []byte("\n"))
	l.pos += n
}

// literal reads a string or character literal that starts at pos,
// reporting whether there was one. A quote that never closes is
// punctuation, so one stray apostrophe can't hide the rest of a file.
func (l *lexer) literal() bool {
	c := l.text[l.pos]
	switch {
	case l.tripleQuote && (c == '"' || c == '\'') && bytes.HasPrefix(l.text[l.pos:], []byte{c, c, c}):
		return l.closedBy(3, string([]byte{c, c, c}), true)
	case l.verbatim && c == '@' && l.pos+1 < len(l.text) && l.text[l.pos+1] == '"':
		return l.doubled(l.pos, l.pos+2, '"')
	case l.charQuote && c == '\'':
		return l.char()
	case bytes.IndexByte([]byte(l.rawQuotes), c) >= 0:
		return l.closedBy(1, string(c), l.rawEscapes)
	case bytes.IndexByte([]byte(l.doubledQuotes), c) >= 0:
		return l.doubled(l.pos, l.pos+1, c)
	case bytes.IndexByte([]byte(l.quotes), c) >= 0:
		return l.oneLine(c)
	}
	return false
}

// closedBy reads a string that opens with width bytes and closes at the
// next closer, over lines if need be.
func (l *lexer) closedBy(width int, closer string, escapes bool) bool {
	for i := l.pos + width; i < len(l.text); i++ {
		switch {
		case escapes && l.text[i] == '\\':
			i++
		case bytes.HasPrefix(l.text[i:], []byte(closer)):
			l.emitString(l.pos, i+len(closer))
			return true
		}
	}
	return false
}

// oneLine reads a quote-delimited string with backslash escapes that ends
// on its line.
func (l *lexer) oneLine(quote byte) bool {
	for i := l.pos + 1; i < len(l.text); i++ {
		switch l.text[i] {
		case '\\':
			i++ // an escaped line end continues the string
		case '\n':
			return false
		case quote:
			l.emitString(l.pos, i+1)
			return true
		}
	}
	return false
}

// doubled reads a string from start whose body begins at body and whose
// quote is escaped by doubling it.
func (l *lexer) doubled(start, body int, quote byte) bool {
	for i := body; i < len(l.text); i++ {
		if l.text[i] != quote {
			continue
		}
		if i+1 < len(l.text) && l.text[i+1] == quote {
			i++
			continue
		}
		l.emitString(start, i+1)
		return true
	}
	return false
}

// char reads a character literal: a quote, one character or an escape, and
// a quote. Anything else leaves the quote as punctuation.
func (l *lexer) char() bool {
	i := l.pos + 1
	if i >= len(l.text) || l.text[i] == '\n' || l.text[i] == '\'' {
		return false
	}
	if l.text[i] == '\\' {
		// An escape: \n, \', \u{1F600}, \x41. Close within a few bytes.
		for j := i + 2; j < len(l.text) && j < i+12 && l.text[j] != '\n'; j++ {
			if l.text[j] == '\'' {
				l.emitString(l.pos, j+1)
				return true
			}
		}
		return false
	}
	_, size := utf8.DecodeRune(l.text[i:])
	if i+size < len(l.text) && l.text[i+size] == '\'' {
		l.emitString(l.pos, i+size+1)
		return true
	}
	return false
}

// prefixedString reads a raw string whose prefix is the word from start
// to pos: Rust's r"…" and r#"…"#, or C++'s R"delim(…)delim". It reports
// whether it read one; otherwise pos is unchanged.
func (l *lexer) prefixedString(start int) bool {
	prefix := string(l.text[start:l.pos])
	end := -1
	switch {
	case l.rustRaw && (prefix == "r" || prefix == "br"):
		end = rustRawEnd(l.text, l.pos)
	case l.cppRaw && (prefix == "R" || prefix == "u8R" || prefix == "LR" || prefix == "uR" || prefix == "UR"):
		end = cppRawEnd(l.text, l.pos)
	}
	if end < 0 {
		return false
	}
	l.emitString(start, end)
	return true
}

// rustRawEnd returns where a Rust raw string's body, from at (just past
// its r), ends: #"…"# with as many #s at both ends. It returns -1 when
// there is no such string.
func rustRawEnd(text []byte, at int) int {
	i := at
	for i < len(text) && text[i] == '#' {
		i++
	}
	if i >= len(text) || text[i] != '"' {
		return -1
	}
	closer := `"` + string(text[at:i])
	end := bytes.Index(text[i+1:], []byte(closer))
	if end < 0 {
		return -1
	}
	return i + 1 + end + len(closer)
}

// cppRawEnd returns where a C++ raw string's body, from at (just past its
// R), ends: "delim(…)delim", the delimiter at most 16 characters with no
// space, backslash or parenthesis. It returns -1 when there is no such
// string.
func cppRawEnd(text []byte, at int) int {
	if at >= len(text) || text[at] != '"' {
		return -1
	}
	paren := bytes.IndexByte(text[at:], '(')
	if paren < 0 || paren > 17 || bytes.ContainsAny(text[at+1:at+paren], " \\)\n") {
		return -1
	}
	closer := ")" + string(text[at+1:at+paren]) + `"`
	end := bytes.Index(text[at+paren:], []byte(closer))
	if end < 0 {
		return -1
	}
	return at + paren + end + len(closer)
}

// emitString emits a string token from start to end and moves past it.
func (l *lexer) emitString(start, end int) {
	l.place(String, start, end)
	line := l.line
	l.line += bytes.Count(l.text[l.pos:end], []byte("\n"))
	l.pos = end
	l.emit(str, start, end, line)
}

// partners pairs each opening bracket with its closer: partner[i] is the
// index of the token that closes token i, or -1 when none does. Only
// opening brackets have one.
func partners(tokens []token, text []byte) []int {
	var b buffers
	b.pair(tokens, text)
	return b.partner
}

// pair fills b.partner with the partners of tokens, reusing b's slices.
func (b *buffers) pair(tokens []token, text []byte) {
	partner := slices.Grow(b.partner[:0], len(tokens))[:len(tokens)]
	stack := b.stack[:0]
	for i, t := range tokens {
		partner[i] = -1
		switch t.kind {
		case opening:
			stack = append(stack, i)
		case closing:
			// A closer that doesn't match the innermost opener leaves the
			// stack alone: one stray ) shouldn't unpair the brackets around it.
			if n := len(stack); n > 0 && closes(text[tokens[stack[n-1]].start], text[t.start]) {
				partner[stack[n-1]] = i
				stack = stack[:n-1]
			}
		default:
		}
	}
	b.partner, b.stack = partner, stack
}

// closes reports whether closer closes opener.
func closes(opener, closer byte) bool {
	switch opener {
	case '(':
		return closer == ')'
	case '[':
		return closer == ']'
	default:
		return closer == '}'
	}
}
