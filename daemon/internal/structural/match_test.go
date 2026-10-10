package structural

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// found returns each match's text, with each named hole as name=text.
func found(tb testing.TB, template, text, language string, ignoreCase bool) []string {
	tb.Helper()
	matches, complete := mustParse(tb, template).Find([]byte(text), language, ignoreCase, -1)
	if !complete {
		tb.Fatalf("Find(%q) gave up", template)
	}
	var got []string
	for _, m := range matches {
		shown := text[m.Start:m.End]
		for _, h := range m.Holes {
			shown += fmt.Sprintf(" | %s=%s", h.Name, text[h.Start:h.End])
		}
		got = append(got, shown)
	}
	return got
}

func checkFound(t *testing.T, template, text, language string, want ...string) {
	t.Helper()
	if got := found(t, template, text, language, false); !slices.Equal(got, want) {
		t.Errorf("Find(%q) in %s %q\n got %q\nwant %q", template, language, text, got, want)
	}
}

// The mock's example: holes take whole arguments, a call and all, and a
// match runs over lines when the code does.
func TestHolesTakeBalancedCodeAcrossLines(t *testing.T) {
	text := `def charge(self, body):
    # retry(fn, attempts=N) gives up after N tries
    return retry(
        lambda: self._session.post(url, json=body),
        attempts=self.config.max_attempts)

def refund(self, charge_id):
    return retry(self._refund, charge_id,
                 attempts=2)
`
	checkFound(t, "retry(:[call], attempts=:[n])", text, "python",
		"retry(\n        lambda: self._session.post(url, json=body),\n        attempts=self.config.max_attempts) | call=lambda: self._session.post(url, json=body) | n=self.config.max_attempts",
		"retry(self._refund, charge_id,\n                 attempts=2) | call=self._refund, charge_id | n=2",
	)
}

// Spaces and line breaks don't matter, except between two words.
func TestSpacesDoNotMatterExceptBetweenWords(t *testing.T) {
	checkFound(t, "foo(a,b)", "foo( a ,\n b )", "go", "foo( a ,\n b )")
	checkFound(t, "foo ( a , b )", "foo(a,b)", "go", "foo(a,b)")
	checkFound(t, "go to", "goto; go  to", "go", "go  to")
	checkFound(t, "if x", "ifx; if\tx", "go", "if\tx")
}

// Punctuation written together in the template must be together in the
// code: == is not = =, and -> is not - >.
func TestJoinedPunctuationStaysJoined(t *testing.T) {
	checkFound(t, "a == :[b];", "a = = 1; a == 2; a ==\n3;", "go", "a == 2; | b=2", "a ==\n3; | b=3")
	checkFound(t, "p->:[f];", "p - >x; p->y;", "c", "p->y; | f=y")
	checkFound(t, "a = = b", "a == b; a = = b", "go", "a == b", "a = = b") // apart in the template: either way
}

// Code in strings and comments never matches the template, but a hole may
// take strings and comments whole.
func TestStringsAndCommentsAreNotCode(t *testing.T) {
	text := `// retry(a, attempts=1)
s := "retry(b, attempts=2)"
retry("x)", /* ) */ attempts=3)
`
	checkFound(t, "retry(:[call], attempts=:[n])", text, "go", "retry(\"x)\", /* ) */ attempts=3) | call=\"x)\" | n=3")
}

// The template's strings match the same string, as written.
func TestTemplateStringsMatchWholeStrings(t *testing.T) {
	text := `log.error("failed", err); log.error("failed twice", err); log.error('failed', e)`
	checkFound(t, `log.error("failed", :[e])`, text, "python", `log.error("failed", err) | e=err`)
}

// A hole's brackets always pair up, so a hole never takes half a call.
func TestHolesKeepBracketsPaired(t *testing.T) {
	checkFound(t, "f(:[a])", "f(g(1), h[2], {3})", "go", "f(g(1), h[2], {3}) | a=g(1), h[2], {3}")
	checkFound(t, "f(:[a]) + 1", "f(x) + 2; f((y) + 1", "go")
	checkFound(t, "f(:[a], :[b])", "f(g(1, 2), 3)", "go", "f(g(1, 2), 3) | a=g(1, 2) | b=3")
	checkFound(t, "[:[x]]", "a[[1, 2]]", "python", "[[1, 2]] | x=[1, 2]")
}

