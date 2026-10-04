//go:build race

package query

// raceEnabled reports whether the tests were built with -race, which slows
// the code down too much for timing tests to mean anything.
const raceEnabled = true
