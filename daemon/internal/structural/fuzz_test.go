package structural

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// fuzzLanguages are the syntaxes a fuzzer's byte picks from, plain text first.
func fuzzLanguages() []string {
	names := []string{""}
	for name := range languages {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Lexing any text in any language gives tokens in order, inside the text,
// apart from one another, on lines that never go back.
func FuzzLex(f *testing.F) {
	for _, seed := range []string{
		"f(\"a\", 'b') // c", "x = `y\n` /* z", "r#\"a\"# 'a' '\\u{1}'", `R"d(x)d"`, "@\"a\"\"b\"",
		"'''a\n''' \"\"\"", "a#b ${#c} # d", "-- x\n'a''b'", "<!-- x --> y", "\"a\\\nb\"", "\xff\xfe(\n)",
	} {
		f.Add(seed, uint8(0))
		f.Add(seed, uint8(7))
	}
	names := fuzzLanguages()
	f.Fuzz(func(t *testing.T, text string, pick uint8) {
		language := names[int(pick)%len(names)]
		tokens := lex([]byte(text), syntaxOf(language))
		end, line := 0, 0
		for _, tok := range tokens {
			if tok.start < end || tok.end <= tok.start || tok.end > len(text) {
				t.Fatalf("%s: token %+v after byte %d of %d", language, tok, end, len(text))
			}
			if tok.line < line || tok.line != strings.Count(text[:tok.start], "\n") {
				t.Fatalf("%s: token %+v on line %d, counted %d", language, tok, tok.line, strings.Count(text[:tok.start], "\n"))
			}
			end, line = tok.end, tok.line
		}
		partner := partners(tokens, []byte(text))
		for i, j := range partner {
			if j >= 0 && (j <= i || tokens[i].kind != opening || tokens[j].kind != closing) {
				t.Fatalf("%s: token %d paired with %d", language, i, j)
			}
		}
	})
}

// Matching any template against any text never panics, and what it finds
// is in order, apart, inside the text, and holds every word of the
// template, which is what makes narrowing by those words sound.
func FuzzFind(f *testing.F) {
	for _, seed := range []struct{ template, text string }{
		{"retry(:[call], attempts=:[n])", "retry(\n  f(x), attempts=3)"},
		{"if :[x] == :[x] {", "if a == a { } if b == c {"},
		{"f(:[_], :[b])", "f(1, 2, 3) f()"},
		{"a == :[b];", "a = = 1; a == 2;"},
		{`log("x", :[e])`, `log("x", y) LOG("X", z)`},
		{"= :[v];", "x = (1,\n2);\ny = z\n;"},
		{"} else {", "} else {\n}"},
	} {
		f.Add(seed.template, seed.text, uint8(0), false)
		f.Add(seed.template, seed.text, uint8(9), true)
	}
	names := fuzzLanguages()
	f.Fuzz(func(t *testing.T, source, text string, pick uint8, ignoreCase bool) {
		tmpl, err := Parse(source)
		if err != nil {
			return
		}
		language := names[int(pick)%len(names)]
		found, _ := tmpl.Find([]byte(text), language, ignoreCase, -1)
		end := 0
		for _, m := range found {
			if m.Start < end || m.End <= m.Start || m.End > len(text) {
				t.Fatalf("match %+v after byte %d of %d", m, end, len(text))
			}
			end = m.End
			for _, r := range m.Code {
				if r.Start < m.Start || r.End > m.End || r.End <= r.Start {
					t.Fatalf("code %+v outside match %+v", r, m)
				}
			}
			for _, h := range m.Holes {
				if h.Start < m.Start || h.End > m.End || h.End <= h.Start || h.Number < 1 || h.Number > len(tmpl.Holes()) || tmpl.Holes()[h.Number-1] != h.Name {
					t.Fatalf("hole %+v of %q outside match %+v", h, source, m)
				}
			}
			got := []byte(text[m.Start:m.End])
			for _, word := range append(wordsIn(tmpl, language), tmpl.Words()...) {
				if !containsWord(got, []byte(word), ignoreCase) {
					t.Fatalf("match %q of %q in %s lacks %q", got, source, language, word)
				}
			}
		}
	})
}

// wordsIn returns the template's words and strings as language reads it:
// for plain text, Words.
func wordsIn(tmpl *Template, language string) []string {
	c := tmpl.compile(language)
	var words []string
	for i, tok := range c.tokens {
		if tok.kind == word || tok.kind == str {
			words = append(words, c.text(i))
		}
	}
	return words
}

func containsWord(text, word []byte, ignoreCase bool) bool {
	if ignoreCase {
		return bytes.Contains(bytes.ToLower(text), bytes.ToLower(word)) || bytes.Contains(bytes.ToUpper(text), bytes.ToUpper(word))
	}
	return bytes.Contains(text, word)
}
