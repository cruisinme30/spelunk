package query

import (
	"strings"
	"unicode/utf8"

	"github.com/cruisinme30/spelunk/daemon/internal/glob"
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
// leading ^. Invalid UTF-8, which RE2 refuses, becomes the replacement
// character, as it does in parsed values.
func globPattern(pattern string) string {
	pattern = strings.TrimSuffix(strings.TrimPrefix(strings.ToValidUTF8(pattern, string(utf8.RuneError)), "^"), "$")
	return "(?:^|/)" + glob.Body(pattern, true) + "$"
}
