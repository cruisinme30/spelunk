package excludes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// set compiles patterns for one root, "r".
func set(patterns ...protocol.ExcludePattern) *Set {
	return New([]protocol.RootExcludes{{RootID: "r", Patterns: patterns}})
}

func globs(globs ...string) []protocol.ExcludePattern {
	patterns := make([]protocol.ExcludePattern, len(globs))
	for i, g := range globs {
		patterns[i] = protocol.ExcludePattern{Glob: g}
	}
	return patterns
}

func TestHidingMatchesLikeVSCode(t *testing.T) {
	tests := []struct {
		glob, path string
		hidden     bool
	}{
		{"**/node_modules", "node_modules/a.js", true},
		{"**/node_modules", "web/node_modules/a/b.js", true},
		{"**/node_modules", "web/node_modules_old/a.js", false},
		// Without a slash, VS Code anchors the pattern at the folder.
		{"build", "build/out.py", true},
		{"build", "http/build/steps.py", false},
		{"docs/generated", "docs/generated/api.md", true},
		{"docs/generated/", "docs/generated/api.md", true},
		{"./dist", "dist/a.js", true},
		{"**/*.min.js", "web/app.min.js", true},
		{"**/*.min.js", "web/app.js", false},
		{"**/{dist,out}", "a/out/x.js", true},
		{"**/{dist,out}", "a/output/x.js", false},
		{"**/*.{png,jp{e,}g}", "img/a.jpeg", true},
		{"**/*.{png,jp{e,}g}", "img/a.jpg", true},
		{"**/*.{png,jp{e,}g}", "img/a.gif", false},
		{"**/[Tt]emp", "x/Temp/a", true},
		{"**/[!T]emp", "x/Temp/a", false},
		{"logs/**", "logs/today.log", true},
		{"*.log", "a.log", true},
		{"*.log", "a/b.log", false},
		{"file?.txt", "file1.txt", true},
		{"a+b(c)", "a+b(c)", true},
	}
	for _, tt := range tests {
		got := set(globs(tt.glob)...).Hiding("r", "/nowhere", tt.path) != ""
		if got != tt.hidden {
			t.Errorf("%q hides %q = %v, want %v", tt.glob, tt.path, got, tt.hidden)
		}
	}
}

func TestHidingNamesThePatternOfItsOwnRoot(t *testing.T) {
	s := New([]protocol.RootExcludes{
		{RootID: "a", Patterns: globs("dist", "**/coverage")},
		{RootID: "b", Patterns: globs("build")},
	})
	if got := s.Hiding("a", "/a", "src/coverage/x.html"); got != "**/coverage" {
		t.Errorf("Hiding = %q, want **/coverage", got)
	}
	if got := s.Hiding("b", "/b", "dist/x.js"); got != "" {
		t.Errorf("root b hides dist/x.js by %q, but only root a has dist", got)
	}
	if got := s.Hiding("c", "/c", "build/x"); got != "" {
		t.Errorf("a root without patterns hides by %q", got)
	}
}

func TestHidingAFileOnlyBesideTheSiblingItsWhenClauseNames(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"src/checkout.ts", "src/checkout.js", "src/legacy.js"} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := set(protocol.ExcludePattern{Glob: "**/*.js", When: "$(basename).ts"})
	if got := s.Hiding("r", root, "src/checkout.js"); got != "**/*.js" {
		t.Errorf("checkout.js beside checkout.ts: Hiding = %q, want **/*.js", got)
	}
	if got := s.Hiding("r", root, "src/legacy.js"); got != "" {
		t.Errorf("legacy.js has no .ts beside it, but Hiding = %q", got)
	}
}

func TestNewWithNoPatternsHidesNothing(t *testing.T) {
	var none *Set
	if New(nil) != none || New([]protocol.RootExcludes{{RootID: "r"}}) != none {
		t.Error("New without patterns isn't nil")
	}
	if got := none.Hiding("r", "/r", "a"); got != "" {
		t.Errorf("a nil Set hides by %q", got)
	}
}
