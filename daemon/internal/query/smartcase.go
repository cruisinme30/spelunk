package query

import (
	"regexp/syntax"
	"strings"
	"unicode"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// hasCapital reports whether the text the query searches for (text terms,
// sym: and msg: values) has a capital letter, which makes smart case match
// case. Filters such as f: and repo: don't count, as ripgrep's --smart-case
// ignores its globs.
func hasCapital(root *protocol.Node) bool {
	found := false
	walk(root, func(n *protocol.Node) {
		searched := n.Kind == protocol.NodeKindText ||
			n.Kind == protocol.NodeKindOp && (n.Op == protocol.OpNameSym || n.Op == protocol.OpNameMsg)
		if !found && searched {
			found = valueHasCapital(n.Value, n.Match)
		}
	})
	return found
}

// valueHasCapital reports whether a value has a capital letter. In a regex
// only literal letters count, so \W, \S and \p{Lu} don't, nor do letters
// under (?i).
func valueHasCapital(value string, match protocol.Match) bool {
	if match != protocol.MatchRegex {
		return strings.IndexFunc(value, unicode.IsUpper) >= 0
	}
	re, err := syntax.Parse(value, syntax.Perl)
	if err != nil {
		return false // the parser reports it, and the query can't run
	}
	return literalHasCapital(re)
}

func literalHasCapital(re *syntax.Regexp) bool {
	if re.Op == syntax.OpLiteral && re.Flags&syntax.FoldCase == 0 {
		for _, r := range re.Rune {
			if unicode.IsUpper(r) {
				return true
			}
		}
	}
	for _, sub := range re.Sub {
		if literalHasCapital(sub) {
			return true
		}
	}
	return false
}
