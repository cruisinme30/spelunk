package trigram

import (
	"reflect"
	"testing"
)

func TestLiteralSearchAgreesWithTheRegex(t *testing.T) {
	content := "Kelvin\n\u212aelvin scale\nkelvin\nnothing\n\u017ftop and STOP\n"
	tests := []struct {
		query string
		want  []string
	}{
		{"kelvin", []string{"f.txt:1 Kelvin", "f.txt:2 \u212aelvin scale", "f.txt:3 kelvin"}},
		{"case:yes kelvin", []string{"f.txt:3 kelvin"}},
		{"stop", []string{"f.txt:5 \u017ftop and STOP"}},
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
	if kelvin.usableOn([]byte("\u212aelvin")) {
		t.Error("folded finder with a k used on text with a KELVIN SIGN")
	}
	if !kelvin.usableOn([]byte("plain text")) {
		t.Error("folded finder refused plain text")
	}
	if _, ok := newLiteralFinder(plan.Terms[1]); ok {
		t.Error("finder for café (non-ASCII, ignoring case) = ok, want the regex")
	}
	sensitive := mustPlan(t, "case:yes Size", defaultSettings, "")
	if f, ok := newLiteralFinder(sensitive.Terms[0]); !ok || f.folded || string(f.needle) != "Size" {
		t.Errorf("case-sensitive finder = %+v, %v; want exact Size", f, ok)
	}
	if _, ok := newLiteralFinder(mustPlan(t, "/a.b/", defaultSettings, "").Terms[0]); ok {
		t.Error("finder for a regex term = ok, want the regex")
	}
}
