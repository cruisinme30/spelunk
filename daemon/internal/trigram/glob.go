package trigram

import (
	"regexp"
	"strings"
)

// Glob matches slash-separated paths against an index.exclude pattern:
// "**" spans directories, "*" and "?" stay within one, as in VS Code's
// files.exclude. A pattern without a slash matches at any depth, and a
// pattern that matches a folder also matches everything inside it.
// Brace alternatives ({a,b}) are not supported: braces match themselves.
type Glob struct{ re *regexp.Regexp }

// CompileGlob turns a glob into a matcher.
func CompileGlob(pattern string) (Glob, error) {
	if !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("(?:/.*)?$") // a matching folder takes its contents with it
	re, err := regexp.Compile(b.String())
	return Glob{re: re}, err
}

// Match reports whether path (slash-separated, relative to the repo) matches.
func (g Glob) Match(path string) bool { return g.re.MatchString(path) }

// Excluder holds the compiled index.exclude patterns.
type Excluder []Glob

// NewExcluder compiles patterns, skipping any that are invalid.
func NewExcluder(patterns []string) Excluder {
	var e Excluder
	for _, p := range patterns {
		if g, err := CompileGlob(p); err == nil {
			e = append(e, g)
		}
	}
	return e
}

// Excludes reports whether any pattern matches path.
func (e Excluder) Excludes(path string) bool {
	for _, g := range e {
		if g.Match(path) {
			return true
		}
	}
	return false
}
