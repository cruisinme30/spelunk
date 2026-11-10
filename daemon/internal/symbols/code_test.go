package symbols

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefinitionsIgnoreStringsAndComments(t *testing.T) {
	// @covers op:sym op:kind
	tests := []struct {
		name, language, source string
		want                   []string
	}{
		{"Python triple-quoted string", "python", "example = '''\ndef phantom():\n    pass\n'''\ndef real():\n    pass\n", []string{"5 function real"}},
		{"Python docstring", "python", "\"\"\"\nclass Phantom:\n    def fake(self):\n        pass\n\"\"\"\nclass Real:\n    pass\n", []string{"6 class Real"}},
		{"JavaScript template", "javascript", "const example = `\nfunction phantom() {}\n`;\nfunction real() {}\n", []string{"4 function real"}},
		{"Go raw string", "go", "var example = `\nfunc phantom() {}\n`\nfunc real() {}\n", []string{"4 function real"}},
		{"C block comment", "c", "/*\nint phantom(void) {\n}\n*/\nint real(void) {\n}\n", []string{"5 function real"}},
		{"Rust external function", "rust", "pub unsafe extern \"C\" fn real() {}\n", []string{"1 function real"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := found(test.language, test.source); !reflect.DeepEqual(got, test.want) {
				t.Errorf("Extract(%q, %q) = %q, want %q", test.language, test.source, got, test.want)
			}
			if outline := Outline(test.language, []byte(test.source)); len(outline) != len(test.want) {
				t.Errorf("Outline(%q, %q) has %d definitions, want %d", test.language, test.source, len(outline), len(test.want))
			}
		})
	}
}

func TestBraceBodiesDoNotDependOnIndentation(t *testing.T) {
	// @covers op:kind op:content
	tests := []struct {
		name, language, source string
		want                   []string
	}{
		{"Go one-line body", "go", "func one() int { return 1 }\nfunc two() {}\n", []string{"one 1-1", "two 2-2"}},
		{"C prototype has no body", "c", "int declared(void);\nint real(void) {\nrun();\n}\n", []string{"real 2-4"}},
		{"Go braces in parameter types", "go", "func run(arg interface { Call() }) {\n  arg.Call()\n}\n", []string{"run 1-3"}},
		{"Kotlin expression body before braced function", "kotlin", "fun one() = 1\nfun two() {\n  run()\n}\n", []string{"one 1-1", "two 2-4"}},
		{"C unindented body", "c", "void foo(void) {\ndangerCall();\n}\n", []string{"foo 1-3"}},
		{"C brace on its own line", "c", "void foo(void)\n{\ndangerCall();\n}\n", []string{"foo 1-4"}},
		{"C multiline unindented parameters", "c", "void foo(\nint value\n)\n{\ndangerCall();\n}\n", []string{"foo 1-6"}},
		{"Braces in C literals and comments", "c", "void foo(void) {\nputs(\"}\");\n/* } */\nif (ready) {\nputs(\"{\");\n}\n}\nvoid bar(void) {\n}\n", []string{"foo 1-7", "bar 8-9"}},
		{"Type alias does not claim a later literal", "go", "type Count int\nvar values = []Count{\n1,\n}\n", []string{"Count 1-1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := spans(test.language, test.source); !reflect.DeepEqual(got, test.want) {
				t.Errorf("Extract(%q, %q) spans = %q, want %q", test.language, test.source, got, test.want)
			}
		})
	}
}

func TestAmbiguousBraceSyntaxKeepsLanguageSpanRules(t *testing.T) {
	// @covers op:kind op:content
	tests := []struct {
		name, language, source string
		want                   []string
	}{
		{"Go struct return", "go", "func makeValue() struct { Value int } {\n    return struct { Value int }{Value: 1}\n}\n", []string{"makeValue 1-3"}},
		{"Go interface return", "go", "func makeValue() interface { Call() } {\n    return nil\n}\n", []string{"makeValue 1-3"}},
		{"TypeScript object return", "typescript", "function makeValue(): { value: number } {\n  return { value: 1 };\n}\n", []string{"makeValue 1-3"}},
		{"TypeScript typed arrow", "typescript", "const makeValue = (): { value: number } => {\n  return { value: 1 };\n};\n", []string{"makeValue 1-3"}},
		{"JavaScript regex closer", "javascript", "function test() {\n  const rx = /}/;\n  return rx;\n}\n", []string{"test 1-4"}},
		{"JavaScript regex opener", "javascript", "function test() {\n  const rx = /{/;\n  return rx;\n}\nfunction after() {}\n", []string{"test 1-4", "after 5-5"}},
		{"C macro brace", "c", "void test(void) {\n#define END }\n  call();\n}\n", []string{"test 1-4"}},
		{"C continued macro brace", "c", "void test(void) {\n#define END \\\n }\n  call();\n}\n", []string{"test 1-5"}},
		{"Python string closer without indentation", "python", "class C:\n    def method(self):\n        value = \"\"\"\nplain\n\"\"\"\n        return value\n", []string{"C 1-6", "method 2-6"}},
		{"Python body containing only docstring", "python", "def only():\n    \"\"\"\nplain\n\"\"\"\n", []string{"only 1-4"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := spans(test.language, test.source); !reflect.DeepEqual(got, test.want) {
				t.Errorf("Extract(%q, %q) spans = %q, want %q", test.language, test.source, got, test.want)
			}
		})
	}
}

