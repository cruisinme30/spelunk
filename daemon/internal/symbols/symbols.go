// Package symbols finds the definitions in a source file (classes,
// interfaces, functions, methods and types) for sym: and for the outline
// of a file preview.
//
// It reads one line at a time with a few regular expressions per language,
// as ctags does for most languages. That misses definitions split across
// lines and can't tell a nested function from a method in every language,
// but it needs no parser and no external tool, and it is fast enough to run
// on every file the index reads.
package symbols

import (
	"bytes"
	"regexp"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// Symbol is one definition.
type Symbol struct {
	Name string
	Kind protocol.SymbolKind
	Line int // 1-based
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
	number := 0
	for rest := content; len(rest) > 0; {
		number++
		line := rest
		if end := bytes.IndexByte(rest, '\n'); end >= 0 {
			line, rest = rest[:end], rest[end+1:]
		} else {
			rest = nil
		}
		if len(line) > maxLineBytes {
			continue
		}
		squeezed = squeezeSpace(squeezed[:0], line)
		if symbol, ok := match(rules, squeezed); ok {
			symbol.Line = number
			found = append(found, symbol)
		}
	}
	return found
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

// match applies the first rule that matches line.
func match(rules []rule, line []byte) (Symbol, bool) {
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
		return Symbol{Name: name, Kind: kind}, true
	}
	return Symbol{}, false
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
}

// define makes a rule finding kind with pattern.
func define(kind protocol.SymbolKind, pattern string) rule {
	re := regexp.MustCompile(pattern)
	return rule{re: re, kind: kind, name: re.SubexpIndex("name")}
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