// A hole takes the least code it can: the first place the code after it
// matches ends it.
func TestHolesAreLazy(t *testing.T) {
	checkFound(t, "f(:[a], :[b])", "f(1, 2, 3)", "go", "f(1, 2, 3) | a=1 | b=2, 3")
	checkFound(t, "if :[c] {", "if a && b { x } if c {", "go", "if a && b { | c=a && b", "if c { | c=c")
}

// A hole may take nothing.
func TestAHoleMayBeEmpty(t *testing.T) {
	matches, _ := mustParse(t, "f(:[args])").Find([]byte("f() + f(1)"), "go", false, -1)
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	if len(matches[0].Holes) != 0 {
		t.Errorf("an empty hole is listed: %+v", matches[0].Holes)
	}
	if len(matches[1].Holes) != 1 {
		t.Errorf("a full hole isn't listed: %+v", matches[1].Holes)
	}
}

// A name used twice must take the same code both times, spaces aside;
// :[_] takes anything every time.
func TestRepeatedNamesMatchEqualCode(t *testing.T) {
	text := "if a.b == a.b {} if a.b == a.c {} if f( x ) == f(x) {}"
	checkFound(t, "if :[x] == :[x] {", text, "go", "if a.b == a.b { | x=a.b | x=a.b", "if f( x ) == f(x) { | x=f( x ) | x=f(x)")
	checkFound(t, "if :[_] == :[_] {", text, "go", "if a.b == a.b {", "if a.b == a.c {", "if f( x ) == f(x) {")
}

// Outside the template's brackets a hole stays on its line, so a template
// whose end never comes can't take the rest of the file.
func TestATopLevelHoleStaysOnItsLine(t *testing.T) {
	text := "x = foo\nbar;\ny = (1,\n 2);"
	checkFound(t, "= :[v];", text, "go", "= (1,\n 2); | v=(1,\n 2)")
	// It may start on a later line than the code before it.
	checkFound(t, "return :[x];", "return\n  a;", "go", "return\n  a; | x=a")
	// Inside the template's brackets a hole runs over lines.
	checkFound(t, "f(:[a])", "f(1,\n2,\n3)", "go", "f(1,\n2,\n3) | a=1,\n2,\n3")
}

// The code after a top-level hole may sit on a later line; only what the
// hole takes stays on the line.
func TestTheCodeAfterATopLevelHoleMayBeOnALaterLine(t *testing.T) {
	checkFound(t, "return :[x];", "return a\n;", "go", "return a\n; | x=a")
}

// Words and strings match in any case when case is ignored; punctuation
// is the same either way.
func TestIgnoreCaseFoldsWordsAndStrings(t *testing.T) {
	text := `LOG.Error("X", e)`
	if got := found(t, `log.error("x", :[e])`, text, "go", false); len(got) != 0 {
		t.Errorf("case-sensitive got %q", got)
	}
	if got := found(t, `log.error("x", :[e])`, text, "go", true); !slices.Equal(got, []string{`LOG.Error("X", e) | e=e`}) {
		t.Errorf("ignoring case got %q", got)
	}
}

// Matches don't overlap; the leftmost wins, and a match inside another
// is part of it.
func TestMatchesDoNotOverlap(t *testing.T) {
	checkFound(t, "f(:[a])", "f(f(1)) f(2)", "go", "f(f(1)) | a=f(1)", "f(2) | a=2")
}

// limit stops Find after that many matches.
func TestFindStopsAtTheLimit(t *testing.T) {
	matches, complete := mustParse(t, "f(:[a])").Find([]byte("f(1) f(2) f(3)"), "go", false, 2)
	if len(matches) != 2 || !complete {
		t.Errorf("got %d matches, complete %v; want 2, true", len(matches), complete)
	}
}

// Code ranges cover the template's own code, joined across spaces on a
// line, never across a line break or a hole.
func TestCodeRangesCoverTheTemplatesCode(t *testing.T) {
	text := "retry(\n  fn, attempts = 3)"
	matches, _ := mustParse(t, "retry(:[call], attempts=:[n])").Find([]byte(text), "python", false, -1)
	if len(matches) != 1 {
		t.Fatalf("got %d matches", len(matches))
	}
	var code []string
	for _, r := range matches[0].Code {
		code = append(code, text[r.Start:r.End])
	}
	if want := []string{"retry(", ", attempts =", ")"}; !slices.Equal(code, want) {
		t.Errorf("code = %q, want %q", code, want)
	}
	holes := matches[0].Holes
	if len(holes) != 2 || holes[0].Number != 1 || holes[1].Number != 2 {
		t.Errorf("holes = %+v, want numbers 1 and 2", holes)
	}
}

