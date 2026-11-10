package query

import (
	"slices"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

func TestUnknownOperatorTextFixPreservesEscapesAndFollowingTerms(t *testing.T) {
	// @covers diag:unknown_operator
	for _, word := range []string{
		`unknown:\`,
		`unknown:drivers\\net`,
		`unknown:"drivers\\net"`,
		`unknown:"a\"b"`,
		`unknown:"😀\\copy_process"`,
	} {
		raw := word + ` schedule`
		fix := requireTextFix(t, raw)
		fixed := ApplyFix(raw, fix)
		repaired := Parse(fixed, testResolver)
		values := []string{}
		if repaired.Root != nil {
			walk(repaired.Root, func(n *protocol.Node) {
				if n.Kind == protocol.NodeKindText {
					values = append(values, n.Value)
				}
			})
		}
		if !slices.Equal(values, []string{word, "schedule"}) || len(repaired.Diagnostics) != 0 {
			t.Errorf("text fix for %q gives %q with values %q diagnostics %v", raw, fixed, values, codes(repaired.Diagnostics))
		}
	}
}

func requireTextFix(t *testing.T, raw string) protocol.Fix {
	t.Helper()
	for _, diagnostic := range Parse(raw, testResolver).Diagnostics {
		if diagnostic.Code != DiagUnknownOperator {
			continue
		}
		for _, fix := range diagnostic.Fixes {
			if fix.Title == "Search for the text instead" {
				return fix
			}
		}
	}
	t.Fatalf("no text fallback offered for %q", raw)
	return protocol.Fix{}
}

func TestValueCompletionPreservesSuggestedText(t *testing.T) {
	for _, value := range []string{
		`copy_process\ worker\`,
		`copy_process\"worker`,
		"copy_process\nworker",
		"copy_process\rworker",
		`/copy_process/`,
		`copy_process\\worker`,
	} {
		resolver := fakeResolver{symbols: []SymbolStat{{Name: value, Kind: protocol.SymbolKindFunction, Definitions: 1}}}
		const raw = "sym: handle_mm_fault"
		completions := Complete(raw, len("sym:"), resolver, fixedNow)
		if len(completions) != 1 {
			t.Fatalf("Complete(%q) = %v, want one suggestion", value, completions)
		}
		fixed := ApplyFix(raw, completions[0].Insert)
		repaired := Parse(fixed, resolver)
		if len(repaired.Diagnostics) != 0 || repaired.Root == nil || len(repaired.Root.Children) != 2 {
			t.Fatalf("completion %q gives %q: %s, diagnostics %v", value, fixed, shape(repaired.Root), codes(repaired.Diagnostics))
		}
		symbol, following := repaired.Root.Children[0], repaired.Root.Children[1]
		literal := symbol.Match == protocol.MatchLiteral || symbol.Match == protocol.MatchPhrase
		if symbol.Op != protocol.OpNameSym || symbol.Value != value || !literal || following.Value != "handle_mm_fault" {
			t.Errorf("completion %q gives %q: %s", value, fixed, shape(repaired.Root))
		}
	}
}
