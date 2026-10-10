package structural

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

// Match is one place a template matched, in bytes of the text.
type Match struct {
	Start, End int
	// Code are the parts the template's own code matched, joined where
	// only spaces on one line part them.
	Code []Range
	// Holes are what each named hole took, in template order; a hole that
	// took nothing, and every :[_], is left out.
	Holes []Hole
}

// Range is a run of bytes, from Start up to End.
type Range struct{ Start, End int }

// Hole is the code a named hole took.
type Hole struct {
	Name string
	// Number is the hole's place among the template's names, from 1 (see
	// Template.Holes); a name used twice has one number.
	Number     int
	Start, End int
}

// maxSteps bounds the work of one Find. A template whose holes could split
// a long line many ways may need more; Find then stops and says so.
const maxSteps = 2_000_000

// Find returns the template's matches in text, a file in language (a lang
// package name, or "" for plain text): leftmost first and never
// overlapping, at most limit of them (all when limit is negative). Words
// and strings match in any case when ignoreCase. complete is false when
// Find gave up before the end of text because the template was taking too
// long, as it can when holes may split a long line in very many ways.
func (t *Template) Find(text []byte, language string, ignoreCase bool, limit int) (found []Match, complete bool) {
	c := t.compile(language)
	if c.never {
		return nil, true
	}
	tokens := lex(text, syntaxOf(language))
	m := &matcher{
		c: c, text: text, tokens: tokens, partner: partners(tokens, text), ignoreCase: ignoreCase,
		at: make([]int, len(c.tokens)), took: make([][2]int, len(c.tokens)), first: firstOfName(c),
	}
	for start := 0; start < len(tokens) && (limit < 0 || len(found) < limit); {
		if !m.same(0, start) {
			start++
			continue
		}
		if m.match(0, start) {
			found = append(found, m.result(start))
			start = m.end
			continue
		}
		if m.out {
			return found, false
		}
		start++
	}
	return found, true
}

// firstOfName returns, for each hole token, the index of the first hole
// token with its name: a later one must take the same code.
func firstOfName(c *compiled) []int {
	first := make([]int, len(c.tokens))
	seen := map[string]int{}
	for i, name := range c.name {
		first[i] = i
		if c.tokens[i].kind != hole || name == Anonymous {
			continue
		}
		if j, ok := seen[name]; ok {
			first[i] = j
		} else {
			seen[name] = i
		}
	}
	return first
}

// matcher matches one compiled template against one text's tokens.
type matcher struct {
	c          *compiled
	text       []byte
	tokens     []token
	partner    []int
	ignoreCase bool
	first      []int
	// at is the text token each template code token matched, and took the
	// text tokens each hole took, from and up to; both describe the match
	// being tried.
	at   []int
	took [][2]int
	// end is the text token after the last match found.
	end   int
	steps int
	// out is set when the steps ran out.
	out bool
}

// match reports whether the template from token ti on matches the text
// from token at on, recording what it matched.
func (m *matcher) match(ti, at int) bool {
	if m.spend() {
		return false
	}
	if ti == len(m.c.tokens) {
		m.end = at
		return true
	}
	if m.c.tokens[ti].kind != hole {
		if at >= len(m.tokens) || !m.same(ti, at) || !m.joinedLike(ti, at) {
			return false
		}
		m.at[ti] = at
		return m.match(ti+1, at+1)
	}
	if j := m.first[ti]; j != ti {
		return m.again(ti, at, m.took[j])
	}
	return m.fill(ti, at)
}

// spend counts one step, reporting whether the steps have run out.
func (m *matcher) spend() bool {
	m.steps++
	if m.steps > maxSteps {
		m.out = true
	}
	return m.out
}

