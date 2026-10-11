package query

import (
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

func TestTheGuidesExamplesAreValidQueries(t *testing.T) {
	guide := Guide()
	_, examples, ok := strings.Cut(guide, "\nExamples:\n")
	if !ok {
		t.Fatal("the guide has no examples")
	}
	for _, line := range strings.Split(strings.TrimSpace(examples), "\n") {
		text := strings.TrimSpace(line)
		parsed := Parse(text, testResolver)
		for _, d := range parsed.Diagnostics {
			if d.Severity == protocol.SeverityError {
				t.Errorf("example %q: %s", text, d.Message)
			}
		}
		if _, _, err := NewPlan(parsed, protocol.Settings{DefaultCount: 50}, time.Now(), ""); err != nil {
			t.Errorf("example %q doesn't plan: %v", text, err)
		}
	}
}

func TestTheGuideNamesEveryOperator(t *testing.T) {
	guide := Guide()
	for i := range operators {
		if !strings.Contains(guide, "  "+operators[i].full+":") {
			t.Errorf("the guide doesn't list %s:", operators[i].full)
		}
	}
}
