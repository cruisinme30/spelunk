package query

import (
	"fmt"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// Guide is the query language in plain text, for the spelunk command's
// help and for AI agents, made from the operator table so it always names
// the operators the parser accepts.
func Guide() string {
	var b strings.Builder
	op := func(name protocol.OpName) string { return operatorNamed(name).full + ":" }
	b.WriteString("Spelunk query language\n\n")
	b.WriteString("Words are ANDed: retry timeout finds lines (and file names) with both.\n")
	b.WriteString("\"read timeout\" is a phrase. /Retry(Policy)?/ is an RE2 regex.\n")
	b.WriteString("OR joins alternatives: retry OR backoff. Parentheses group: (parse OR lex) error.\n")
	b.WriteString("- or NOT excludes anything: -test, NOT \"todo\", -" + op(protocol.OpNameF) + "vendor/.\n")
	b.WriteString("A query needs at least one thing to search for; it can't be only exclusions.\n\n")
	b.WriteString("Operators (name:value; a short name, where there is one, works the same):\n")
	for i := range operators {
		o := &operators[i]
		names := o.full + ":"
		if o.short != "" {
			names += " (" + o.short + ":)"
		}
		fmt.Fprintf(&b, "  %-22s %s", names, o.summary)
		switch o.scope {
		case scopeHistoryOnly:
			b.WriteString(" [commits only]")
		case scopeWorkingTreeOnly:
			b.WriteString(" [current files only]")
		case scopeBoth:
		}
		if o.global {
			b.WriteString(" [once per query, applies to all of it]")
		}
		b.WriteString("\n")
		if values := fixedValues[o.name]; len(values) > 0 {
			for _, v := range values {
				fmt.Fprintf(&b, "      %-18s %s\n", o.full+":"+v.value, v.detail)
			}
		} else if len(o.examples) > 0 {
			fmt.Fprintf(&b, "      e.g. %s\n", o.full+":"+strings.Join(o.examples, ", "+o.full+":"))
		}
	}
	commits := op(protocol.OpNameType) + "commit"
	b.WriteString("\nSearching history: " + commits + " searches commit messages and diffs; " +
		op(protocol.OpNameType) + TypeAdded + " and " + op(protocol.OpNameType) + TypeRemoved +
		" search only the lines commits added or removed. " + op(protocol.OpNameAuthor) + ", " +
		op(protocol.OpNameMsg) + ", " + op(protocol.OpNameSince) + " and " + op(protocol.OpNameUntil) + " narrow them.\n")
	b.WriteString("\nExamples:\n")
	for _, example := range []string{
		"retry " + op(protocol.OpNameLang) + "go -" + op(protocol.OpNameF) + "_test",
		op(protocol.OpNameSym) + "ParseQuery",
		op(protocol.OpNameKind) + "function " + op(protocol.OpNameSym) + "/^handle/",
		op(protocol.OpNameRef) + "ParseQuery " + op(protocol.OpNameF) + "src/",
		commits + " " + op(protocol.OpNameAuthor) + "jane " + op(protocol.OpNameSince) + "2w timeout",
		op(protocol.OpNameType) + TypeAdded + " " + op(protocol.OpNameSince) + "6m retryPolicy",
		op(protocol.OpNameAt) + "v1.0.0 retryPolicy",
	} {
		b.WriteString("  " + example + "\n")
	}
	return b.String()
}

// operatorNamed is the operator table's entry for name.
func operatorNamed(name protocol.OpName) *operator {
	for i := range operators {
		if operators[i].name == name {
			return &operators[i]
		}
	}
	panic("no operator " + name)
}
