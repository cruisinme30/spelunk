package structural

import (
	"slices"
	"strings"
	"testing"
)

// texts returns the text of each token lex finds, strings marked with «».
func texts(text, language string) []string {
	var got []string
	for _, tok := range lex([]byte(text), syntaxOf(language)) {
		shown := text[tok.start:tok.end]
		if tok.kind == str {
			shown = "«" + shown + "»"
		}
		got = append(got, shown)
	}
	return got
}

// Each language's strings are one token and its comments are no token, so
// neither can hold a match of the template's code.
func TestLexReadsEachLanguagesStringsAndComments(t *testing.T) {
	tests := []struct {
		language, text string
		want           []string
	}{
		{"go", `f("a(b", 'c') // g(h)`, []string{"f", "(", `«"a(b"»`, ",", "«'c'»", ")"}},
		{"go", "x := `raw\n(` /* y(\n */ z", []string{"x", ":", "=", "«`raw\n(`»", "z"}},
		{"go", `s := "a\"b" + "\\"`, []string{"s", ":", "=", `«"a\"b"»`, "+", `«"\\"»`}},
		{"python", `f('it''s', "x") # g(`, []string{"f", "(", "«'it'»", "«'s'»", ",", `«"x"»`, ")"}},
		{"python", "s = \"\"\"a\n(b\"\"\" + '''c'''", []string{"s", "=", "«\"\"\"a\n(b\"\"\"»", "+", "«'''c'''»"}},
		{"python", `x = f"{a}" + b'y' + ""`, []string{"x", "=", "f", `«"{a}"»`, "+", "b", "«'y'»", "+", `«""»`}},
		{"javascript", "a = `x${b}\n)` + 'c' // d", []string{"a", "=", "«`x${b}\n)`»", "+", "«'c'»"}},
		{"typescript", `const s = 'don\'t'`, []string{"const", "s", "=", `«'don\'t'»`}},
		{"rust", `fn f<'a>(x: &'a str) -> char { 'x' }`, []string{"fn", "f", "<", "'", "a", ">", "(", "x", ":", "&", "'", "a", "str", ")", "-", ">", "char", "{", "«'x'»", "}"}},
		{"rust", `let c = '\n'; let u = '\u{1F600}';`, []string{"let", "c", "=", `«'\n'»`, ";", "let", "u", "=", `«'\u{1F600}'»`, ";"}},
		{"rust", `r"a(b" + r#"c"d"# + br"e"`, []string{`«r"a(b"»`, "+", `«r#"c"d"#»`, "+", `«br"e"»`}},
		{"rust", "r + rx", []string{"r", "+", "rx"}},
		{"c++", `auto s = R"x(a)"b)x" + R"(c)";`, []string{"auto", "s", "=", `«R"x(a)"b)x"»`, "+", `«R"(c)"»`, ";"}},
		{"c", `char c = '\''; /* ( */ int`, []string{"char", "c", "=", `«'\''»`, ";", "int"}},
		{"csharp", `var p = @"C:\dir\""x" + """a"b""";`, []string{"var", "p", "=", `«@"C:\dir\""x"»`, "+", `«"""a"b"""»`, ";"}},
		{"kotlin", "val s = \"\"\"\n(\"\"\"", []string{"val", "s", "=", "«\"\"\"\n(\"\"\"»"}},
		{"java", `String s = "x"; char c = 'y';`, []string{"String", "s", "=", `«"x"»`, ";", "char", "c", "=", "«'y'»", ";"}},
		{"shell", "echo 'it''s' \"$x\" # (\necho ${#a} a#b", []string{"echo", "«'it''s'»", `«"$x"»`, "echo", "$", "{", "#", "a", "}", "a", "#", "b"}},
		{"sql", "SELECT 'it''s' -- (\nFROM \"t\"\"x\" /* ) */", []string{"SELECT", "«'it''s'»", "FROM", `«"t""x"»`}},
		{"php", "f('a') # g(\n/* ( */ h(\"b\") // i(", []string{"f", "(", "«'a'»", ")", "h", "(", `«"b"»`, ")"}},
		{"ruby", `puts "a(" # b(`, []string{"puts", `«"a("»`}},
		{"yaml", `key: "v(" # c(`, []string{"key", ":", `«"v("»`}},
		{"toml", "a = '''x\n(''' # y", []string{"a", "=", "«'''x\n('''»"}},
		{"css", `a { content: "(" } /* ) */`, []string{"a", "{", "content", ":", `«"("»`, "}"}},
		{"json", `{"a": [1, "]"]} // (`, []string{"{", `«"a"»`, ":", "[", "1", ",", `«"]"»`, "]", "}"}},
		{"html", `<p class="x">don't (<!-- ) --></p>`, []string{"<", "p", "class", "=", `"`, "x", `"`, ">", "don", "'", "t", "(", "<", "/", "p", ">"}},
		{"markdown", "it's a (note)", []string{"it", "'", "s", "a", "(", "note", ")"}},
		{"make", "all: $(OBJ) # (", []string{"all", ":", "$", "(", "OBJ", ")"}},
		{"", `don't "a(" b`, []string{"don", "'", "t", `«"a("»`, "b"}},
	}
	for _, test := range tests {
		if got := texts(test.text, test.language); !slices.Equal(got, test.want) {
			t.Errorf("lex %s %q\n got %q\nwant %q", test.language, test.text, got, test.want)
		}
	}
}

