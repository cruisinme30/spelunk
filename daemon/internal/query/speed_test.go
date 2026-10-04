package query

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// parseBudget is the M1 exit gate: query/parse answers within 15 ms.
const parseBudget = 15 * time.Millisecond

// speedQueries are the mocks' queries plus the longest query allowed.
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

func TestParseIsFastEnough(t *testing.T) {
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
			for i := 0; i < b.N; i++ {
				parseOnce(text)
			}
		})
	}
}
