package trigram

import (
	"context"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/symbols"
)

func TestMatchRankOrdersExactPrefixContainsLetters(t *testing.T) {
	// cspell:ignore rtpl Ünïcode 𝒻ind
	for _, tc := range []struct {
		name, text string
		exact      bool
		rank       int
		ok         bool
	}{
		{"RetryPolicy", "retrypolicy", false, rankExact, true},
		{"RetryPolicy", "retry", false, rankPrefix, true},
		{"NewRetryPolicy", "retry", false, rankContains, true},
		{"RetryPolicy", "rtpl", false, rankLetters, true},
		{"RetryPolicy", "lpr", false, 0, false},
		{"RetryPolicy", "RetryPolicyX", false, 0, false},
		{"Ünïcode", "ünï", false, rankPrefix, true},
		{"naïve_𝒻ind", "ï𝒻", false, rankLetters, true},
		{"RetryPolicy", "RetryPolicy", true, rankExact, true},
		{"RetryPolicy", "retryPolicy", true, 0, false},
		{"NewRetryPolicy", "RetryPolicy", true, 0, false},
	} {
		rank, ok := matchRank(tc.name, tc.text, foldName(tc.text), tc.exact)
		if ok != tc.ok || ok && rank != tc.rank {
			t.Errorf("matchRank(%q, %q, exact=%v) = %d, %v; want %d, %v", tc.name, tc.text, tc.exact, rank, ok, tc.rank, tc.ok)
		}
	}
}

func TestFoundSymbolColumnCountsUTF16(t *testing.T) {
	doc := &Doc{Path: "a.go", Content: []byte("package a\n\nfunc (𝒻 ï) Find() {}\r\nfunc gone() {}\n")}
	for _, tc := range []struct {
		symbol symbols.Symbol
		want   int
	}{
		// 𝒻 takes two UTF-16 units and ï one, as VS Code counts.
		{symbols.Symbol{Name: "Find", Line: 3}, 13},
		{symbols.Symbol{Name: "package", Line: 1}, 1},
		// A name its line doesn't spell (the file changed) starts the line.
		{symbols.Symbol{Name: "Missing", Line: 4}, 1},
		{symbols.Symbol{Name: "PastTheEnd", Line: 99}, 1},
	} {
		got := FoundSymbol{Doc: doc, Symbol: &tc.symbol}.Column()
		if got != tc.want {
			t.Errorf("column of %s on line %d = %d, want %d", tc.symbol.Name, tc.symbol.Line, got, tc.want)
		}
	}
}

func TestFindSymbolsKeepsTheBestWithinItsLimit(t *testing.T) {
	// A one-letter query over many definitions keeps only the best, which
	// keepBest trims to as it goes.
	var defs []symbols.Symbol
	for i := range 5000 {
		defs = append(defs, symbols.Symbol{Name: "handler_" + string(rune('a'+i%26)) + "_long_name", Kind: protocol.SymbolKindFunction, Line: i + 1})
	}
	defs = append(defs, symbols.Symbol{Name: "h", Kind: protocol.SymbolKindFunction, Line: 9999})
	shard := &Shard{Docs: []Doc{{Path: "big.go", Lang: "go", Symbols: defs}}}
	repos := []Repo{{ID: "r", Name: "r", Root: "/r", Shard: shard}}
	found := FindSymbols(context.Background(), repos, SymbolQuery{Text: "h", Limit: 10})
	if len(found) != 10 {
		t.Fatalf("found %d, want 10", len(found))
	}
	if found[0].Symbol.Name != "h" {
		t.Errorf("best = %q, want the exact name h", found[0].Symbol.Name)
	}
}

func TestFindSymbolsStopsWhenCancelled(t *testing.T) {
	docs := make([]Doc, 3*checkEvery)
	for i := range docs {
		docs[i] = Doc{Path: "f" + string(rune('a'+i%26)), Symbols: []symbols.Symbol{{Name: "Policy", Line: 1}}}
	}
	repos := []Repo{{ID: "r", Name: "r", Root: "/r", Shard: &Shard{Docs: docs}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	query := SymbolQuery{Text: "policy", Limit: 10 * len(docs)}
	if all := FindSymbols(context.Background(), repos, query); len(all) != len(docs) {
		t.Fatalf("found %d, want all %d", len(all), len(docs))
	}
	if found := FindSymbols(ctx, repos, query); len(found) >= len(docs) {
		t.Errorf("a cancelled find still read all %d files", len(found))
	}
}
