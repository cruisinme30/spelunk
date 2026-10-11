package trigram

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
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
		"kind:function parse":          {"p.go:3 "},
		"kind:function -parse":         {"p.go:5 ", "p.go:7 "},
		"kind:function (parse OR Clo)": {"p.go:3 ", "p.go:7 "},
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

// usesFile uses Policy in a method of a class, in a function, and outside
// every definition; idle never does.
var usesFile = map[string]string{"u.py": `from policies import Policy

class Client:
    def charge(self):
        return Policy()

def build():
    p = Policy()
    return p

def idle():
    return None
`}

// usesRepo is usesFile indexed with its definitions.
func usesRepo() Repo {
	repo := repoOf("r", usesFile)
	withSymbols(repo.Shard)
	return repo
}

// within renders code results as "path:line in kind name".
func within(items []protocol.ResultItem) []string {
	out := []string{}
	for _, item := range items {
		if item.Within == nil {
			out = append(out, fmt.Sprintf("%s:%d outside", item.Path, item.Line))
			continue
		}
		out = append(out, fmt.Sprintf("%s:%d in %s %s", item.Path, item.Line, item.Within.Kind, item.Within.Name))
	}
	return out
}

func TestATermOnTextNextToKindMatchesInsideTheDefinitions(t *testing.T) {
	for query, want := range map[string][]string{
		"kind:function ref:Policy":                 {"u.py:8 in function build"},
		"kind:method ref:Policy":                   {"u.py:5 in method charge"},
		"kind:class ref:Policy":                    {"u.py:5 in class Client"},
		"symbol:/^(Client|charge)$/ ref:Policy":    {"u.py:5 in method charge"}, // the innermost definition kept
		"kind:function content:return":             {"u.py:9 in function build", "u.py:12 in function idle"},
		"kind:function struct:\"Policy(:[args])\"": {"u.py:8 in function build"},
		"kind:function bui ref:Policy":             {"u.py:8 in function build"}, // a word still names the definition
		"kind:function idl ref:Policy":             {},
	} {
		items, _ := runItems(t, query, defaultSettings, "", usesRepo())
		if got := within(items); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %q, want %q", query, got, want)
		}
	}
}

func TestAUseCarriesTheLinesOfItsDefinition(t *testing.T) {
	items, _ := runItems(t, "kind:function ref:Policy", defaultSettings, "", usesRepo())
	if len(items) != 1 || items[0].Within == nil || items[0].Within.Line != 7 || items[0].Within.End != 9 {
		t.Fatalf("got %+v, want the use on line 8 within build, lines 7-9", items)
	}
}

func TestATermOnTextLeftOutNextToKindLeavesOutTheDefinitionsWhoseBodyHasIt(t *testing.T) {
	got, _ := run(t, "kind:function -ref:Policy", usesRepo())
	if want := []string{"u.py:11 "}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q (idle alone never uses Policy)", got, want)
	}
}

func TestUsesOutsideTheDefinitionsAreCountedForKind(t *testing.T) {
	_, stats := run(t, "kind:function ref:Policy", usesRepo())
	if len(stats.Hidden) != 1 || stats.Hidden[0].Reason != "kind" || stats.Hidden[0].Count != 2 || stats.Hidden[0].Unit != "matches" {
		t.Errorf("hidden = %+v, want 2 matches hidden by kind:function (the import and the method's use)", stats.Hidden)
	}
}
