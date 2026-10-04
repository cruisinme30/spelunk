package query

import (
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)

func labels(text string, cursor int) []string {
	var list []string
	for _, c := range Complete(text, cursor, testResolver, fixedNow) {
		list = append(list, c.Label)
	}
	return list
}

func TestCompletingOperatorsByPrefix(t *testing.T) {
	// "timeout s" offers since: then sym:, and accepting inserts since:.
	text := "timeout s"
	completions := Complete(text, len(text), testResolver, fixedNow)
	if got := labels(text, len(text)); strings.Join(got, " ") != "since: sym:" {
		t.Fatalf("Complete(%q) = %v, want [since: sym:]", text, got)
	}
	if got := applyFix(text, completions[0].Insert); got != "timeout since:" {
		t.Errorf("accepting since: gives %q, want %q", got, "timeout since:")
	}
}

func TestCompletingAuthorsShowsCountsReposAndRecency(t *testing.T) {
	// "author:ja" lists Jane first and inserts author:"Jane Doe".
	text := "author:ja"
	completions := Complete(text, len(text), testResolver, fixedNow)
	if got := labels(text, len(text)); strings.Join(got, ",") != "Jane Doe,Jason Kim,Marta Ruiz" {
		t.Fatalf("Complete(%q) = %v, want Jane Doe, Jason Kim, Marta Ruiz", text, got)
	}
	if got, want := completions[0].Detail, "214 commits · payments-api, shared-libs · last 3 days ago"; got != want {
		t.Errorf("Jane's detail = %q, want %q", got, want)
	}
	if got := applyFix(text, completions[0].Insert); got != `author:"Jane Doe" ` {
		t.Errorf("accepting Jane gives %q, want %q", got, `author:"Jane Doe" `)
	}
}

func TestCompletingValuesOfFixedOperators(t *testing.T) {
	tests := map[string]string{
		"type:":    "file code commit",
		"type:c":   "code commit",
		"case:":    "yes no",
		"since:":   "30d 2w 6m 1y",
		"lang:ty":  "typescript",
		"repo:web": "web-checkout",
	}
	for text, want := range tests {
		if got := strings.Join(labels(text, len(text)), " "); got != want {
			t.Errorf("Complete(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestNoCompletionForAValueTypedInFull(t *testing.T) {
	for _, text := range []string{"lang:python", "lang:Python", "case:yes", "repo:web-checkout"} {
		if got := Complete(text, len(text), testResolver, fixedNow); len(got) != 0 {
			t.Errorf("Complete(%q) = %+v, want nothing", text, got)
		}
	}
}

func TestNoCompletionsInsideTermsOrRegexes(t *testing.T) {
	for _, text := range []string{"", "timeout ", "/regex", `"phrase`, "f:/abc", "xyz"} {
		if got := labels(text, len(text)); len(got) != 0 {
			t.Errorf("Complete(%q) = %v, want none", text, got)
		}
	}
}
