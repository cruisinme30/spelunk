package query

import (
	"regexp"
	"strings"
)

// looksLikeGlob reports whether an f: or repo: value was meant as a glob
// (*.go, src/**/test_*.py) rather than a regex.
//
// A * reads as a glob wildcard unless it follows ., ), ] or }, where it is
// regex syntax (.*, [a-z]*). In paths, test_* means "test_ and anything",
// not "test and any number of underscores", so a * after a plain character
// is a wildcard too. A ? reads as a wildcard only when the value isn't a
// valid regex, because regexes like docs? (doc or docs) are common.
func looksLikeGlob(value string, invalidRegex bool) bool {
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++ // an escaped character is never a wildcard
		case '*':
			if i == 0 || !strings.ContainsRune(".)]}", rune(value[i-1])) {
				return true
			}
		case '?':
			if invalidRegex {
				return true
			}
		}
	}
	return false
}

// globPattern turns a path glob into the regex it means. ** spans folders,
// * and ? stay within one, and \x is a literal x. Like VS Code's "files to
// include", a glob may start at any folder: *.go matches cmd/main.go, and
// src/*.ts matches web/src/app.ts. It always matches to the end of the
// path, so a trailing $ written out of regex habit is dropped, as is a
// leading ^.
func globPattern(glob string) string {
	glob = strings.TrimSuffix(strings.TrimPrefix(glob, "^"), "$")
	var b strings.Builder
	b.WriteString("(?:^|/)")
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
		case glob[i] == '\\' && i+1 < len(glob):
			i++
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		default:
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		}
	}
	b.WriteString("$")
	return b.String()
}
