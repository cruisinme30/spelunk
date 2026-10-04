package query

import (
	"regexp"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

func TestPathValuesReadAsGlobOrRegex(t *testing.T) {
	tests := []struct {
		value string
		want  protocol.Match
	}{
		{"*.go$", protocol.MatchGlob},
		{"*.go", protocol.MatchGlob},
		{"src/**/*.ts", protocol.MatchGlob},
		{"test_*.py", protocol.MatchGlob},
		{"file?[1].txt[", protocol.MatchGlob}, // not a valid regex, so ? is a wildcard
		{`\.go$`, protocol.MatchRegex},
		{`.*test\.py$`, protocol.MatchRegex},
		{"[a-z]*.go", protocol.MatchRegex},
		{"docs?", protocol.MatchRegex},
		{`\*`, protocol.MatchRegex},
		{"src/", protocol.MatchRegex},
	}
	for _, tt := range tests {
		q := mustParseCleanly(t, "f:"+tt.value)
		if got := q.Root.Match; got != tt.want {
			t.Errorf("f:%s reads as %s, want %s", tt.value, got, tt.want)
		}
	}
}

func TestGlobsMatchPathsLikeVSCodeIncludes(t *testing.T) {
	tests := []struct {
		glob string
		yes  []string
		no   []string
	}{
		{"*.go$", []string{"main.go", "cmd/server/main.go"}, []string{"main.go.txt", "gopher.md", "go/README"}},
		{"*.go", []string{"internal/query/glob.go"}, []string{"glob.gox"}},
		{"src/*.ts", []string{"src/app.ts", "web/src/app.ts"}, []string{"src/lib/app.ts", "other/app.ts"}},
		{"src/**/*.ts", []string{"src/app.ts", "src/lib/deep/app.ts"}, []string{"lib/app.ts"}},
		{"test_*.py", []string{"tests/test_retry.py"}, []string{"tests/retry_test.py"}},
		{"file?.txt", []string{"file1.txt"}, []string{"file10.txt", "file/.txt"}},
		{`a\*b`, []string{"a*b"}, []string{"axb"}},
	}
	for _, tt := range tests {
		re := regexp.MustCompile(globPattern(tt.glob))
		for _, path := range tt.yes {
			if !re.MatchString(path) {
				t.Errorf("glob %s (regex %s) misses %s, want a match", tt.glob, re, path)
			}
		}
		for _, path := range tt.no {
			if re.MatchString(path) {
				t.Errorf("glob %s (regex %s) matches %s, want no match", tt.glob, re, path)
			}
		}
	}
}

func TestGlobFilterKeepsMatchingFiles(t *testing.T) {
	plan, _, err := NewPlan(mustParseCleanly(t, "f:*.GO"), defaultSettings, fixedNow, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Pred.Kids) != 1 {
		t.Fatalf("predicate has %d parts, want 1", len(plan.Pred.Kids))
	}
	path, ok := plan.Pred.Kids[0].(*Path)
	if !ok {
		t.Fatalf("predicate = %T, want *Path", plan.Pred.Kids[0])
	}
	if !path.Re.MatchString("cmd/main.go") {
		t.Errorf("f:*.GO (regex %s) misses cmd/main.go; matching ignores case by default", path.Re)
	}
}
