package structural

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Anonymous is the hole name that takes anything and is never compared.
const Anonymous = "_"

// Template is a parsed template: code with holes.
type Template struct {
	source string
	parts  []part
	// names are the template's named holes, each once, in the order they
	// first appear; a hole's number is its place here plus one.
	names []string
	// compiled is the template read by each language's syntax, made the
	// first time a file in that language is matched; searches match files
	// side by side, so mu guards it.
	mu       sync.Mutex
	compiled map[string]*compiled
}

// part is a run of the template's code, or one of its holes.
type part struct {
	code string
	name string // the hole's name, when isHole
	// isHole is set for :[name].
	isHole bool
}

// Parse reads a template, or says what is wrong with it in a sentence a
// person can act on.
func Parse(source string) (*Template, error) {
	t := &Template{source: source}
	rest := source
	for rest != "" {
		at := strings.Index(rest, ":[")
		if at < 0 {
			t.parts = append(t.parts, part{code: rest})
			break
		}
		if at > 0 {
			t.parts = append(t.parts, part{code: rest[:at]})
		}
		end := strings.IndexByte(rest[at:], ']')
		if end < 0 {
			return nil, errors.New("a hole opens with :[ but never closes with ]")
		}
		name := rest[at+2 : at+end]
		if !isHoleName(name) {
			return nil, fmt.Errorf("%q isn't a hole: a hole is :[ then a name of letters, digits and _, then ], as in :[args]", rest[at:at+end+1])
		}
		t.parts = append(t.parts, part{name: name, isHole: true})
		if name != Anonymous && !slices.Contains(t.names, name) {
			t.names = append(t.names, name)
		}
		rest = rest[at+end+1:]
	}
	if err := t.check(); err != nil {
		return nil, err
	}
	return t, nil
}

// isHoleName reports whether name is _ or starts with a letter or _ and
// goes on with letters, digits and _.
func isHoleName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		letter := 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || r == '_'
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// check says what makes the template unable to match sensibly: nothing
// but holes, a hole first or last (it would take nothing, or everything
// before), or two holes with nothing between them (no way to tell where
// one ends). Its brackets needn't pair up: if err != nil { is a template.
func (t *Template) check() error {
	code := t.trimmed()
	if !slices.ContainsFunc(code, func(p part) bool { return !p.isHole }) {
		return errors.New("a template needs some code, as in retry(:[args])")
	}
	if code[0].isHole {
		return errors.New("a template can't start with a hole: begin it with code, as in retry(:[args])")
	}
	if code[len(code)-1].isHole {
		return errors.New("a template can't end with a hole: end it with code, as in retry(:[args])")
	}
	for i := 1; i < len(code); i++ {
		if code[i].isHole && code[i-1].isHole {
			return fmt.Errorf("two holes in a row can't tell where one ends: put code between :[%s] and :[%s]", code[i-1].name, code[i].name)
		}
	}
	return nil
}

// trimmed returns the template's parts without those that are only space,
// which can't separate two holes or start a template.
func (t *Template) trimmed() []part {
	var kept []part
	for _, p := range t.parts {
		if p.isHole || strings.TrimSpace(p.code) != "" {
			kept = append(kept, p)
		}
	}
	return kept
}

// String returns the template as written.
func (t *Template) String() string { return t.source }

// Holes returns the template's named holes, each once, in the order they
// first appear; :[_] is not among them.
func (t *Template) Holes() []string { return t.names }

// Words returns the words and strings of the template that every match
// has in it, as written (or in another case, when case is ignored), in
// whatever language: a file without one of them can't match, so the index
// can skip it. A word some language reads as part of a comment isn't one,
// and neither is a word a language can't match the template in.
func (t *Template) Words() []string {
	plain := t.compile("")
	var words []string
	for i, tok := range plain.tokens {
		if (tok.kind == word || tok.kind == str) && t.everyLanguageHas(plain.text(i)) {
			words = append(words, plain.text(i))
		}
	}
	return words
}

// everyLanguageHas reports whether the template, as each language reads
// it, has a word or string containing w.
func (t *Template) everyLanguageHas(w string) bool {
	for name := range languages {
		c := t.compile(name)
		if c.never {
			continue
		}
		has := false
		for i, tok := range c.tokens {
			if (tok.kind == word || tok.kind == str) && strings.Contains(c.text(i), w) {
				has = true
				break
			}
		}
		if !has {
			return false
		}
	}
	return true
}

// compiled is a template read by one language's syntax: its tokens, with
// the holes among them.
type compiled struct {
	src    []byte // the template's code, holes written as a single space
	tokens []token
	// name is the hole name of each hole token ("" for the others).
	name []string
	// number is the 1-based number of each named hole token's name, 0 for
	// :[_] and the others.
	number []int
	// depth is how many brackets of the template each token is inside.
	depth []int
	// never is set when this language's syntax reads the template so that
	// it can't match: it starts or ends with a hole, or has two in a row,
	// once a comment marker in it hides some of its code.
	never bool
}

func (c *compiled) text(i int) string {
	return string(c.src[c.tokens[i].start:c.tokens[i].end])
}

// compile reads the template by language's syntax, once per language.
func (t *Template) compile(language string) *compiled {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.compiled[language]; ok {
		return c
	}
	c := t.read(syntaxOf(language))
	c.depth = nesting(c.tokens)
	c.never = neverMatches(c.tokens)
	if t.compiled == nil {
		t.compiled = map[string]*compiled{}
	}
	t.compiled[language] = c
	return c
}

// read lexes the template's code by one syntax, with a hole token for
// each hole.
func (t *Template) read(s syntax) *compiled {
	c := &compiled{}
	var b strings.Builder
	for _, p := range t.parts {
		if p.isHole {
			// A hole is a gap in the code: the code on its two sides is
			// read apart, and isn't joined across it.
			c.tokens = append(c.tokens, token{kind: hole, start: b.Len(), end: b.Len()})
			c.name = append(c.name, p.name)
			c.number = append(c.number, holeNumber(t.names, p.name))
			b.WriteByte(' ')
			continue
		}
		base := b.Len()
		b.WriteString(p.code)
		for _, tok := range lex([]byte(p.code), s) {
			tok.start, tok.end = tok.start+base, tok.end+base
			if len(c.tokens) > 0 && c.tokens[len(c.tokens)-1].kind == hole {
				tok.joined = false
			}
			c.tokens = append(c.tokens, tok)
			c.name = append(c.name, "")
			c.number = append(c.number, 0)
		}
	}
	c.src = []byte(b.String())
	return c
}

// nesting returns how many brackets each token is inside.
func nesting(tokens []token) []int {
	depths := make([]int, len(tokens))
	depth := 0
	for i, tok := range tokens {
		if tok.kind == closing && depth > 0 {
			depth--
		}
		depths[i] = depth
		if tok.kind == opening {
			depth++
		}
	}
	return depths
}

// neverMatches reports whether tokens start or end with a hole, or have
// two in a row: what Parse rules out, but a language's comments can bring
// back by hiding code.
func neverMatches(tokens []token) bool {
	if len(tokens) == 0 || tokens[0].kind == hole || tokens[len(tokens)-1].kind == hole {
		return true
	}
	for i := 1; i < len(tokens); i++ {
		if tokens[i].kind == hole && tokens[i-1].kind == hole {
			return true
		}
	}
	return false
}

func holeNumber(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i + 1
		}
	}
	return 0
}