// A template with no holes finds code whatever its spacing, outside
// strings and comments.
func TestATemplateWithoutHolesFindsCodeAnySpacing(t *testing.T) {
	checkFound(t, "if err != nil {", "if err!=nil{ // if err != nil {\n}", "go", "if err!=nil{")
}

// Each language reads its own strings: a Rust lifetime isn't a string,
// and a Python triple-quoted string hides the code in it.
func TestEachLanguageReadsItsOwnStrings(t *testing.T) {
	checkFound(t, "fn :[name]<'a>(", "fn first<'a>(x: &'a str) {}", "rust", "fn first<'a>( | name=first")
	checkFound(t, "run(:[x])", "s = \"\"\"run(1)\"\"\"\nrun(2)", "python", "run(2) | x=2")
	checkFound(t, "run(:[x])", "s = `run(1)`\nrun(2)", "go", "run(2) | x=2")
	checkFound(t, "run(:[x])", "# run(1)\nrun(2)", "shell", "run(2) | x=2")
	checkFound(t, "run(:[x])", "-- run(1)\nrun(2)", "sql", "run(2) | x=2")
}

// A template a language reads with a comment in it can't match: the
// comment hides code after it, leaving a hole last.
func TestATemplateALanguageCantMatchFindsNothing(t *testing.T) {
	tmpl := mustParse(t, "f(:[a]) # g(:[b])")
	if got, complete := tmpl.Find([]byte("f(1) # g(2)"), "python", false, -1); len(got) != 0 || !complete {
		t.Errorf("python: got %d, complete %v", len(got), complete)
	}
	if got, _ := tmpl.Find([]byte("f(1) # g(2)"), "go", false, -1); len(got) != 1 {
		t.Errorf("go: got %d matches, want 1", len(got))
	}
}

// CRLF line ends read like LF ones.
func TestCRLFReadsLikeLF(t *testing.T) {
	checkFound(t, "f(:[a], :[b])", "f(1,\r\n  2)", "go", "f(1,\r\n  2) | a=1 | b=2")
}

// A template whose holes could split a long line very many ways gives up
// rather than run for long, and says so.
func TestFindGivesUpOnTemplatesThatTakeTooLong(t *testing.T) {
	tmpl := mustParse(t, "f(:[a], :[b], :[c], :[d], :[e], :[f], :[g], x)")
	text := "f(" + strings.Repeat("1, ", 400) + "y)"
	began := time.Now()
	_, complete := tmpl.Find([]byte(text), "go", false, -1)
	if complete {
		t.Error("Find finished a search that should have run out of steps")
	}
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("Find took %v to give up", took)
	}
}

// Large files with many near misses stay fast: each hole is bounded by
// its brackets or its line.
func TestFindStaysFastOnLargeFiles(t *testing.T) {
	var b strings.Builder
	for i := range 20_000 {
		fmt.Fprintf(&b, "    retry(call_%d, tries=%d)\n    x = f(%d)\n", i, i, i)
	}
	tmpl := mustParse(t, "retry(:[call], attempts=:[n])")
	began := time.Now()
	matches, complete := tmpl.Find([]byte(b.String()), "python", false, -1)
	if len(matches) != 0 || !complete {
		t.Errorf("got %d matches, complete %v", len(matches), complete)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("Find took %v on %d bytes", took, b.Len())
	}
}

// One template matches files side by side, as a search does, each
// language read once.
func TestFindIsSafeSideBySide(t *testing.T) {
	tmpl := mustParse(t, "f(:[a])")
	done := make(chan int)
	for i := range 8 {
		go func() {
			language := fuzzLanguages()[i%3]
			matches, _ := tmpl.Find([]byte("f(1) f(2)"), language, false, -1)
			done <- len(matches)
		}()
	}
	for range 8 {
		if n := <-done; n != 2 {
			t.Errorf("got %d matches, want 2", n)
		}
	}
}