// fill tries hole ti on ever longer runs of text from token at (see
// grow), each time looking for the template's code after it there. A hole
// is never the last token, so the template token after it is code.
func (m *matcher) fill(ti, at int) bool {
	line := -1
	if at < len(m.tokens) {
		line = m.tokens[at].line
	}
	for end := at; ; {
		if end < len(m.tokens) && m.same(ti+1, end) {
			m.took[ti] = [2]int{at, end}
			if m.match(ti+1, end) {
				return true
			}
			if m.out {
				return false
			}
		}
		var ok bool
		if end, ok = m.grow(ti, end, line); !ok || m.spend() {
			return false
		}
	}
}

// grow returns where hole ti's run of text ends if it also takes the text
// at token end: one more token, or one more bracketed group, so a hole's
// brackets always pair up. ok is false when it can't: at the end of the
// text, at a closing bracket that closes something before the hole, or,
// outside the template's brackets, at a token on a later line than line,
// the one the hole starts on.
func (m *matcher) grow(ti, end, line int) (next int, ok bool) {
	if end >= len(m.tokens) {
		return end, false
	}
	tok := m.tokens[end]
	switch {
	case tok.kind == closing, m.c.depth[ti] == 0 && tok.line > line:
		return end, false
	case tok.kind == opening && m.partner[end] < 0:
		return end, false
	case tok.kind == opening:
		return m.partner[end] + 1, true
	default:
		return end + 1, true
	}
}

// again matches hole ti, a name used before, to the same code the name
// took before (prior), token by token, ignoring space.
func (m *matcher) again(ti, at int, prior [2]int) bool {
	n := prior[1] - prior[0]
	if at+n > len(m.tokens) {
		return false
	}
	for i := range n {
		a, b := m.tokens[prior[0]+i], m.tokens[at+i]
		if a.kind != b.kind || !m.equal(m.text[a.start:a.end], m.text[b.start:b.end], a.kind) {
			return false
		}
	}
	m.took[ti] = [2]int{at, at + n}
	return m.match(ti+1, at+n)
}

// same reports whether text token at is template code token ti.
func (m *matcher) same(ti, at int) bool {
	want, got := m.c.tokens[ti], m.tokens[at]
	if want.kind != got.kind {
		return false
	}
	return m.equal(m.c.src[want.start:want.end], m.text[got.start:got.end], want.kind)
}

// joinedLike reports whether text token at touches the token before it
// when the template's punctuation does: == must not match = =.
func (m *matcher) joinedLike(ti, at int) bool {
	want := m.c.tokens[ti]
	if !want.joined || want.kind != punct || ti == 0 || m.c.tokens[ti-1].kind != punct {
		return true
	}
	return m.tokens[at].joined
}

// equal compares two tokens' text: words and strings in any case when
// case is ignored, everything else exactly.
func (m *matcher) equal(a, b []byte, k kind) bool {
	if m.ignoreCase && (k == word || k == str) {
		return bytes.EqualFold(a, b)
	}
	return bytes.Equal(a, b)
}

// result turns the match just found, from text token start, into a Match.
func (m *matcher) result(start int) Match {
	found := Match{Start: m.tokens[start].start, End: m.tokens[m.end-1].end}
	for ti, tok := range m.c.tokens {
		if tok.kind != hole {
			t := m.tokens[m.at[ti]]
			found.Code = m.joinCode(found.Code, Range{Start: t.start, End: t.end})
			continue
		}
		took := m.took[ti]
		if number := m.c.number[ti]; number > 0 && took[1] > took[0] {
			found.Holes = append(found.Holes, Hole{
				Name: m.c.name[ti], Number: number,
				Start: m.tokens[took[0]].start, End: m.tokens[took[1]-1].end,
			})
		}
	}
	return found
}

// joinCode adds r to code, joining it to the last range when only spaces
// on one line part them.
func (m *matcher) joinCode(code []Range, r Range) []Range {
	if n := len(code); n > 0 && onlySpaces(m.text[code[n-1].End:r.Start]) {
		code[n-1].End = r.End
		return code
	}
	return append(code, r)
}

// onlySpaces reports whether b is spaces and tabs (or nothing).
func onlySpaces(b []byte) bool {
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == '\n' || r == '\r' || !unicode.IsSpace(r) {
			return false
		}
		b = b[size:]
	}
	return true
}
