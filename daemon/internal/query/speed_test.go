package query

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// parseBudget is how fast query/parse must answer, so chips and errors keep up with typing.
const parseBudget = 15 * time.Millisecond

// speedQueries are typical queries, one with two errors, and the longest
// query allowed.
var speedQueries = []string{
	"retry_policy",
	`f:.*test\.py$ timeout`,
	`author:jane f:.*test\.py$ timeout`,
	`author:jane (timeout OR retry) -f:vendor/ since:6m`,
	`case:yes /Retry(Policy|Config)/ lang:python`,
	`msg:"fix flaky" repo:web count:20`,
	`author:jane ( timeout OR retry -f:vendor/ sinse:6m`,
	strings.Repeat("(alpha OR beta) -f:vendor/ ", maxQueryLength)[:maxQueryLength],
}

// parseOnce does what query/parse does: parse, plan and complete.
func parseOnce(text string) {
	parsed := Parse(text, nil)
	_, _, _ = NewPlan(parsed, defaultSettings, fixedNow, "")
	Complete(text, len(text), nil, fixedNow)
}

// TestParseIsFastEnough checks the parse budget on this machine. Timing is
// meaningless under -race and too slow for -short, so it skips both.
func TestParseIsFastEnough(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test; skipped with -short")
	}
	if raceEnabled {
		t.Skip("timing test; the race detector slows parsing down")
	}
	for _, text := range speedQueries {
		durations := make([]time.Duration, 200)
		for i := range durations {
			start := time.Now()
			parseOnce(text)
			durations[i] = time.Since(start)
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		if p95 := durations[len(durations)*95/100]; p95 > parseBudget {
			t.Errorf("parsing %.40q…: p95 %v, want under %v", text, p95, parseBudget)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	for _, text := range speedQueries {
		name := text
		if len(name) > 30 {
			name = name[:30] + "…"
		}
		b.Run(name, func(b *testing.B) {
			for range b.N {
				parseOnce(text)
			}
		})
	}
}
