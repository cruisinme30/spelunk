package structural

import "bytes"

// expressionToken skips regex literals where an expression can begin, so
// a regex's braces do not close a JavaScript template field. Division
// after a value remains an operator. Regex content is code, not a string.
func (r *interpolationReader) expressionToken(regexAllowed bool) bool {
	start := r.pos
	if r.updateOperator() {
		// Prefix updates still need an operand; postfix updates complete
		// one. Neither changes whether the previous token was a value.
		return regexAllowed
	}
	if !r.python && regexAllowed && r.text[start] == '/' {
		if end := regexLiteralEnd(r.text, start); end > start {
			r.skip(end - start)
			return false
		}
	}
	r.next()
	text := r.text[start:r.pos]
	if len(bytes.TrimSpace(text)) == 0 || bytes.HasPrefix(text, []byte("//")) || bytes.HasPrefix(text, []byte("/*")) {
		return regexAllowed
	}
	if isWordRune(rune(text[0])) {
		switch string(text) {
		case "return", "throw", "case", "delete", "void", "typeof", "yield", "await", "in", "instanceof", "of", "new":
			return true
		default:
			return false
		}
	}
	return !bytes.ContainsAny(text, ")]}'\"`.")
}

// regexLiteralEnd returns the byte after a complete slash-delimited regex,
// ignoring escapes and character classes, or -1 for a comment or no closer.
func regexLiteralEnd(text []byte, start int) int {
	if start+1 == len(text) || text[start+1] == '/' || text[start+1] == '*' {
		return -1
	}
	class := false
	for pos := start + 1; pos < len(text); pos++ {
		switch text[pos] {
		case '\n', '\r':
			return -1
		case '\\':
			pos++
		case '[':
			class = true
		case ']':
			class = false
		case '/':
			if !class {
				return pos + 1
			}
		default:
		}
	}
	return -1
}

// updateOperator consumes a JavaScript increment or decrement as one token.
// Splitting a postfix update into two signs would mistake following division
// for the beginning of a regex literal.
func (r *interpolationReader) updateOperator() bool {
	if r.python || r.pos+1 >= len(r.text) {
		return false
	}
	c := r.text[r.pos]
	if (c != '+' && c != '-') || r.text[r.pos+1] != c {
		return false
	}
	r.skip(2)
	return true
}
