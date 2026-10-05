package trigram

import (
	"regexp"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/glob"
)

// Glob matches slash-separated paths against an index.exclude pattern:
// "**" spans directories, "*" and "?" stay within one, as in VS Code's
// files.exclude. A pattern without a slash matches at any depth, and a
// pattern that matches a folder also matches everything inside it.
// Brace alternatives ({a,b}) are not supported: braces match themselves.
type Glob struct{ re *regexp.Regexp }

// compileGlob turns a glob into a matcher.
func compileGlob(pattern string) (Glob, error) {
	if !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	// A matching folder takes its contents with it.
	re, err := regexp.Compile("^" + glob.Body(pattern, false) + "(?:/.*)?$")
	return Glob{re: re}, err
}

// match reports whether path (slash-separated, relative to the repo) matches.
func (g Glob) match(path string) bool { return g.re.MatchString(path) }

// Excluder holds the compiled index.exclude patterns.
type Excluder []Glob

// NewExcluder compiles patterns, skipping any that are invalid.
func NewExcluder(patterns []string) Excluder {
	var e Excluder
	for _, p := range patterns {
		if g, err := compileGlob(p); err == nil {
			e = append(e, g)
		}
	}
	return e
}

// Excludes reports whether any pattern matches path.
func (e Excluder) Excludes(path string) bool {
	for _, g := range e {
		if g.match(path) {
			return true
		}
	}
	return false
}
