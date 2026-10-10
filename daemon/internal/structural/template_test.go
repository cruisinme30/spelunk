package structural

import (
	"slices"
	"strings"
	"testing"
)

// A template is code with :[name] holes; Parse keeps the names in the
// order they first appear, once each, and leaves :[_] out.
func TestParseKeepsHoleNamesInOrder(t *testing.T) {
	tests := map[string][]string{
		"retry(:[call], attempts=:[n])": {"call", "n"},
		"foo(:[a], :[a])":               {"a"},
		"foo(:[_], :[x], :[_])":         {"x"},
		"if err != nil { return err }":  nil,
		"f(:[A_1], :[b2])":              {"A_1", "b2"},
		"a[:[i]] = a[:[j]]":             {"i", "j"},
		"x :[_y] y":                     {"_y"},
		"log.Printf(\":[x]\")":          {"x"}, // a hole is a hole even in quotes
	}
	for source, want := range tests {
		tmpl, err := Parse(source)
		if err != nil {
			t.Errorf("Parse(%q): %v", source, err)
			continue
		}
		if got := tmpl.Holes(); !slices.Equal(got, want) {
			t.Errorf("Parse(%q).Holes() = %q, want %q", source, got, want)
		}
		if tmpl.String() != source {
			t.Errorf("Parse(%q).String() = %q", source, tmpl.String())
		}
	}
}

// Parse says, in words a person can act on, what keeps a template from
// matching sensibly.
func TestParseExplainsBadTemplates(t *testing.T) {
	tests := map[string]string{
		"":                       "needs some code",
		"   ":                    "needs some code",
		":[x]":                   "needs some code",
		":[a] :[b]":              "needs some code",
		":[fn](:[args])":         "can't start with a hole",
		"  :[fn](:[args])":       "can't start with a hole",
		"retry(:[args]) :[rest]": "can't end with a hole",
		"f(:[a]:[b])":            "two holes in a row",
		"f(:[a] :[b])":           "two holes in a row",
		"f(:[a":                  "never closes",
		"f(:[])":                 `":[]" isn't a hole`,
		"f(:[1x])":               `":[1x]" isn't a hole`,
		"f(:[a-b])":              `":[a-b]" isn't a hole`,
		"f(:[a b])":              `":[a b]" isn't a hole`,
	}
	for source, want := range tests {
		_, err := Parse(source)
		if err == nil {
			t.Errorf("Parse(%q) = nil error, want one saying %q", source, want)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %q, want it to say %q", source, err, want)
		}
	}
}

// A template's brackets needn't pair up: if err != nil { is a fine
// template, and so is a bracket in a string.
func TestParseAllowsUnpairedBrackets(t *testing.T) {
	for _, source := range []string{"if :[c] {", "} else {", "x) + f(:[a])", `print("(", :[x])`, `f(:[x], "[{")`} {
		if _, err := Parse(source); err != nil {
			t.Errorf("Parse(%q): %v", source, err)
		}
	}
}

// Words lists every word and string of the template: a file must have all
// of them to match, so the index can narrow the files to read.
func TestWordsAreTheTemplatesWordsAndStrings(t *testing.T) {
	tests := map[string][]string{
		"retry(:[call], attempts=:[n])": {"retry", "attempts"},
		`log.error("failed: %s", :[e])`: {"log", "error"}, // HTML and Markdown have no strings
		"a == :[b] && c":                {"a", "c"},
		"x[:[i]]":                       {"x"},
		"f(:[a]) # why":                 {"f"},        // a comment in Python and others
		"x = 'abc' + :[y];":             {"x", "abc"}, // a string in some languages, words in others
		"g(:[a]) -- note":               {"g"},        // a comment in SQL, Lua and Haskell
		`h(:[a]) // "c"`:                {"h"},        // a comment in C and the like
	}
	for source, want := range tests {
		tmpl, err := Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.Words(); !slices.Equal(got, want) {
			t.Errorf("Parse(%q).Words() = %q, want %q", source, got, want)
		}
	}
}

// Every match has each of the template's words in it: that's what makes
// narrowing by them sound.
func TestEveryMatchHoldsTheTemplatesWords(t *testing.T) {
	tmpl := mustParse(t, `log.error("x", :[e])`)
	text := `log.error("x", err)
LOG.Error("x",
    other)
log . error ( "x" , third )`
	for _, ignoreCase := range []bool{false, true} {
		found, _ := tmpl.Find([]byte(text), "python", ignoreCase, -1)
		for _, match := range found {
			got := text[match.Start:match.End]
			for _, word := range tmpl.Words() {
				have := got
				if ignoreCase {
					have, word = strings.ToLower(have), strings.ToLower(word)
				}
				if !strings.Contains(have, word) {
					t.Errorf("match %q lacks the word %q", got, word)
				}
			}
		}
	}
}

func mustParse(tb testing.TB, source string) *Template {
	tb.Helper()
	tmpl, err := Parse(source)
	if err != nil {
		tb.Fatalf("Parse(%q): %v", source, err)
	}
	return tmpl
}
