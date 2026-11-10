package symbols

import (
	"bytes"

	"github.com/cruisinme30/spelunk/daemon/internal/structural"
)

// definitionCode hides comment and literal contents while retaining line
// numbers and quotes needed by rules such as extern "C" and C attributes.
// The input is never modified. Shared lexical rules keep strings and comments
// consistent with context-filtered search. The second result maps each
// multiline literal's starting line to its final line for body extents.
func definitionCode(content []byte, language string) ([]byte, map[int]int) {
	places := structural.Places(content, language)
	if len(places) == 0 && !supportsBraceSpans(language) {
		return content, nil
	}
	code := bytes.Clone(content)
	var continued map[int]int
	line, offset := 1, 0
	for _, place := range places {
		line += bytes.Count(content[offset:place.Start], []byte{'\n'})
		endLine := line + bytes.Count(content[place.Start:place.End], []byte{'\n'})
		maskCode(code[place.Start:place.End])
		// Keep quote syntax required by declaration rules. A multiline
		// closer may have no indentation: retaining it would falsely close a
		// Python body. Its extent is recorded separately instead.
		if literalOpening(content, place) {
			code[place.Start] = content[place.Start]
		}
		if endLine == line && isQuote(content[place.End-1]) {
			code[place.End-1] = content[place.End-1]
		}
		if endLine > line && (place.Kind == structural.String || isQuote(content[place.Start])) {
			if continued == nil {
				continued = make(map[int]int)
			}
			continued[line] = max(continued[line], endLine)
		}
		line, offset = endLine, place.End
	}
	if supportsBraceSpans(language) {
		maskDirectives(code, content)
	}
	return code, continued
}

// literalOpening retains the indentation of a literal, including a prefix
// already recognized by the shared lexer (such as Python f/RF or Rust r).
// Interpolation continuation segments start with delimiters, not prefixes,
// so their closing lines remain masked rather than ending an enclosing body.
func literalOpening(content []byte, place structural.Place) bool {
	c := content[place.Start]
	if isQuote(c) {
		return true
	}
	return place.Kind == structural.String && (c == '@' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z')
}

// isQuote reports literal delimiters relevant to declaration rules.
func isQuote(c byte) bool { return c == '\'' || c == '"' || c == '`' }

// maskCode blanks non-newline bytes without changing source positions.
func maskCode(code []byte) {
	for i, c := range code {
		if c != '\n' && c != '\r' {
			code[i] = ' '
		}
	}
}

// maskDirectives excludes C/C++ preprocessing directives, including escaped
// continuations: braces in macro definitions are not source block boundaries.
func maskDirectives(code, content []byte) {
	continued := false
	for start := 0; start < len(code); {
		end := bytes.IndexByte(code[start:], '\n')
		if end < 0 {
			end = len(code)
		} else {
			end += start
		}
		line := code[start:end]
		trimmed := bytes.TrimSpace(content[start:end])
		if continued || bytes.HasPrefix(trimmed, []byte{'#'}) {
			continued = bytes.HasSuffix(trimmed, []byte{'\\'})
			maskCode(line)
		}
		start = end + 1
	}
}

// supportsBraceSpans opts C/C++ into lexical brace spans. Other languages keep the
// established indentation/end heuristic: their return types, regex literals,
// and other brace-bearing syntax need a language parser before changing it.
func supportsBraceSpans(language string) bool { return language == "c" || language == "c++" }

// applyBraceSpans replaces indentation estimates with matching brace spans
// and removes function candidates that turned out to be declarations.
func applyBraceSpans(found []Symbol, code []byte) []Symbol {
	openers, declarations := cHeaders(found, code)
	completeBraceSpans(found, code, openers)
	kept := found[:0]
	for i, symbol := range found {
		if !declarations[i] {
			kept = append(kept, symbol)
		}
	}
	return kept
}

// cHeaders locates body openers and confirmed non-definition functions.
// Header scans stop at the next candidate, so total work is linear even
// when an incomplete header never closes its parameters. Only definition
// braces allocate a map item; unrelated braces need no retained state.
func cHeaders(found []Symbol, code []byte) (openers map[int]int, declarations map[int]bool) {
	if len(found) == 0 {
		return nil, nil
	}
	starts := []int{0}
	for i, c := range code {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	openers = make(map[int]int, len(found))
	for i, symbol := range found {
		start, limit := starts[symbol.Line-1], len(code)
		if i+1 < len(found) {
			limit = starts[found[i+1].Line-1]
		}
		at, declared := scanCHeader(code[start:limit])
		if at >= 0 {
			openers[start+at] = i
		}
		if declared && symbol.Kind == function {
			if declarations == nil {
				declarations = make(map[int]bool)
			}
			declarations[i] = true
		}
	}
	return openers, declarations
}

// completeBraceSpans assigns only complete bodies; an unmatched body keeps
// its indentation-derived fallback. Strings and comments have been masked.
func completeBraceSpans(found []Symbol, code []byte, openers map[int]int) {
	type openBrace struct{ index, depth int }
	var stack []openBrace
	line, depth := 1, 0
	for i, c := range code {
		switch c {
		case '\n':
			line++
		case '{':
			depth++
			if index, ok := openers[i]; ok {
				stack = append(stack, openBrace{index: index, depth: depth})
			}
		case '}':
			if len(stack) > 0 && stack[len(stack)-1].depth == depth {
				found[stack[len(stack)-1].index].End = line
				stack = stack[:len(stack)-1]
			}
			depth = max(0, depth-1)
		}
	}
}

// scanCHeader distinguishes a body from a declaration or initializer before
// it. Reserved implementation attributes can continue a signature on later
// lines (Linux __acquires/__releases, GCC __attribute__, C++ [[attributes]]).
// It skips braces nested in parameter or type-argument lists.
// An unknown continuation keeps the existing incomplete-definition fallback.
func scanCHeader(header []byte) (body int, declaration bool) {
	parens, brackets := 0, 0
	endedLine := false
	for i, c := range header {
		if endedLine && !headerSpace(c) {
			if !headerContinuation(header[i:]) {
				return -1, false
			}
			endedLine = false
		}
		switch c {
		case '(':
			parens++
		case ')':
			parens = max(0, parens-1)
		case '[':
			brackets++
		case ']':
			brackets = max(0, brackets-1)
		case '{':
			if parens+brackets == 0 {
				return i, false
			}
		case '\n':
			endedLine = parens+brackets == 0
		case ';', '=', '}':
			if parens+brackets == 0 {
				return -1, c != '}'
			}
		}
	}
	return -1, false
}

// headerContinuation recognizes punctuation ending a declaration, a body,
// or an implementation attribute after a signature's line break.
func headerContinuation(rest []byte) bool {
	return rest[0] == '{' || rest[0] == ';' || rest[0] == '=' ||
		bytes.HasPrefix(rest, []byte("__")) || bytes.HasPrefix(rest, []byte("[["))
}

// headerSpace is whitespace allowed between a signature and its body brace.
func headerSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
