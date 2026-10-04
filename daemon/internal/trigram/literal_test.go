package trigram

import (
	"bytes"
	"reflect"
	"testing"
)

func TestLiteralSearchAgreesWithTheRegex(t *testing.T) {
	content := "Kelvin\n" + kelvinSign + "elvin scale\nkelvin\nnothing\n" + longS + "top and STOP\n"
	tests := []struct {
		query string
		want  []string
	}{
		{"kelvin", []string{"f.txt:1 Kelvin", "f.txt:2 " + kelvinSign + "elvin scale", "f.txt:3 kelvin"}},
		{"case:yes kelvin", []string{"f.txt:3 kelvin"}},
		{"stop", []string{"f.txt:5 " + longS + "top and STOP"}},
		{"nothing", []string{"f.txt:4 nothing"}},
	}
	repo := repoOf("r", map[string]string{"f.txt": content})
	for _, tt := range tests {
		got, _ := run(t, "type:code "+tt.query, repo)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.query, got, tt.want)
		}
	}
}

func TestLiteralFinderIsOnlyUsedWhereSound(t *testing.T) {
	plan := mustPlan(t, "kelvin café Size", defaultSettings, "")
	kelvin, ok := newLiteralFinder(plan.Terms[0])
	if !ok || !kelvin.folded || string(kelvin.needle) != "kelvin" {
		t.Fatalf("finder for kelvin = %+v, %v; want folded kelvin", kelvin, ok)
	}
	if kelvin.usableOn([]byte(kelvinSign + "elvin")) {
		t.Error("folded finder for kelvin usable on text with a KELVIN SIGN = true, want false")
	}
	if !kelvin.usableOn([]byte("plain text")) {
		t.Error("folded finder for kelvin usable on plain text = false, want true")
	}
	if _, ok := newLiteralFinder(plan.Terms[1]); ok {
		t.Error("finder for café (non-ASCII, ignoring case) = ok, want the regex")
	}
	sensitive := mustPlan(t, "case:yes Size", defaultSettings, "")
	if f, ok := newLiteralFinder(sensitive.Terms[0]); !ok || f.folded || string(f.needle) != "Size" {
		t.Errorf("case-sensitive finder = %+v, %v; want exact Size", f, ok)
	}
	if _, ok := newLiteralFinder(mustPlan(t, "/a.b/", defaultSettings, "").Terms[0]); ok {
		t.Error("finder for a regex term without a 3-letter literal = ok, want the regex")
	}
	regex := mustPlan(t, "case:yes /Retry(Policy|Config)/", defaultSettings, "")
	if f, ok := newLiteralFinder(regex.Terms[0]); !ok || !f.folded || string(f.needle) != "retry" {
		t.Errorf("finder for /Retry(Policy|Config)/ = %+v, %v; want retry, ignoring case", f, ok)
	}
}

func TestIndexFoldFindsEveryCaseOfTheNeedle(t *testing.T) {
	// naive lowercases the text and searches it, the way the engine did
	// before indexFold.
	naive := func(text, needle []byte, from int) int {
		i := bytes.Index(bytes.ToLower(text[from:]), needle)
		if i < 0 {
			return -1
		}
		return from + i
	}
	// Partial and run-together words on purpose:
	// cspell:ignore eout timeouttimeout neout timeou
	needles := []string{"timeout", "t", "tt", "_x9", "eout", "aaa"}
	texts := []string{
		"", "t", "TimeOut", "a TIMEOUT and a timeout", "timeouttimeout", "tttT", "TtTt tT", "aAaAaa", "x_X9_x9",
		"tim\neout timeou", "TIMEOU", "no match here",
	}
	for _, needle := range needles {
		for _, text := range texts {
			for from := 0; from <= len(text); from++ {
				got := indexFold([]byte(text), []byte(needle), from)
				if want := naive([]byte(text), []byte(needle), from); got != want {
					t.Errorf("indexFold(%q, %q, %d) = %d, want %d", text, needle, from, got, want)
				}
			}
		}
	}
}
