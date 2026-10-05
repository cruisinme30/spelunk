package glob

import (
	"regexp"
	"strings"
)

// Body returns the unanchored regex for pattern's wildcards: ** spans
// folders (and **/ may match no folder at all), * and ? stay within one,
// and everything else matches itself. With escapes, \x is a literal x;
// without, a backslash is an ordinary character.
func Body(pattern string, escapes bool) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case pattern[i] == '*':
			b.WriteString("[^/]*")
		case pattern[i] == '?':
			b.WriteString("[^/]")
		case escapes && pattern[i] == '\\' && i+1 < len(pattern):
			i++
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	return b.String()
}
