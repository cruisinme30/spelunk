package benchmark

import (
	"testing"
	"time"
)

func TestPercentile95UsesNearestRank(t *testing.T) {
	values := make([]time.Duration, 20)
	for i := range values {
		values[i] = time.Duration(20-i) * time.Millisecond
	}
	if got := percentile95(values); got != 19*time.Millisecond {
		t.Errorf("percentile95(20 descending samples) = %v, want 19ms", got)
	}
	if got := percentile95([]time.Duration{7 * time.Millisecond}); got != 7*time.Millisecond {
		t.Errorf("percentile95(one sample) = %v, want 7ms", got)
	}
}
