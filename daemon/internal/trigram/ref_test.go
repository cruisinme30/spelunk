package trigram

import "testing"

func TestRefsRoundTrip(t *testing.T) {
	refs := []Ref{
		{PlanID: 3, RepoID: "r1", Path: "src/a|b.go", Line: 12, Column: 4, Length: 6},
		{PlanID: 0, RepoID: "r2", Path: "README.md"}, // a file-name result
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
		"", "tree", "commit|1|r|abc", "tree|x|r|1|0|1|a.go", "tree|1|r|-1|0|1|a.go",
		"tree|1||1|0|1|a.go", "tree|1|r|1|0|1|",
		"tree|1|r|1|0|1|../outside.go", "tree|1|r|1|0|1|/etc/passwd", "tree|1|r|1|0|1|a/../../b",
	} {
		if ref, ok := ParseRef(text); ok {
			t.Errorf("ParseRef(%q) = %+v, want rejected", text, ref)
		}
	}
}
