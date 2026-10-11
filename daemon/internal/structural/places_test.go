package structural

import (
	"slices"
	"testing"
)

// shownPlaces returns each place Places finds as its kind and text:
// "c:// x" for a comment, `s:"y"` for a string.
func shownPlaces(text, language string) []string {
	var got []string
	for _, p := range Places([]byte(text), language) {
		kind := "s:"
		if p.Kind == Comment {
			kind = "c:"
		}
		got = append(got, kind+text[p.Start:p.End])
	}
	return got
}

// Places reads comments and strings by each language's rules, as struct:
// does, so in: can tell where each match is.
func TestPlacesReadsCommentsAndStrings(t *testing.T) {
	tests := []struct {
		language, text string
		want           []string
	}{
		{"go", "f(\"a\") // b\nx := `c\nd` /* e\n*/", []string{`s:"a"`, "c:// b", "s:`c\nd`", "c:/* e\n*/"}},
		{"typescript", "log(`retry ${n}`); // retry", []string{"s:`retry ${n}`", "c:// retry"}},
		{"python", "x = 'a' # b", []string{"s:'a'", "c:# b"}},
		{"shell", "echo ${#a} # b", []string{"c:# b"}},
		{"sql", "SELECT 'it''s' -- x", []string{"s:'it''s'", "c:-- x"}},
		{"html", `<p class="x"><!-- y --></p>`, []string{"c:<!-- y -->"}},
		{"go", "/* never closed\nx", []string{"c:/* never closed\nx"}},
		{"go", `x := "never closed`, nil},
		{"rust", `fn f<'a>(s: &'a str) -> char { 'x' }`, []string{"s:'x'"}},
	}
	for _, test := range tests {
		if got := shownPlaces(test.text, test.language); !slices.Equal(got, test.want) {
			t.Errorf("Places(%q, %s) = %q, want %q", test.text, test.language, got, test.want)
		}
	}
}

// A Python string in triple quotes that starts its line is a docstring,
// which people read as a comment; one inside an expression stays a string.
func TestPlacesReadsDocstringsAsComments(t *testing.T) {
	text := "def f():\n    \"\"\"Retry a call.\"\"\"\n    s = '''text'''\n    '''Block\n    note'''\n"
	want := []string{`c:"""Retry a call."""`, "s:'''text'''", "c:'''Block\n    note'''"}
	if got := shownPlaces(text, "python"); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	// Only Python has docstrings.
	if got := shownPlaces("\"\"\"a\"\"\"", "kotlin"); !slices.Equal(got, []string{`s:"""a"""`}) {
		t.Errorf("kotlin: got %q", got)
	}
}

// Plain text and Markdown are prose, with nothing to tell apart; a file
// in a language without rules for comments and strings is all code.
func TestPlacesReadsProseAndUnknownLanguagesAsCode(t *testing.T) {
	for _, language := range []string{"markdown", "text"} {
		if !IsProse(language) || shownPlaces("Retry notes <!-- x --> \"y\"", language) != nil {
			t.Errorf("%s: not prose, or has places", language)
		}
	}
	if IsProse("python") || IsProse("") {
		t.Error("python or no language read as prose")
	}
	if got := shownPlaces(`<a x="retry"/> <!-- y -->`, ""); got != nil {
		t.Errorf("no language: got %q", got)
	}
}

// Reading places leaves struct:'s tokens as they were.
func TestPlacesKeepsLexingTheSame(t *testing.T) {
	text := []byte("f(\"a\") // b")
	before := lex(text, syntaxOf("go"))
	Places(text, "go")
	if after := lex(text, syntaxOf("go")); !slices.Equal(before, after) {
		t.Errorf("lex changed: %v, then %v", before, after)
	}
}
