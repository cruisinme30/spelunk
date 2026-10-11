package trigram

import (
	"reflect"
	"testing"
)

// parseFile defines one function named after parsing and two that only
// mention it, so a word next to kind: shows whether it matched names or text.
var parseFile = map[string]string{"p.go": `package p

func parseLine(s string) string { return s }

func render(s string) string { return parseLine(s) }

func Close() {}
`}

// parseRepo is parseFile indexed with its definitions.
func parseRepo() Repo {
	repo := repoOf("r", parseFile)
	withSymbols(repo.Shard)
	return repo
}

func TestAWordNextToKindMatchesDefinitionNames(t *testing.T) {
	repo := parseRepo()
	for query, want := range map[string][]string{
		"kind:function parse":         {"p.go:3 "},
		"kind:function -parse":        {"p.go:5 ", "p.go:7 "},
		"kind:function parse OR Clo":  {"p.go:3 ", "p.go:7 "},
		"kind:function content:parse": {"p.go:3 ", "p.go:5 ", "p.go:7 "}, // content: still matches the file's text
	} {
		got, _ := run(t, query, repo)
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %q, want %q", query, got, want)
		}
	}
}

func TestAWordNextToKindIsHighlightedInTheName(t *testing.T) {
	items, _ := runItems(t, "kind:function parse", defaultSettings, "", parseRepo())
	if len(items) != 1 || items[0].Name != "parseLine" || len(items[0].Hits) != 1 || items[0].Hits[0].Start != 0 || items[0].Hits[0].End != 5 {
		t.Fatalf("got %+v, want parseLine with parse highlighted", items)
	}
}
