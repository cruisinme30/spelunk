package symbols

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// languages are the languages with rules, sorted, so a fuzzer's byte picks one.
func languages() []string {
	var names []string
	for name := range byLanguage {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// symbolSeeds are lines that define something in some language, and lines
// that once were slow or odd.
var symbolSeeds = []string{
	"class RetryPolicy:\n    def __init__(self):\n",
	"func (p *Policy) Next(attempt int) time.Duration {\r\n",
	"type Waiter[T any] interface {\n",
	"export const wait = async (ms: number) => {};\n",
	"  async charge(amount: number): Promise<void> {\n",
	"    public static <T> List<T> copy(List<T> items) {\n",
	"pub(crate) unsafe extern \"C\" fn main() {}\n",
	"  def self.helper?\n",
	"int RetryPolicy::next(int attempt) {\n",
	"template <typename T> class Box final : public Base {\n",
	"} Point;\n",
	strings.Repeat(" ", 999) + "\n",
	"public" + strings.Repeat("\t", 900) + "class X\n",
	"\xff\xfe" + "class \xc3\x28Bad\n",
	"class 😀 {\n",
	"\x00def\x00 f():\n",
}

// FuzzExtract checks that Extract never panics, reports lines in order
// within the file, and names that appear on their line.
func FuzzExtract(f *testing.F) {
	for i, seed := range symbolSeeds {
		f.Add(uint8(i), []byte(seed)) //nolint:gosec // G115: there are far fewer than 256 seeds
	}
	names := languages()
	kinds := []protocol.SymbolKind{class, iface, function, method, typeKind, otherKind}
	f.Fuzz(func(t *testing.T, pick uint8, content []byte) {
		lang := names[int(pick)%len(names)]
		lines := bytes.Split(content, []byte("\n"))
		previous := 0
		for _, symbol := range Extract(lang, content) {
			if symbol.Line <= previous || symbol.Line > len(lines) {
				t.Fatalf("Extract(%s, %q): line %d after line %d, file has %d lines", lang, content, symbol.Line, previous, len(lines))
			}
			previous = symbol.Line
			if symbol.Name == "" || !utf8.ValidString(symbol.Name) || !bytes.Contains(lines[symbol.Line-1], []byte(symbol.Name)) {
				t.Fatalf("Extract(%s, %q): name %q is not on line %d", lang, content, symbol.Name, symbol.Line)
			}
			if !slices.Contains(kinds, symbol.Kind) {
				t.Fatalf("Extract(%s, %q): unknown kind %q", lang, content, symbol.Kind)
			}
		}
	})
}

// FuzzSqueezeSpaceKeepsMatches checks that the rules find the same symbol
// in a line with its whitespace runs squeezed as in the line as written.
func FuzzSqueezeSpaceKeepsMatches(f *testing.F) {
	for _, seed := range symbolSeeds {
		f.Add(strings.TrimSuffix(seed, "\n"))
	}
	f.Fuzz(func(t *testing.T, line string) {
		if len(line) > maxLineBytes || strings.Contains(line, "\n") {
			return
		}
		squeezed := squeezeSpace(nil, []byte(line))
		for _, lang := range languages() {
			want, wantOK := match(byLanguage[lang], []byte(line))
			got, gotOK := match(byLanguage[lang], squeezed)
			if got != want || gotOK != wantOK {
				t.Fatalf("%s: match(%q) = %v %v, squeezed %q = %v %v", lang, line, want, wantOK, squeezed, got, gotOK)
			}
		}
	})
}

// TestExtractIsFastOnPathologicalFiles reads 4 MB files that are one long
// line, blank-padded lines, deep braces and CRLF line ends, in every
// language, and checks that none takes much longer than a file of short
// ordinary lines. Blank-padded lines used to take 20 times as long in
// Swift, Java and Kotlin. The budget is relative, so a busy machine slows
// both alike.
func TestExtractIsFastOnPathologicalFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test; skipped with -short")
	}
	if raceEnabled {
		t.Skip("timing test; the race detector slows matching down")
	}
	const size = 4 << 20
	ordinary := []byte(strings.Repeat("  total += 1;\n", size/15))
	lines := map[string]string{
		"one long line":        strings.Repeat("x", size),
		"blank-padded lines":   strings.Repeat(strings.Repeat(" ", 990)+"x\n", size/992),
		"tab-indented lines":   strings.Repeat(strings.Repeat("\t", 990)+"x\n", size/992),
		"padded modifiers":     strings.Repeat("public"+strings.Repeat(" ", 990)+"x\n", size/998),
		"deeply nested braces": strings.Repeat("{", size/2) + strings.Repeat("}", size/2),
		"CRLF line ends":       strings.Repeat("  total += 1;\r\n", size/16),
	}
	for _, lang := range languages() {
		start := time.Now()
		Extract(lang, ordinary)
		budget := 3*time.Since(start) + 100*time.Millisecond
		for name, content := range lines {
			start := time.Now()
			Extract(lang, []byte(content))
			if elapsed := time.Since(start); elapsed > budget {
				t.Errorf("Extract(%s, %s): %v, want under %v (3 times as long as ordinary lines)", lang, name, elapsed, budget)
			}
		}
	}
}
