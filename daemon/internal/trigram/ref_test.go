package trigram

import (
	"strings"
	"testing"
)

// Full shas, of a SHA-1 and of a SHA-256 repo.
var (
	sha40 = strings.Repeat("3f9c2e1a", 5)
	sha64 = strings.Repeat("3f9c2e1a", 8)
)

func TestRefsRoundTrip(t *testing.T) {
	refs := []Ref{
		{PlanID: 3, RepoID: "r1", Path: "src/a|b.go", Line: 12, Column: 4, Length: 6},
		{PlanID: 4, RepoID: "r1", Path: "src/a.go", Line: 12, Column: 4, Length: 9, EndLine: 14, EndColumn: 7}, // spans lines
		{PlanID: 0, RepoID: "r2", Path: "README.md"},                                                           // a file-name result
		{PlanID: 5, RepoID: "r1", Path: "src/a.go", Line: 3, Column: 1, Length: 2, Revision: sha40},            // at a revision
		{PlanID: 5, RepoID: "r1", Path: "src/a.go", Revision: sha64},                                           // in a SHA-256 repo
	}
	for _, want := range refs {
		got, ok := ParseRef(want.String())
		if !ok || got != want {
			t.Errorf("ParseRef(%q) = %+v, %v; want %+v", want.String(), got, ok, want)
		}
	}
}

func TestMalformedRefsAreRejected(t *testing.T) {
	for _, text := range []string{
		"", "tree", "commit|1|r|abc", "tree|x|r|1|0|1|0|0|a.go", "tree|1|r|-1|0|1|0|0|a.go", "tree|1|r|1|0|1|-2|0|a.go",
		"tree|1|r|1|0|1|a.go", // without where the match ends
		"tree|1||1|0|1|0|0|a.go", "tree|1|r|1|0|1|0|0|",
		"tree|1|r|1|0|1|0|0|../outside.go", "tree|1|r|1|0|1|0|0|/etc/passwd", "tree|1|r|1|0|1|0|0|a/../../b",
		// A revision is a full sha: anything else would reach git as a name.
		"tree@|1|r|1|0|1|0|0|a.go", "tree@HEAD|1|r|1|0|1|0|0|a.go", "tree@3f9c2e1|1|r|1|0|1|0|0|a.go",
		"tree@--output=x|1|r|1|0|1|0|0|a.go", "tree@" + sha40 + "x|1|r|1|0|1|0|0|a.go",
		"tree@" + strings.ToUpper(sha40) + "|1|r|1|0|1|0|0|a.go", "trees@" + sha40 + "|1|r|1|0|1|0|0|a.go",
	} {
		if ref, ok := ParseRef(text); ok {
			t.Errorf("ParseRef(%q) = %+v, want rejected", text, ref)
		}
	}
}
