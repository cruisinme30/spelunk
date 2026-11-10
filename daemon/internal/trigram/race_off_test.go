//go:build !race

package trigram

// raceEnabled reports whether allocation assertions run under the race
// detector, whose instrumentation intentionally changes allocation totals.
const raceEnabled = false
