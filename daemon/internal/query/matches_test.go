package query

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func matchTerm(pattern string, wholeWord bool) *Content {
	re := regexp.MustCompile(pattern)
	return &Content{Re: re, WholeWord: wholeWord, resumable: canResume(re)}
}

func visitLocations(ctx context.Context, term *Content, text string, budget int) ([][]int, error) {
	var found [][]int
	err := term.VisitMatches(ctx, []byte(text), budget, func(loc []int) bool {
		found = append(found, append([]int(nil), loc...))
		return true
	})
	return found, err
}

func TestVisitMatchesReportsExactAndExceededBudgets(t *testing.T) {
	term := matchTerm("a", false)
	if got, err := visitLocations(t.Context(), term, "aa", 2); err != nil || !reflect.DeepEqual(got, [][]int{{0, 1}, {1, 2}}) {
		t.Fatalf("exact budget = %v, %v", got, err)
	}
	if got, err := visitLocations(t.Context(), term, "aaa", 2); !errors.Is(err, ErrMatchBudget) || !reflect.DeepEqual(got, [][]int{{0, 1}, {1, 2}}) {
		t.Fatalf("exceeded budget = %v, %v", got, err)
	}
}

func TestVisitMatchesChargesEmptyAndRejectedCandidates(t *testing.T) {
	zeroWidth := matchTerm("a*", false)
	text := strings.Repeat("b", 10) + "a"
	if got, err := visitLocations(t.Context(), zeroWidth, text, 10); !errors.Is(err, ErrMatchBudget) || slicesContainNonEmpty(got) {
		t.Fatalf("empty prefix = %v, %v; want budget error before the late match", got, err)
	}
	if got, err := visitLocations(t.Context(), zeroWidth, text, 11); err != nil || !slicesContainNonEmpty(got) {
		t.Fatalf("complete empty prefix = %v, %v; want the late match", got, err)
	}

	whole := matchTerm("a", true)
	if got, err := visitLocations(t.Context(), whole, "aaaa a", 4); !errors.Is(err, ErrMatchBudget) || len(got) != 0 {
		t.Fatalf("rejected whole words = %v, %v; want budget error", got, err)
	}
	if got, err := visitLocations(t.Context(), whole, "aaaa a", 5); err != nil || !reflect.DeepEqual(got, [][]int{{5, 6}}) {
		t.Fatalf("complete whole words = %v, %v", got, err)
	}
}

func TestVisitMatchesPreservesOverlapAndOriginalAssertions(t *testing.T) {
	overlap := matchTerm("a-a", true)
	if got, err := visitLocations(t.Context(), overlap, "ba-a-a", 10); err != nil || !reflect.DeepEqual(got, [][]int{{3, 6}}) {
		t.Fatalf("overlap = %v, %v", got, err)
	}
	for _, test := range []struct {
		pattern, text string
		want          [][]int
	}{
		{pattern: `(?m)^a`, text: "a\nba", want: [][]int{{0, 1}}},
		{pattern: `\ba`, text: "ba a", want: [][]int{{3, 4}}},
	} {
		got, err := visitLocations(t.Context(), matchTerm(test.pattern, false), test.text, 10)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s on %q = %v, %v; want %v", test.pattern, test.text, got, err, test.want)
		}
	}
}

func TestVisitMatchesHonorsCallbackStopAndCancellation(t *testing.T) {
	term := matchTerm("a", false)
	visited := 0
	err := term.VisitMatches(t.Context(), []byte(strings.Repeat("a", 100)), 1, func([]int) bool {
		visited++
		return false
	})
	if err != nil || visited != 1 {
		t.Fatalf("callback stop = %d visits, %v", visited, err)
	}

	ctx, cancel := context.WithCancelCause(t.Context())
	want := errors.New("stop matching")
	visited = 0
	err = term.VisitMatches(ctx, []byte(strings.Repeat("a", 100)), 100, func([]int) bool {
		visited++
		cancel(want)
		return true
	})
	if !errors.Is(err, want) || visited != 1 {
		t.Fatalf("cancel = %d visits, %v; want one visit and cause", visited, err)
	}
}

func TestVisitMatchesBoundsDenseSixteenMiBLine(t *testing.T) {
	term := matchTerm("a*", false)
	text := strings.Repeat("b", 16<<20)
	visited := 0
	err := term.VisitMatches(t.Context(), []byte(text), MaxMatchPositions, func([]int) bool {
		visited++
		return true
	})
	if !errors.Is(err, ErrMatchBudget) || visited != MaxMatchPositions {
		t.Fatalf("dense line = %d visits, %v; want %d and budget error", visited, err, MaxMatchPositions)
	}
}

func slicesContainNonEmpty(locs [][]int) bool {
	for _, loc := range locs {
		if loc[1] > loc[0] {
			return true
		}
	}
	return false
}
