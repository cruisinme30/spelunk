package query

import (
	"regexp/syntax"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// This file is how a regex comes to match across lines. Matching is per
// line, as in ripgrep, except for a regex with \n in it: like VS Code's
// search, that is the sign it is meant to span lines, so it is matched
// against the whole file instead. Every other regex keeps matching one line
// at a time, so \s and [^x] still never run onto the next line in it.

// spansLines reports whether a regex pattern has a newline in it: a \n
// escape anywhere, even in a class such as [^\n] (harmless: it still stops
// at the end of a line), or a newline written another way (\x0a, \012).
func spansLines(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			if pattern[i+1] == 'n' {
				return true
			}
			i++ // an escaped backslash can't start an escape
		}
	}
	parsed, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return false
	}
	return hasNewlineLiteral(parsed)
}

// hasNewlineLiteral reports whether re matches a newline literally.
func hasNewlineLiteral(re *syntax.Regexp) bool {
	if re.Op == syntax.OpLiteral && strings.ContainsRune(string(re.Rune), '\n') {
		return true
	}
	for _, sub := range re.Sub {
		if hasNewlineLiteral(sub) {
			return true
		}
	}
	return false
}

// reportMultilineInHistory warns about each regex with \n in a history
// query: commits are searched one changed line at a time, so it can only
// match in current files.
func reportMultilineInHistory(root *protocol.Node, problems *diagnostics) {
	walk(root, func(node *protocol.Node) {
		if node.Kind == protocol.NodeKindText && node.Match == protocol.MatchRegex && spansLines(node.Value) {
			problems.add(protocol.SeverityWarning, DiagMultilineInHistory, node.Span,
				"A regex with \\n matches across lines only in current files; commits are searched one changed line at a time")
		}
	})
}
