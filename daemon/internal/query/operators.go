package query

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cruisinme30/unified-search/daemon/internal/lang"
	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// scope says where an operator can be used.
type scope int

const (
	scopeBoth scope = iota
	scopeHistoryOnly
	scopeWorkingTreeOnly
)

// operator is one row of the operator table (docs/dev/implementation-plan.md, "Operator semantics").
type operator struct {
	name protocol.OpName
	// global operators apply to the whole query and may appear once, at the top level.
	global bool
	scope  scope
	// summary is the one-line description shown in completions.
	summary string
	// examples are valid values, offered as fixes for a bad value and as completions.
	examples []string
	// interpret checks a value and says how it matches. A non-empty problem is a bad_value.
	interpret func(value string, form valueForm) (match protocol.Match, problem string)
}

// operators lists every operator in completion order: most used first, so
// "s" offers since: before sym:.
var operators = []operator{
	{name: protocol.OpNameF, summary: "File path, as a regex", examples: []string{`f:\.py$`, "f:src/", "f:test"}, interpret: asPathRegex},
	{name: protocol.OpNameAuthor, scope: scopeHistoryOnly, summary: "Commits by this person", interpret: asName},
	{name: protocol.OpNameSince, summary: "Only changes inside a time window", examples: []string{"since:30d", "since:2w", "since:6m", "since:1y"}, interpret: asDuration},
	{name: protocol.OpNameSym, scope: scopeWorkingTreeOnly, summary: "Symbol definitions", interpret: asText},
	{name: protocol.OpNameLang, summary: "Programming language", examples: []string{"lang:python", "lang:go", "lang:typescript"}, interpret: asLanguage},
	{name: protocol.OpNameRepo, summary: "Repo name, as a regex", interpret: asPathRegex},
	{name: protocol.OpNameMsg, scope: scopeHistoryOnly, summary: "Words in the commit message", interpret: asText},
	{name: protocol.OpNameType, global: true, summary: "Only file names, code, or commits", examples: []string{"type:file", "type:code", "type:commit"}, interpret: oneOf("file", "code", "commit")},
	{name: protocol.OpNameCase, global: true, summary: "Match case (yes) or ignore it (no)", examples: []string{"case:yes", "case:no"}, interpret: oneOf("yes", "no")},
	{name: protocol.OpNameCount, global: true, summary: "How many results", examples: []string{"count:50", "count:200", "count:all"}, interpret: asCount},
}

func lookupOperator(name string) (operator, bool) {
	for _, op := range operators {
		if op.name == name {
			return op, true
		}
	}
	return operator{}, false
}

func operatorNames() []string {
	names := make([]string, len(operators))
	for i, op := range operators {
		names[i] = op.name
	}
	return names
}

// regexProblem compiles pattern as RE2 and returns its error message, if any.
func regexProblem(pattern string) string {
	if _, err := regexp.Compile(pattern); err != nil {
		return "Not a valid RE2 regex: " + strings.TrimPrefix(err.Error(), "error parsing regexp: ")
	}
	return ""
}

// asPathRegex: f: and repo: are regexes even without slashes; quotes make them literal.
func asPathRegex(value string, form valueForm) (protocol.Match, string) {
	if form == formQuoted {
		return protocol.MatchPhrase, ""
	}
	return protocol.MatchRegex, regexProblem(value)
}

// asText: sym: and msg: take a literal, a "phrase" or a /regex/.
func asText(value string, form valueForm) (protocol.Match, string) {
	switch form {
	case formQuoted:
		return protocol.MatchPhrase, ""
	case formRegex:
		return protocol.MatchRegex, regexProblem(value)
	default:
		return protocol.MatchLiteral, ""
	}
}

// asName: author: takes a substring of a name or email, or a "full name".
func asName(_ string, form valueForm) (protocol.Match, string) {
	switch form {
	case formQuoted:
		return protocol.MatchPhrase, ""
	case formRegex:
		return protocol.MatchLiteral, "author: takes a name or email, not a regex"
	default:
		return protocol.MatchLiteral, ""
	}
}

var durationPattern = regexp.MustCompile(`^([1-9]\d{0,4})([dwmy])$`)

// asDuration: since: takes <n>d, <n>w, <n>m or <n>y.
func asDuration(value string, form valueForm) (protocol.Match, string) {
	if form != formBare || !durationPattern.MatchString(value) {
		return protocol.MatchLiteral, "since: takes a number and a unit: d, w, m or y"
	}
	return protocol.MatchLiteral, ""
}

// asLanguage: lang: takes a known language name or alias.
func asLanguage(value string, _ valueForm) (protocol.Match, string) {
	if _, ok := lang.Resolve(value); !ok {
		return protocol.MatchLiteral, "Unknown language " + strconv.Quote(value)
	}
	return protocol.MatchLiteral, ""
}

// nearestLanguages suggests up to three canonical languages for a mistyped lang: value.
func nearestLanguages(value string) []string {
	const limit = 3
	var names []string
	seen := map[string]bool{}
	for _, candidate := range nearest(strings.ToLower(value), lang.Values()) {
		name, _ := lang.Resolve(candidate)
		if !seen[name] && len(names) < limit {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// asCount: count: takes a positive number or "all".
func asCount(value string, _ valueForm) (protocol.Match, string) {
	if value == "all" {
		return protocol.MatchLiteral, ""
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return protocol.MatchLiteral, "count: takes a positive number or all"
	}
	return protocol.MatchLiteral, ""
}

func oneOf(values ...string) func(string, valueForm) (protocol.Match, string) {
	return func(value string, _ valueForm) (protocol.Match, string) {
		for _, v := range values {
			if value == v {
				return protocol.MatchLiteral, ""
			}
		}
		return protocol.MatchLiteral, "Use one of: " + strings.Join(values, ", ")
	}
}
