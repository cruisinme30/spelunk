package trigram

import (
	"runtime"
	"strings"
	"testing"
)

// A regex with \n, or a struct: template, that matches thousands of times
// on one long line (a minified bundle, say) costs about as much as the
// line: each match made its own copy of the whole line, and joining them
// measured the line again for each, so 20,000 matches on a 100 KB line
// allocated gigabytes and took seconds.
func TestManyMatchesOnOneLongLineCostAboutTheLine(t *testing.T) {
	// @covers op:struct
	const matches = 20_000
	const call = "f(x) "
	// A few kilobytes a match at most, not the line again for each.
	const budget uint64 = (4*5 + 4096) * matches // 5 bytes a call
	text := []byte(strings.Repeat(call, matches) + "\n")
	for _, query := range []string{`/f\(x\)\n?/`, `struct:"f(:[a])"`} {
		t.Run(query, func(t *testing.T) {
			term := mustPlan(t, query, defaultSettings, "").Terms[0]
			matcher := newLineMatcher(text, termCache{})
			matcher.lang = "python"
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			found := matcher.matches(term)
			runtime.ReadMemStats(&after)
			if len(found) != 1 || found[0].number != 1 || len(found[0].hits) == 0 {
				t.Fatalf("matches = %d results, want the one line with its hits", len(found))
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > budget {
				t.Errorf("matching allocated %d bytes, want at most %d", allocated, budget)
			}
			if len(found[0].hits) > maxLineHits {
				t.Errorf("the line has %d hits, want at most %d", len(found[0].hits), maxLineHits)
			}
		})
	}
}
