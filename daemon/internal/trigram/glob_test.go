package trigram

import "testing"

func TestGlobs(t *testing.T) {
	// @covers setting:index.exclude
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"node_modules", "node_modules", true},
		{"node_modules", "web/node_modules", true},
		{"**/node_modules/**", "web/node_modules/react/index.js", true},
		{"*.min.js", "dist/app.min.js", true},
		{"*.min.js", "dist/app.js", false},
		{"dist/*.js", "dist/app.js", true},
		{"dist/*.js", "dist/sub/app.js", false},
		{"dist/**", "dist/sub/app.js", true},
		{"build/?.o", "build/a.o", true},
		{"build/?.o", "build/ab.o", false},
		{"a+b.txt", "a+b.txt", true}, // regex characters are literal
		{"a+b.txt", "aab.txt", false},
	}
	for _, tt := range tests {
		g, err := CompileGlob(tt.pattern)
		if err != nil {
			t.Fatalf("CompileGlob(%q): %v", tt.pattern, err)
		}
		if got := g.Match(tt.path); got != tt.want {
			t.Errorf("glob %q matching %q = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestExcluderMatchesAnyPattern(t *testing.T) {
	e := NewExcluder([]string{"**/vendor/**", "*.lock"})
	for path, want := range map[string]bool{"vendor/x.go": true, "a/vendor/b/c.go": true, "yarn.lock": true, "src/main.go": false} {
		if got := e.Excludes(path); got != want {
			t.Errorf("Excludes(%q) = %v, want %v", path, got, want)
		}
	}
}
