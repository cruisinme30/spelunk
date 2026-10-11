// Package excludes matches paths against VS Code's files.exclude and
// search.exclude, as VS Code's own search applies them: each pattern is a
// VS Code glob relative to its workspace folder, a pattern that matches a
// folder hides everything inside it, and a when clause hides a file only
// beside a sibling of the name it gives.
//
// The dialect is VS Code's, not index.exclude's: a pattern without a slash
// matches at the top of the folder only (build is ./build, not
// http/build), braces give alternatives ({dist,out}), and [abc] is a class.
package excludes

import (
	"os"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// Set holds the compiled patterns of every workspace folder, by root id.
// A nil Set hides nothing.
type Set struct {
	byRoot map[string][]pattern
	// siblings caches whether a when clause's sibling exists, by absolute
	// path, so a search stats each one once.
	siblings sync.Map
}

// pattern is one compiled files.exclude or search.exclude entry.
type pattern struct {
	glob string // as written, for the hidden-results note
	re   *regexp.Regexp
	when string // the when clause, or ""
}

// New compiles the settings' editor excludes, skipping patterns that
// don't compile. It returns nil when no folder has any.
func New(roots []protocol.RootExcludes) *Set {
	set := &Set{byRoot: map[string][]pattern{}}
	for _, root := range roots {
		for _, p := range root.Patterns {
			re, err := compile(p.Glob)
			if err != nil {
				continue
			}
			set.byRoot[root.RootID] = append(set.byRoot[root.RootID], pattern{glob: p.Glob, re: re, when: p.When})
		}
	}
	if len(set.byRoot) == 0 {
		return nil
	}
	return set
}

// Hiding returns the pattern that hides rel, a slash-separated path in the
// folder with id rootID at the absolute path root, or "" when none does.
// A when clause is checked against the files on disk under root.
func (s *Set) Hiding(rootID, root, rel string) string {
	if s == nil || rel == "" {
		return ""
	}
	for _, p := range s.byRoot[rootID] {
		m := p.re.FindStringSubmatch(rel)
		if m == nil {
			continue
		}
		if p.when != "" && !s.siblingExists(root, m[1], p.when) {
			continue
		}
		return p.glob
	}
	return ""
}

// siblingExists reports whether the when clause names a file beside
// matched (the part of the path the pattern matched): $(basename) stands
// for matched's name without its extension, so $(basename).ts beside
// src/a.js is src/a.ts.
func (s *Set) siblingExists(root, matched, when string) bool {
	name := path.Base(matched)
	base := strings.TrimSuffix(name, path.Ext(name))
	sibling := path.Join(root, path.Dir(matched), strings.ReplaceAll(when, "$(basename)", base))
	if cached, ok := s.siblings.Load(sibling); ok {
		found, _ := cached.(bool)
		return found
	}
	_, err := os.Stat(sibling)
	found := err == nil
	s.siblings.Store(sibling, found)
	return found
}

// compile turns a VS Code glob into a regex that matches a path the
// pattern hides: the path itself, or anything inside a folder it matches.
// Group 1 is the part the pattern matched.
func compile(glob string) (*regexp.Regexp, error) {
	glob = strings.TrimPrefix(glob, "./")
	glob = strings.TrimPrefix(glob, "/")
	if trimmed := strings.TrimSuffix(glob, "/"); trimmed != "" {
		glob = trimmed // a folder hides its contents anyway
	}
	return regexp.Compile("^(" + body(glob) + ")(?:/.*)?$")
}

// body is the regex for a glob's wildcards: ** spans folders (and **/
// may match no folder at all), * and ? stay within one, {a,b} is either
// alternative, [abc] and [!abc] are classes, and everything else matches
// itself.
func body(glob string) string {
	var b strings.Builder
	for i := 0; i < len(glob); i++ {
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case glob[i] == '*':
			b.WriteString("[^/]*")
		case glob[i] == '?':
			b.WriteString("[^/]")
		case glob[i] == '{':
			end := closing(glob, i)
			if end < 0 {
				b.WriteString(regexp.QuoteMeta("{"))
				continue
			}
			alternatives := splitTop(glob[i+1 : end])
			for j, alternative := range alternatives {
				alternatives[j] = body(alternative)
			}
			b.WriteString("(?:" + strings.Join(alternatives, "|") + ")")
			i = end
		case glob[i] == '[':
			end := strings.IndexByte(glob[i+1:], ']')
			if end < 1 {
				b.WriteString(regexp.QuoteMeta("["))
				continue
			}
			class := glob[i+1 : i+1+end]
			if class[0] == '!' {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		}
	}
	return b.String()
}

// closing returns the index of the } that closes the { at open, counting
// nested braces, or -1.
func closing(glob string, open int) int {
	depth := 0
	for i := open; i < len(glob); i++ {
		switch glob[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTop splits a brace's contents at its top-level commas.
func splitTop(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i := range len(s) {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}
