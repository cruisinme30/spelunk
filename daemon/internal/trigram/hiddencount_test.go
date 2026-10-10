package trigram

import (
	"context"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// A note says how many results a filter hid only when it could count them
// all. Here ignoring case finds 60,000 matches, past query.MaxResults, so
// the count stops at 50,000; the note said 20,000 matches were hidden by
// case:yes when 30,000 were.
func TestNoHiddenNoteWhenItsCountStopsShort(t *testing.T) {
	// @covers op:case
	const lines = 30_000
	repo := repoOf("r", map[string]string{"a.txt": strings.Repeat("Retry\n", lines) + strings.Repeat("retry\n", lines)})
	plan := mustPlan(t, "case:yes Retry", defaultSettings, "")
	plan.Limit = 0
	stats, err := Search(context.Background(), plan, []Repo{repo}, 0, func(protocol.ResultItem) {})
	if err != nil || stats.Total != lines || stats.Truncated {
		t.Fatalf("search: %d results, truncated %v, err %v; want %d, complete", stats.Total, stats.Truncated, err, lines)
	}
	for _, note := range stats.Hidden {
		if note.Reason == "case" {
			t.Errorf("hidden note %+v; want none, since ignoring case counts past %d", note, query.MaxResults)
		}
	}
}
