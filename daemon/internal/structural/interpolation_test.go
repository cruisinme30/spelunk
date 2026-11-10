package structural

import (
	"slices"
	"strings"
	"testing"
)

func TestPlacesSeparatesExecutableInterpolation(t *testing.T) {
	// @covers op:in
	tests := []struct {
		name, language, text string
		want                 []string
	}{
		{"template", "javascript", "const s = `answer ${dangerCall()}`;", []string{"s:`answer ${", "s:}`"}},
		{"adjacent fields", "typescript", "`${a}${b}`", []string{"s:`${", "s:}${", "s:}`"}},
		{"escaped field", "javascript", "`\\${literal} ${code}`", []string{"s:`\\${literal} ${", "s:}`"}},
		{"nested template", "javascript", "`a ${f(`b ${g()}`)} c`", []string{"s:`a ${", "s:`b ${", "s:}`", "s:} c`"}},
		{"nested object and comment", "javascript", "`a ${f({x: '}' /* } */})} b`", []string{"s:`a ${", "s:'}'", "c:/* } */", "s:} b`"}},
		{"regex braces", "javascript", "`a ${/}/.test(value) ? run() : other()} b`", []string{"s:`a ${", "s:} b`"}},
		{"regex character class", "javascript", "`a ${/[}\"/]/.test(value)} b`", []string{"s:`a ${", "s:} b`"}},
		{"division", "javascript", "`a ${amount / count + run()} b`", []string{"s:`a ${", "s:} b`"}},
		{"postfix increment then division", "javascript", "`${counter++ / \"dangerCall\".length / 2}`", []string{"s:`${", `s:"dangerCall"`, "s:}`"}},
		{"postfix decrement then division", "javascript", "`${counter-- / \"dangerCall\".length / 2}`", []string{"s:`${", `s:"dangerCall"`, "s:}`"}},
		{"prefix increment then division", "javascript", "`${++counter / \"dangerCall\".length / 2}`", []string{"s:`${", `s:"dangerCall"`, "s:}`"}},
		{"formatted", "python", `s = f"answer {dangerCall()}"`, []string{`s:f"answer {`, `s:}"`}},
		{"doubled braces", "python", `F"{{literal}} {run()}"`, []string{`s:F"{{literal}} {`, `s:}"`}},
		{"raw formatted", "python", `rf"\{run()} {{literal}}"`, []string{`s:rf"\{`, `s:} {{literal}}"`}},
		{"format fields", "python", `f"{value:{width}.{precision}f}"`, []string{`s:f"{`, "s::{", "s:}.{", "s:}f", `s:}"`}},
		{"format conversion", "python", `f"{value!r:>20}"`, []string{`s:f"{`, "s::>20", `s:}"`}},
		{"dictionary", "python", `f"{ {'x': '}'}['x'] }"`, []string{`s:f"{`, "s:'x'", "s:'}'", "s:'x'", `s:}"`}},
		{"same quote expression", "python", `f"{get("x")}"`, []string{`s:f"{`, `s:"x"`, `s:}"`}},
		{"multiline", "python", "f'''a\n{run()}\nb'''", []string{"s:f'''a\n{", "s:}\nb'''"}},
		{"not formatted", "python", `"answer {run()}"`, []string{`s:"answer {run()}"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shownPlaces(tt.text, tt.language); !slices.Equal(got, tt.want) {
				t.Errorf("Places(%q, %s) = %q, want %q", tt.text, tt.language, got, tt.want)
			}
			assertOrderedPlaces(t, tt.text, tt.language)
		})
	}
}

func assertOrderedPlaces(t *testing.T, text, language string) {
	t.Helper()
	end := 0
	for _, place := range Places([]byte(text), language) {
		if place.Start < end || place.End <= place.Start || place.End > len(text) {
			t.Fatalf("Places(%q, %s) has invalid or overlapping place: %+v", text, language, place)
		}
		end = place.End
	}
}

func TestNestedRustComments(t *testing.T) {
	// @covers op:in op:struct
	text := "before /* outer /* inner */ dangerCall(); */ after"
	if got := shownPlaces(text, "rust"); !slices.Equal(got, []string{"c:/* outer /* inner */ dangerCall(); */"}) {
		t.Errorf("Places(%q, rust) = %q", text, got)
	}
	if got := texts(text, "rust"); !slices.Equal(got, []string{"before", "after"}) {
		t.Errorf("lex(%q, rust) = %q", text, got)
	}
	if got := shownPlaces("/* outer /* inner */", "rust"); !slices.Equal(got, []string{"c:/* outer /* inner */"}) {
		t.Errorf("unterminated outer comment = %q", got)
	}
	if got := shownPlaces(text, "c"); !slices.Equal(got, []string{"c:/* outer /* inner */"}) {
		t.Errorf("C comment incorrectly nested: %q", got)
	}
}

// Deeply nested or incomplete interpolation must terminate without broken
// place ranges or unbounded recursion.
func TestInterpolationBoundsAndMalformedInput(t *testing.T) {
	for _, text := range []string{
		"`a ${call(`b ${x}`)}", "`a ${call(}", `f"{value:{width`,
		`f"{` + strings.Repeat("a:{", 300) + strings.Repeat("}", 301) + `"`,
		strings.Repeat("`${", 300) + "x" + strings.Repeat("}`", 300),
	} {
		assertOrderedPlaces(t, text, "javascript")
		assertOrderedPlaces(t, text, "python")
	}
}

// ASCII fast paths must keep the previous Unicode word and whitespace rules.
func TestLexASCIIAndUnicodeWhitespace(t *testing.T) {
	text := "alpha_1\t東京\vnaïve\fcafé\rπ\u00a0omega"
	want := []string{"alpha_1", "東京", "naïve", "café", "π", "omega"}
	if got := texts(text, "python"); !slices.Equal(got, want) {
		t.Errorf("lex(%q) = %q, want %q", text, got, want)
	}
}

// Failed nested templates must share a work budget, not retry every suffix
// exponentially. Exhausted input cannot become a string replacement target.
func TestMalformedInterpolationHasBoundedWork(t *testing.T) {
	for _, tt := range []struct{ language, opening string }{
		{"javascript", "`${"}, {"python", `f"{`},
	} {
		for _, repetitions := range []int{20, 1_000} {
			text := []byte(strings.Repeat(tt.opening, repetitions) + `"dangerCall"`)
			var places []Place
			l := lexer{syntax: syntaxOf(tt.language), text: text, gap: true, places: &places}
			for l.pos < len(text) {
				l.next()
			}
			if l.interpolationWork == nil || *l.interpolationWork != 0 {
				t.Fatalf("%s: malformed nesting did not exhaust the shared budget", tt.language)
			}
			for _, p := range places {
				if p.Kind == String {
					t.Errorf("%s: ambiguous malformed source became string text: %+v", tt.language, p)
				}
			}
		}
	}
}
