package structural

import (
	"slices"
	"strings"
	"testing"
)

func TestCPlacesSpliceLinesBeforeReadingCommentsAndStrings(t *testing.T) {
	// @covers op:in op:struct
	for _, tt := range []struct {
		name, source string
		want         []string
	}{
		{"line comment", "// note \\\ncontinued\ncall();", []string{"c:// note \\\ncontinued"}},
		{"split line opener", "/\\\n/ note\ncall();", []string{"c:/\\\n/ note"}},
		{"split block markers", "/\\\n* note *\\\n/ call();", []string{"c:/\\\n* note *\\\n/"}},
		{"block close before code", "/* note *\\\n/ call(); /* tail */", []string{"c:/* note *\\\n/", "c:/* tail */"}},
		{"CRLF string", "\"first\\\r\nsecond\"; call();", []string{"s:\"first\\\r\nsecond\""}},
		{"escape after splice", "\"first\\\\\n\"second\"; call();", []string{"s:\"first\\\\\n\"second\""}},
		{"macro string", "#define TEXT \"first\\\n/* second */\"\ncall();", []string{"s:\"first\\\n/* second */\""}},
		{"multi-character constant", "int x = '\"ab'; call(); // tail", []string{"s:'\"ab'", "c:// tail"}},
		{"digit separators", "int x = 1'000'000; double y = 0x1.af'feP+2; char c = '0';", []string{"s:'0'"}},
		{"Unicode offsets", "int café = 0; /* λ *\\\n/ café++; \"λ\"", []string{"c:/* λ *\\\n/", "s:\"λ\""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, language := range []string{"c", "c++"} {
				if got := shownPlaces(tt.source, language); !slices.Equal(got, tt.want) {
					t.Errorf("Places(%q, %q) = %q, want %q", tt.source, language, got, tt.want)
				}
			}
		})
	}
}

func TestCKernelAssemblyCommentTextRemainsInStrings(t *testing.T) {
	// @covers op:in op:struct
	// cspell:words selftests unpriv
	// Reduced from tools/testing/selftests/bpf/progs/verifier_unpriv.c:
	// assembly comments inside continued C string literals remain strings.
	literal := "\"r2 = r0;\\\n\t/* u64 foo; */\\\n\tr6 = r10;\\\n\tr6 += -8;\""
	source := "asm volatile (" + literal + ");\n/* outside */ call();"
	want := []string{"s:" + literal, "c:/* outside */"}
	if got := shownPlaces(source, "c"); !slices.Equal(got, want) {
		t.Errorf("Places(%q, c) = %q, want %q", source, got, want)
	}
	for _, tok := range lex([]byte(source), syntaxOf("c")) {
		if wantLine := strings.Count(source[:tok.start], "\n"); tok.line != wantLine {
			t.Errorf("token %+v has line %d, want %d", tok, tok.line, wantLine)
		}
	}
}

func TestCLineSplicingDoesNotApplyToOtherLanguages(t *testing.T) {
	// @covers op:in
	for _, language := range []string{"go", "java", "rust", "javascript"} {
		source := "// note \\\ncall();"
		want := []string{"c:// note \\"}
		if got := shownPlaces(source, language); !slices.Equal(got, want) {
			t.Errorf("Places(%q, %q) = %q, want %q", source, language, got, want)
		}
	}
}

func TestCppRawStringsRetainPhysicalNewlines(t *testing.T) {
	// @covers op:in op:struct
	for _, source := range []string{
		"R\"tag(first\\\n\" /* second */)tag\"; call();",
		"u\\\n8R\\\n\"tag(first\\\n\" /* second */)tag\"; call();",
	} {
		want := []string{"s:" + source[:len(source)-len("; call();")]}
		if got := shownPlaces(source, "c++"); !slices.Equal(got, want) {
			t.Errorf("Places(%q, c++) = %q, want %q", source, got, want)
		}
	}
}

func TestCppSplitIdentifierCannotOpenRawString(t *testing.T) {
	// @covers op:in op:struct
	source := "#define abR\nconst char *s = ab\\\nR\"(literal\"; call(); const char *t = \"tail)\";"
	want := []string{"s:\"(literal\"", "s:\"tail)\""}
	if got := shownPlaces(source, "c++"); !slices.Equal(got, want) {
		t.Errorf("Places(%q, c++) = %q, want %q", source, got, want)
	}
}