func TestCHeaderSkipsBlankLinesInOnePass(t *testing.T) {
	// @covers op:kind op:content
	header := "void test(void)" + strings.Repeat("\n", 100_000) + "{\n}"
	got, _ := scanCHeader([]byte(header))
	if want := strings.IndexByte(header, '{'); got != want {
		t.Errorf("header body starts at %d, want %d", got, want)
	}
}

func TestCDefinitionsDistinguishBodiesFromDeclarations(t *testing.T) {
	// @covers op:sym op:kind op:content
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"multiline prototype", "static int declared(int first,\n int second);\n", nil},
		{"prototype with separate terminator", "int declared(void)\n;\n", nil},
		{"prototype with trailing attribute", "static int declared(int value)\n __attribute__((pure));\n", nil},
		{"macro variable initializer", "static DEFINE_PER_CPU(struct work, pending) =\n WORK_INIT(notify);\n", nil},
		{"macro aggregate initializer", "static DECLARE_ENTRY(entry)\n= { 1, 2 };\n", nil},
		{"one-line function", "int actual(void) { return 1; }\n", []string{"actual 1-1"}},
		{"prototype followed by definition", "int declared(int first,\n int second);\nint actual(void) { return 1; }\n", []string{"actual 3-3"}},
		{"trailing attributes", "static int actual(int value)\n__acquires(lock)\n__releases(lock)\n{\nreturn value;\n}\n", []string{"actual 1-6"}},
		{"standard attribute", "int actual(void)\n[[deprecated]]\n{\nreturn 1;\n}\n", []string{"actual 1-5"}},
		{"braces and semicolons in attribute string", "int actual(void)\n__section(\"};\")\n{\nreturn 1;\n}\n", []string{"actual 1-5"}},
		{"incomplete signature", "int pending(\n", []string{"pending 1-1"}},
	} {
		for _, language := range []string{"c", "c++"} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				if got := spans(language, tc.source); !reflect.DeepEqual(got, tc.want) {
					t.Errorf("Extract(%q, %q) = %q, want %q", language, tc.source, got, tc.want)
				}
				extracted := Extract(language, []byte(tc.source))
				outline := Outline(language, []byte(tc.source))
				if len(outline) != len(extracted) {
					t.Fatalf("Outline has %d definitions, Extract has %d", len(outline), len(extracted))
				}
				for i, entry := range outline {
					if entry.Name != extracted[i].Name || entry.Line != extracted[i].Line {
						t.Errorf("Outline[%d] = %+v, Extract = %+v", i, entry, extracted[i])
					}
				}
			})
		}
	}
}

func TestCOutlineRetainsParentsAfterSkippingDeclarations(t *testing.T) {
	// @covers op:sym
	source := "int declared(int first,\n int second);\nclass Outer {\n  class Inner {\n  };\n};\n"
	got := Outline("c++", []byte(source))
	if len(got) != 2 || got[0].Name != "Outer" || got[0].Depth != 0 || got[1].Name != "Inner" || got[1].Depth != 1 {
		t.Errorf("Outline = %+v, want Outer containing Inner", got)
	}
}

func BenchmarkCHeaderBlankLines(b *testing.B) {
	for _, lines := range []struct {
		name  string
		count int
	}{{"2000", 2_000}, {"8000", 8_000}, {"16000", 16_000}} {
		b.Run(lines.name, func(b *testing.B) {
			header := []byte("void test(void)" + strings.Repeat("\n", lines.count) + "{\n}")
			b.ResetTimer()
			for range b.N {
				if at, _ := scanCHeader(header); at < 0 {
					b.Fatal("body not found")
				}
			}
		})
	}
}

func TestFormattedLiteralPreservesItsBodyIndentation(t *testing.T) {
	// @covers op:kind op:content
	for _, prefix := range []string{"f", "F", "fr", "Fr", "fR", "FR", "rf", "Rf", "rF", "RF"} {
		t.Run(prefix, func(t *testing.T) {
			literal := prefix + "\"\"\"hello\nworld\n\"\"\"\n"
			inside := "def first():\n    " + literal
			if got, want := spans("python", inside), []string{"first 1-4"}; !reflect.DeepEqual(got, want) {
				t.Errorf("Extract(python, %q) spans = %q, want %q", inside, got, want)
			}
			outside := "def first():\n    pass\n" + literal + "def second():\n    pass\n"
			if got, want := spans("python", outside), []string{"first 1-2", "second 6-7"}; !reflect.DeepEqual(got, want) {
				t.Errorf("Extract(python, %q) spans = %q, want %q", outside, got, want)
			}
		})
	}
}
