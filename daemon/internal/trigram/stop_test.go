package trigram

import (
	"context"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// longText is 20,000 lines that every stop test's term matches, far more
// text than matching reads between asking whether to stop.
var longText = strings.Repeat("needle in a line\n", 20_000)

// Matching a long text asks whether the search should stop as it reads,
// and gives up on the rest once told to, whatever way the term is matched:
// one big file must not keep a search running past its time limit or its
// cancellation.
func TestMatchingGivesUpOnTheRestOfATextOnceTheSearchStops(t *testing.T) {
	for _, text := range []string{"needle", "/needle/", "/^needle/", `/a line\n/`, `struct:"needle in"`} {
		t.Run(text, func(t *testing.T) {
			term := mustPlan(t, text, defaultSettings, "").Terms[0]
			running := newLineMatcher([]byte(longText), termCache{})
			asked := 0
			running.stop = func() bool { asked++; return false }
			all := len(running.matches(term))
			if running.cut || asked == 0 || all < 19_999 {
				t.Fatalf("running search: %d lines, cut %v, asked %d times; want them all, not cut, asked", all, running.cut, asked)
			}

			stopped := newLineMatcher([]byte(longText), termCache{})
			asked = 0
			stopped.stop = func() bool { asked++; return true }
			if found := len(stopped.matches(term)); !stopped.cut || asked != 1 || found >= all {
				t.Fatalf("stopped search: %d of %d lines, cut %v, asked %d times; want fewer, cut, asked once", found, all, stopped.cut, asked)
			}
		})
	}
}

// A search that stops while matching a file marks its results truncated:
// the file it gave up on may hold more.
func TestASearchStoppedInsideAFileIsTruncated(t *testing.T) {
	plan := mustPlan(t, "needle", defaultSettings, "")
	repos := []Repo{repoOf("r", map[string]string{"big.txt": longText})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newSearcher(ctx, plan, 0, func(protocol.ResultItem) {})
	s.forEachCandidate(repos, func(_ rankedCandidate, matcher *lineMatcher, _ func(query.Pred) bool) {
		cancel() // as a deadline passing while the file is read
		matcher.matches(plan.Terms[0])
		if !matcher.cut {
			t.Error("matching read the whole file after the search was cancelled")
		}
	})
	if !s.truncated {
		t.Error("results are not marked truncated")
	}
}
