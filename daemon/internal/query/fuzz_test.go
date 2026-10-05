package query

import (
	"strings"
	"testing"
	"time"
)

// TestParseIsLinearOnLongQueries parses a megabyte of each construct that
// once took quadratic time or overflowed the stack, and a query at the
// length limit whose misplaced globals took factorial time.
func TestParseIsLinearOnLongQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test; skipped with -short")
	}
	const size = 1 << 20
	queries := map[string]string{
		"stray closing parens":         strings.Repeat(")", size),
		"opening parens":               strings.Repeat("(", size),
		"nested groups":                strings.Repeat("(a ", size/3),
		"minus signs":                  strings.Repeat("-", size) + "x",
		"misplaced globals":            "x" + strings.Repeat(" -case:yes", size/10),
		"a short query's globals":      "x" + strings.Repeat(" -case:yes", maxQueryLength/10-1),
		"keywords":                     strings.Repeat("OR AND ", size/7),
		"unknown operators":            strings.Repeat("zz:x ", size/5),
		"an unclosed quote of escapes": `"` + strings.Repeat(`\`, size),
	}
	for name, text := range queries {
		done := make(chan struct{})
		go func() {
			defer close(done)
			parseOnce(text)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("parsing a megabyte of %s took over 10 s", name)
		}
	}
}
