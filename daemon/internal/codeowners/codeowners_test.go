package codeowners

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// githubExample is the example CODEOWNERS file from GitHub's documentation,
// trimmed to the lines whose rules differ, with other owner names.
const githubExample = `# This is a comment.
# These owners will be the default owners for everything in the repo.
*       @global-owner1 @global-owner2

# Order is important; the last matching pattern takes the most precedence.
*.js    @js-owner #This is an inline comment.

*.go docs@example.com

*.txt @acme/writers

/build/logs/ @docs-team

docs/*  docs@example.com

apps/ @mona

/docs/ @docs-team

/scripts/ @docs-team @mona

**/logs @mona

/apps/ @mona
/apps/github
`

func TestMatchFollowsGitHubsExample(t *testing.T) {
	// @covers op:owner
	file := Parse(".github/CODEOWNERS", []byte(githubExample))
	tests := []struct {
		path string
		want []string
	}{
		{"README.md", []string{"@global-owner1", "@global-owner2"}},
		{"web/app.js", []string{"@js-owner"}},
		{"cmd/main.go", []string{"docs@example.com"}},
		{"notes/todo.txt", []string{"@acme/writers"}},
		// /build/logs/ is anchored, and takes every file under it.
		{"build/logs/today/run.log", []string{"@mona"}}, // **/logs is later and also matches
		{"build/logs/run.md", []string{"@mona"}},
		{"build/run.md", []string{"@global-owner1", "@global-owner2"}},
		// docs/* only takes the folder's own files; /docs/ (later) takes them all.
		{"docs/getting-started.md", []string{"@docs-team"}},
		{"docs/build-app/troubleshooting.md", []string{"@docs-team"}},
		// apps/ matches a folder at any depth.
		{"src/apps/main.c", []string{"@mona"}},
		{"scripts/deploy.sh", []string{"@docs-team", "@mona"}},
		// **/logs matches a logs folder anywhere, and a logs file.
		{"deep/inside/logs/a.c", []string{"@mona"}},
		// /apps/github has no owners, so it is unowned though /apps/ owns the rest.
		{"apps/github/index.c", nil},
		{"apps/github", nil},
		{"apps/other.c", []string{"@mona"}},
	}
	for _, tt := range tests {
		if got := file.Owners(tt.path); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Owners(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestMatchSaysWhichLineDecided(t *testing.T) {
	// @covers op:owner
	file := Parse("CODEOWNERS", []byte(githubExample))
	rule, ok := file.Match("web/app.js")
	if !ok || rule.Line != 6 || rule.Pattern != "*.js" {
		t.Errorf("Match(web/app.js) = line %d %q (%v), want line 6 *.js", rule.Line, rule.Pattern, ok)
	}
	if _, ok := Parse("CODEOWNERS", []byte("/docs/ @a\n")).Match("src/a.go"); ok {
		t.Error("Match(src/a.go) matched, want no rule")
	}
}

func TestPatterns(t *testing.T) {
	// @covers op:owner
	tests := []struct {
		pattern string
		matches []string
		misses  []string
	}{
		{"*", []string{"a", "a/b/c.go"}, nil},
		{"/*", []string{"README.md"}, []string{"src/a.go"}},
		{"/**", []string{"README.md", "src/a/b.go"}, nil},
		{"*.py", []string{"a.py", "src/b.py"}, []string{"a.pyc", "a.py.txt"}},
		{"src/*.py", []string{"src/a.py"}, []string{"src/x/a.py", "lib/src/a.py"}},
		{"src/**/*.py", []string{"src/a.py", "src/x/y/a.py"}, []string{"a.py"}},
		{"payments", []string{"payments", "payments/a.go", "src/payments/a.go"}, []string{"payments-api/a.go"}},
		{"/payments", []string{"payments/a.go"}, []string{"src/payments/a.go"}},
		{"payments/", []string{"payments/a.go", "src/payments/a.go"}, []string{"payments"}},
		{"src/payments", []string{"src/payments/a.go", "src/payments"}, []string{"lib/src/payments/a.go"}},
		{"file?.txt", []string{"file1.txt"}, []string{"file10.txt", "file/.txt"}},
		{`my\ folder/`, []string{"my folder/a.txt"}, []string{"my/a.txt"}},
		{"Docs/", []string{"Docs/a.md"}, []string{"docs/a.md"}}, // paths are case-sensitive
		{"a+b(1).txt", []string{"a+b(1).txt"}, []string{"aab1.txt"}},
	}
	for _, tt := range tests {
		file := Parse("CODEOWNERS", []byte(tt.pattern+" @owner\n"))
		if len(file.Rules) != 1 {
			t.Errorf("Parse(%q) has %d rules, want 1", tt.pattern, len(file.Rules))
			continue
		}
		for _, path := range tt.matches {
			if _, ok := file.Match(path); !ok {
				t.Errorf("%q doesn't match %q, want it to", tt.pattern, path)
			}
		}
		for _, path := range tt.misses {
			if _, ok := file.Match(path); ok {
				t.Errorf("%q matches %q, want it not to", tt.pattern, path)
			}
		}
	}
}

func TestParseSkipsWhatGitHubSkips(t *testing.T) {
	// @covers op:owner
	content := strings.Join([]string{
		"!*.md @a",          // 1: negation isn't supported
		"[abc].go @a",       // 2: character ranges aren't supported
		"*.go not-an-owner", // 3: an owner must be a @user, @org/team or email
		"*.go @a/b/c",       // 4: a team has one slash
		"/ @a",              // 5: no pattern left
		"",                  // 6
		"   # an indented comment",
		"*.go\t@a  @b/c   x@y.io",  // 8: tabs and runs of spaces split fields
		"*.rb @-bad",               // 9: a handle can't start with -
		"*.c @a # a trailing note", // 10
	}, "\n")
	file := Parse("CODEOWNERS", []byte(content))
	if want := []int{1, 2, 3, 4, 5, 9}; !reflect.DeepEqual(file.Skipped, want) {
		t.Errorf("Skipped = %v, want %v", file.Skipped, want)
	}
	var got []string
	for _, rule := range file.Rules {
		got = append(got, rule.Pattern+" "+strings.Join(rule.Owners, ","))
	}
	if want := []string{"*.go @a,@b/c,x@y.io", "*.c @a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Rules = %q, want %q", got, want)
	}
}

func TestParseKeepsAPatternWithNoOwners(t *testing.T) {
	// @covers op:owner
	file := Parse("CODEOWNERS", []byte("* @a\n/vendor/\r\n"))
	if len(file.Rules) != 2 || file.Owners("vendor/x.go") != nil || file.Owners("x.go")[0] != "@a" {
		t.Errorf("Rules = %+v; want vendor/ unowned and the rest @a", file.Rules)
	}
}

func TestSameOwner(t *testing.T) {
	// @covers op:owner
	tests := []struct {
		owner, value string
		want         bool
	}{
		{"@jane", "@jane", true},
		{"@jane", "jane", true},
		{"@Jane", "JANE", true},
		{"@jane", "jan", false},
		{"@janet", "jane", false},
		{"@acme/payments-core", "@acme/payments-core", true},
		{"@acme/payments-core", "acme/payments-core", true},
		{"@acme/payments-core", "payments-core", true},
		{"@acme/payments-core", "@payments-core", true},
		{"@acme/payments-core", "acme", false},
		{"@acme/payments-core", "payments", false},
		{"jane@example.com", "jane@example.com", true},
		{"jane@example.com", "JANE@example.com", true},
		{"jane@example.com", "jane", false},
		{"jane@example.com", "example.com", false},
	}
	for _, tt := range tests {
		if got := SameOwner(tt.owner, tt.value); got != tt.want {
			t.Errorf("SameOwner(%q, %q) = %v, want %v", tt.owner, tt.value, got, tt.want)
		}
	}
}

func TestLoadUsesTheFirstLocationGitHubFinds(t *testing.T) {
	// @covers op:owner
	write := func(t *testing.T, root, path, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("none", func(t *testing.T) {
		if file := Load(t.TempDir(), nil); file != nil {
			t.Errorf("Load(empty) = %+v, want nil", file)
		}
	})
	t.Run("docs_only", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "docs/CODEOWNERS", "* @docs\n")
		if file := Load(root, nil); file == nil || file.Path != "docs/CODEOWNERS" || file.Owners("a")[0] != "@docs" {
			t.Errorf("Load = %+v, want docs/CODEOWNERS", file)
		}
	})
	t.Run("github_wins", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "docs/CODEOWNERS", "* @docs\n")
		write(t, root, "CODEOWNERS", "* @root\n")
		write(t, root, ".github/CODEOWNERS", "* @github\n")
		if file := Load(root, nil); file == nil || file.Path != ".github/CODEOWNERS" || file.Owners("a")[0] != "@github" {
			t.Errorf("Load = %+v, want .github/CODEOWNERS", file)
		}
	})
	t.Run("a_folder_named_CODEOWNERS_is_skipped", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, ".github/CODEOWNERS/x", "")
		write(t, root, "CODEOWNERS", "* @root\n")
		if file := Load(root, nil); file == nil || file.Path != "CODEOWNERS" {
			t.Errorf("Load = %+v, want CODEOWNERS", file)
		}
	})
	t.Run("too_big", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "CODEOWNERS", "* @a\n"+strings.Repeat("#", maxFileBytes))
		if file := Load(root, nil); file != nil {
			t.Errorf("Load(too big) = %+v, want nil", file)
		}
	})
}
