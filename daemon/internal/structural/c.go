package structural

// cCursor reads the logical C/C++ source after escaped newline removal,
// retaining physical offsets for token ranges and replacement plans. Raw
// strings read the original bytes instead: C++ preserves their newlines.
type cCursor struct {
	text []byte
	pos  int
}

func (c *cCursor) next() (byte, bool) {
	c.pos = cSpliceEnd(c.text, c.pos)
	if c.pos == len(c.text) {
		return 0, false
	}
	b := c.text[c.pos]
	c.pos++
	return b, true
}

// cSpliceEnd skips consecutive backslash-newline pairs, including CRLF.
func cSpliceEnd(text []byte, at int) int {
	for at+1 < len(text) && text[at] == '\\' {
		switch {
		case text[at+1] == '\n':
			at += 2
		case at+2 < len(text) && text[at+1] == '\r' && text[at+2] == '\n':
			at += 3
		default:
			return at
		}
	}
	return at
}

func (l *lexer) cToken() bool {
	end := cSpliceEnd(l.text, l.pos)
	if end == l.pos {
		return l.cNumber() || l.cWord()
	}
	l.skip(end - l.pos)
	return true
}

// cWord keeps identifiers joined across splices. Otherwise the R at the
// end of a split identifier could incorrectly open a C++ raw string.
func (l *lexer) cWord() bool {
	first, _ := runeAt(l.text, l.pos)
	if !isWordRune(first) {
		return false
	}
	start, line := l.pos, l.line
	if l.cPrefixedString(start) {
		return true
	}
	end := start
	for at := start; at < len(l.text); at = cSpliceEnd(l.text, end) {
		r, size := runeAt(l.text, at)
		if !isWordRune(r) {
			break
		}
		end = at + size
	}
	l.skip(end - start)
	l.emit(word, start, end, line)
	return true
}

func (l *lexer) cComment() bool {
	if l.text[l.pos] != '/' {
		return false
	}
	c := cCursor{text: l.text, pos: l.pos + 1}
	marker, ok := c.next()
	if !ok || marker != '/' && marker != '*' {
		return false
	}
	end := c.commentEnd(marker)
	l.place(Comment, l.pos, end)
	l.skip(end - l.pos)
	return true
}

func (c *cCursor) commentEnd(marker byte) int {
	var previous byte
	for {
		b, ok := c.next()
		switch {
		case !ok:
			return len(c.text)
		case marker == '/' && b == '\n':
			return c.pos - 1
		case marker == '*' && previous == '*' && b == '/':
			return c.pos
		}
		previous = b
	}
}

func (l *lexer) cQuoted(start, at int, quote byte) bool {
	c := cCursor{text: l.text, pos: at + 1}
	escaped := false
	for {
		b, ok := c.next()
		switch {
		case !ok || b == '\n' || b == '\r':
			return false
		case !escaped && b == quote:
			l.emitString(start, c.pos)
			return true
		}
		escaped = !escaped && b == '\\'
	}
}

// cNumber keeps preprocessing numbers together, so C++/C23 digit separators
// cannot be mistaken for character literals. Signs only follow exponents.
func (l *lexer) cNumber() bool {
	c := cCursor{text: l.text, pos: l.pos}
	first, _ := c.next()
	if !cDigit(first) && (first != '.' || !c.nextDigit()) {
		return false
	}
	start, end, line := l.pos, c.pos, l.line
	previous := first
	for {
		b, ok := c.next()
		if !ok || !c.numberByte(b, previous) {
			break
		}
		previous, end = b, c.pos
	}
	l.skip(end - l.pos)
	l.emit(word, start, end, line)
	return true
}

func (c *cCursor) peek() (byte, bool) {
	trial := *c
	return trial.next()
}

func (c *cCursor) nextDigit() bool {
	b, ok := c.peek()
	return ok && cDigit(b)
}

func cDigit(b byte) bool { return b >= '0' && b <= '9' }

func (c *cCursor) numberByte(b, previous byte) bool {
	switch b {
	case '+', '-':
		return previous == 'e' || previous == 'E' || previous == 'p' || previous == 'P'
	case '\'':
		next, ok := c.peek()
		return ok && cNumberWord(next)
	default:
		return b == '.' || cNumberWord(b)
	}
}

func cNumberWord(b byte) bool {
	return b >= 128 || isWordRune(rune(b))
}

// cPrefixedString recognizes prefixes even when line splices divide them.
// Ordinary character constants may contain multiple characters in C/C++.
func (l *lexer) cPrefixedString(start int) bool {
	c := cCursor{text: l.text, pos: start}
	var prefix [3]byte
	for n := 0; n <= len(prefix); n++ {
		b, ok := c.next()
		if !ok {
			return false
		}
		if b == '"' || b == '\'' {
			return l.cPrefixedLiteral(start, c.pos-1, string(prefix[:n]), b)
		}
		if n < len(prefix) {
			prefix[n] = b
		}
	}
	return false
}

func (l *lexer) cPrefixedLiteral(start, at int, prefix string, quote byte) bool {
	switch prefix {
	case "u8", "u", "U", "L":
		return l.cQuoted(start, at, quote)
	case "R", "u8R", "uR", "UR", "LR":
		if l.cppRaw && quote == '"' {
			if end := cppRawEnd(l.text, at); end >= 0 {
				l.emitString(start, end)
				return true
			}
		}
	}
	return false
}
