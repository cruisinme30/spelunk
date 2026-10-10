package trigram

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

func TestCanMatchNewline(t *testing.T) {
	for pattern, want := range map[string]bool{
		`[^;]*zz`: true, `\s+x`: true, `(?s)a.*b`: true, `[\s\S]`: true, `a\nb`: true, `\W`: true,
		`a.*b`: false, `[^;\n]*zz`: false, `\w+`: false, `retry`: false, `[a-z]+`: false, `\d|x`: false,
	} {
		if got := canMatchNewline(regexp.MustCompile(pattern)); got != want {
			t.Errorf("canMatchNewline(%q) = %v, want %v", pattern, got, want)
		}
	}
}

// A regex that can cross lines, with no literal to look for, finds its
// lines in time linear in the file. Finding candidates by searching the
// whole file with it, then each next line again, was quadratic: this
// search ran out of its time limit and found nothing.
func TestARegexThatCanCrossLinesIsMatchedLineByLine(t *testing.T) {
	// @covers syntax:regex
	var b strings.Builder
	for i := range 20_000 {
		b.WriteString("    value_" + strconv.Itoa(i) + " = compute(value)\n")
	}
	b.WriteString("zz\n")
	repo := repoOf("r", map[string]string{"big.py": b.String()})
	for text, want := range map[string]int{`/[^;]*zz/`: 1, `/(?s).*zz/`: 1, `/[^;]*value_19999 /`: 1, `/\s+=/`: 20_000} {
		plan := mustPlan(t, text, defaultSettings, "")
		plan.Limit = 0 // count only
		stats, err := Search(context.Background(), plan, []Repo{repo}, 0, func(protocol.ResultItem) {})
		if err != nil || stats.Total != want || stats.Truncated {
			t.Errorf("search %s: %d results, truncated %v, err %v; want %d, complete", text, stats.Total, stats.Truncated, err, want)
		}
	}
}
