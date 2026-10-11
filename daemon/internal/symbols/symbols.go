package symbols

import (
	"bytes"
	"regexp"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// Symbol is one definition.
type Symbol struct {
	Name string
	Kind protocol.SymbolKind
	Line int // 1-based
	// End is the definition's last line (1-based, at least Line): the last
	// line indented under it, or the closing brace or end that lines up
	// with it (see bodies).
	End int
}

// maxLineBytes skips lines too long to be a definition someone wrote, such
// as minified code.
const maxLineBytes = 1_000

// Extract returns the definitions in content, a file in language lang (a
// canonical name from the lang package), in line order. It returns nil for
// a language it has no rules for.
func Extract(lang string, content []byte) []Symbol {
	rules := byLanguage[lang]
	if len(rules) == 0 {
		return nil
	}
	var found []Symbol
	var squeezed []byte
	var b bodies
	eachLine(content, func(number int, line []byte) {
		indent := indentOf(line)
		if indent == len(line) {
			return // a blank line ends nothing
		}
		b.read(found, number, line[indent:], indent)
		squeezed = squeezeSpace(squeezed[:0], line)
		if symbol, _, ok := match(rules, squeezed); ok {
			symbol.Line = number
			found = append(found, symbol)
			b.open = append(b.open, openBody{index: len(found) - 1, indent: indent})
		}
	})
	b.closeAll(found)
	return found
}

// bodies finds where definitions end, as the outline finds what is inside
// them: a definition's body is the lines indented under it. A line as far
// left as the definition ends it, and is its last line when it closes it,
// as a "}" or Ruby's "end" does; a ")" or "{" there still belongs to the
// definition's first line, as in a Go signature spread over lines or a C
// function whose brace sits on a line of its own.
type bodies struct {
	open     []openBody // innermost last
	nonBlank int        // the last line read that wasn't blank
}

// openBody is a definition whose end hasn't been read yet.
type openBody struct{ index, indent int }

// read takes line number, without its indentation of indent, before it is
// matched as a definition, and ends the definitions it closes.
func (b *bodies) read(found []Symbol, number int, text []byte, indent int) {
	for len(b.open) > 0 {
		top := b.open[len(b.open)-1]
		if top.indent < indent {
			break
		}
		if top.indent == indent && continuesHeader(text) {
			break
		}
		b.open = b.open[:len(b.open)-1]
		if top.indent == indent && closesBody(text) {
			found[top.index].End = number
			break
		}
		found[top.index].End = b.nonBlank
	}
	b.nonBlank = number
}

// closeAll ends the definitions still open at the end of the file.
func (b *bodies) closeAll(found []Symbol) {
	for _, open := range b.open {
		found[open.index].End = max(b.nonBlank, found[open.index].Line)
	}
	b.open = nil
}

// continuesHeader says whether a line as far left as a definition carries
// on its first line rather than ending it.
func continuesHeader(text []byte) bool {
	return text[0] == ')' || text[0] == ']' || text[0] == '{'
}

// closesBody says whether a line as far left as a definition is its last.
func closesBody(text []byte) bool {
	if text[0] == '}' {
		return true
	}
	rest, ok := bytes.CutPrefix(bytes.TrimRight(text, " \t\r"), []byte("end"))
	return ok && (len(rest) == 0 || rest[0] == ';' || bytes.HasPrefix(rest, []byte(" #")))
}

// eachLine calls f with each line of content and its 1-based number,
// skipping lines over maxLineBytes.
func eachLine(content []byte, f func(number int, line []byte)) {
	number := 0
	for rest := content; len(rest) > 0; {
		number++
		line := rest
		if end := bytes.IndexByte(rest, '\n'); end >= 0 {
			line, rest = rest[:end], rest[end+1:]
		} else {
			rest = nil
		}
		if len(line) <= maxLineBytes {
			f(number, line)
		}
	}
}

// squeezeSpace appends line to buf with each run of whitespace (what \s
// matches) turned into one space. The rules only ever ask whether there is
// whitespace, never how much, so they match the same; but RE2 tracks a
// thread per way to split a run between \s* and \s+, which made a 10 MB
// file of blank-padded lines take seconds instead of milliseconds.
func squeezeSpace(buf, line []byte) []byte {
	inSpace := false
	for _, c := range line {
		if c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\n' {
			if !inSpace {
				buf = append(buf, ' ')
			}
			inSpace = true
			continue
		}
		buf = append(buf, c)
		inSpace = false
	}
	return buf
}

// match applies the first rule that matches line. parent is the type a
// Go method's receiver names, if the rule has a group for it.
func match(rules []rule, line []byte) (symbol Symbol, parent string, ok bool) {
	for _, r := range rules {
		m := r.re.FindSubmatchIndex(line)
		if m == nil {
			continue
		}
		name := string(line[m[2*r.name]:m[2*r.name+1]])
		if reserved[name] {
			continue
		}
		kind := r.kind
		if r.indent > 0 && m[2*r.indent+1] > m[2*r.indent] {
			kind = r.indentedKind
		}
		if r.parent > 0 && m[2*r.parent] >= 0 {
			parent = string(line[m[2*r.parent]:m[2*r.parent+1]])
		}
		return Symbol{Name: name, Kind: kind}, parent, true
	}
	return Symbol{}, "", false
}

// rule is one way a language defines something: a regex with a group
// named "name", and optionally one named "indent".
type rule struct {
	re   *regexp.Regexp
	kind protocol.SymbolKind
	name int // the index of the name group
	// indent is the index of the indentation group, or 0. When it matched
	// something the kind is indentedKind: a def inside a class is a method.
	indent       int
	indentedKind protocol.SymbolKind
	// parent is the index of a group named "parent", or 0: the type a Go
	// method's receiver names.
	parent int
}

// define makes a rule finding kind with pattern.
func define(kind protocol.SymbolKind, pattern string) rule {
	re := regexp.MustCompile(pattern)
	return rule{re: re, kind: kind, name: re.SubexpIndex("name"), parent: max(0, re.SubexpIndex("parent"))}
}

// defineFunction makes a rule finding functions at the left margin and
// methods when indented; pattern has a group named "indent".
func defineFunction(pattern string) rule {
	r := define(protocol.SymbolKindFunction, pattern)
	r.indent, r.indentedKind = r.re.SubexpIndex("indent"), protocol.SymbolKindMethod
	return r
}

// reserved are words that look like a name to a loose rule but are control
// flow: "if (ready) {" is not a method called if.
var reserved = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true, "else": true,
	"do": true, "try": true, "sizeof": true, "function": true, "typeof": true,
	"await": true, "throw": true, "case": true, "using": true, "lock": true, "foreach": true, "when": true,
}