// A quote that doesn't close on its line is punctuation: one apostrophe in
// a comment-less language, or a typo, mustn't hide the rest of the file.
func TestLexLeavesAnUnclosedQuoteAsPunctuation(t *testing.T) {
	tests := []struct {
		language, text string
		want           []string
	}{
		{"python", "x = 'abc\ny = f(1)", []string{"x", "=", "'", "abc", "y", "=", "f", "(", "1", ")"}},
		{"go", "s := \"abc\nt", []string{"s", ":", "=", `"`, "abc", "t"}},
		{"go", "s := `abc", []string{"s", ":", "=", "`", "abc"}},
		{"python", `x = """abc`, []string{"x", "=", `"`, `«""»`, "abc"}},
		{"sql", "x = 'abc", []string{"x", "=", "'", "abc"}},
		{"rust", "r#\"abc", []string{"r", "#", `"`, "abc"}},
		{"c++", `R"x(abc`, []string{"R", `"`, "x", "(", "abc"}},
		{"c", "'ab'", []string{"'", "ab", "'"}},
	}
	for _, test := range tests {
		if got := texts(test.text, test.language); !slices.Equal(got, test.want) {
			t.Errorf("lex %s %q\n got %q\nwant %q", test.language, test.text, got, test.want)
		}
	}
}

// A backslash before a line end continues a one-line string onto the next.
func TestLexContinuesAStringPastAnEscapedLineEnd(t *testing.T) {
	got := texts("x = \"a\\\nb\" + c", "c")
	want := []string{"x", "=", "«\"a\\\nb\"»", "+", "c"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Tokens know their line, counted past strings and comments that span
// lines, and whether they touch the token before.
func TestLexCountsLinesAndJoins(t *testing.T) {
	text := "a == b\n/* x\ny */ c = = d\r\n\"e\nf\" g"
	tokens := lex([]byte(text), syntaxOf("go"))
	type seen struct {
		text   string
		line   int
		joined bool
	}
	var got []seen
	for _, tok := range tokens {
		got = append(got, seen{text[tok.start:tok.end], tok.line, tok.joined})
	}
	want := []seen{
		{"a", 0, false}, {"=", 0, false}, {"=", 0, true}, {"b", 0, false},
		{"c", 2, false}, {"=", 2, false}, {"=", 2, false}, {"d", 2, false},
		{`"`, 3, false}, {"e", 3, true}, {"f", 4, false}, {`"`, 4, true}, {"g", 4, false},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

// Words take letters and digits of any script, with _; other characters
// are one punctuation token each.
func TestLexReadsUnicodeWords(t *testing.T) {
	got := texts("naïve_1 = café·π + 東京", "python")
	want := []string{"naïve_1", "=", "café", "·", "π", "+", "東京"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// partners pairs brackets by kind, leaving a stray closer out of it.
func TestPartnersPairBrackets(t *testing.T) {
	text := "f(a[b) {c}] (d"
	tokens := lex([]byte(text), plainText)
	partner := partners(tokens, []byte(text))
	var pairs []string
	for i, j := range partner {
		if j >= 0 {
			pairs = append(pairs, text[tokens[i].start:tokens[j].end])
		}
	}
	want := []string{"[b) {c}]", "{c}"}
	if !slices.Equal(pairs, want) {
		t.Errorf("pairs = %q, want %q", pairs, want)
	}
}

// An unclosed block comment runs to the end of the text.
func TestLexEndsAnUnclosedCommentAtTheEnd(t *testing.T) {
	if got := texts("a /* b(\nc", "go"); !slices.Equal(got, []string{"a"}) {
		t.Errorf("got %q", got)
	}
	if got := strings.Join(texts("a <!-- b", "html"), " "); got != "a" {
		t.Errorf("html got %q", got)
	}
}
