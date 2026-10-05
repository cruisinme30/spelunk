package query

import (
	"regexp"
	"slices"
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

// operator describes one operator: where it may be used, how its value is
// checked, and what completions offer for it.
type operator struct {
	// name is the operator in the parsed query, whichever spelling was typed.
	name protocol.OpName
	// full and short are the spellings people type: file: and f:. name is
	// accepted too, so lang:, sym: and msg: keep working.
	full, short string
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

// operators lists every operator in completion order, most used first. A
// word that is exactly a short name offers that operator first, so "s"
// offers symbol: before since:.
var operators = []operator{
	{name: protocol.OpNameF, full: "file", short: "f", summary: "File path, as a regex or a glob", examples: []string{"f:*.py", `f:\.py$`, "f:src/", "f:test"}, interpret: asPathRegex},
	{name: protocol.OpNameAuthor, full: "author", short: "a", scope: scopeHistoryOnly, summary: "Commits by this person", interpret: asName},
	{name: protocol.OpNameSince, full: "since", short: "d", summary: "Only changes inside a time window", examples: []string{"since:30d", "since:2w", "since:6m", "since:1y", "since:today", "since:yesterday", "since:2h"}, interpret: asDuration},
	{name: protocol.OpNameSym, full: "symbol", short: "s", scope: scopeWorkingTreeOnly, summary: "Symbol definitions", interpret: asText},
	{name: protocol.OpNameLang, full: "language", short: "l", summary: "Programming language", examples: []string{"lang:python", "lang:go", "lang:typescript"}, interpret: asLanguage},
	{name: protocol.OpNameRepo, full: "repo", short: "r", summary: "Repo name, as a regex or a glob", interpret: asPathRegex},
	{name: protocol.OpNameMsg, full: "message", short: "m", scope: scopeHistoryOnly, summary: "Words in the commit message", interpret: asText},
	{name: protocol.OpNameType, full: "type", short: "t", global: true, summary: "Only file names, code, or commits", examples: []string{"type:file", "type:code", "type:commit"}, interpret: oneOf("file", "code", "commit")},
	{name: protocol.OpNameCase, full: "case", short: "c", global: true, summary: "Match case (yes) or ignore it (no)", examples: []string{"case:yes", "case:no"}, interpret: oneOf("yes", "no")},
	{name: protocol.OpNameCount, full: "count", short: "n", global: true, summary: "How many results", examples: []string{"count:50", "count:200", "count:all"}, interpret: asCount},
}

// lookupOperator finds an operator by any of its spellings: f, file.
func lookupOperator(name string) (operator, bool) {
	for _, op := range operators {
		if op.accepts(name) {
			return op, true
		}
	}
	return operator{}, false
}

// spellings are the names an operator answers to, full name first.
func (op operator) spellings() []string {
	return []string{op.full, op.short, op.name}
}

func (op operator) accepts(name string) bool {
	return slices.Contains(op.spellings(), name)
}

// operatorNames are the full names, which unknown-operator fixes offer.
func operatorNames() []string {
	names := make([]string, len(operators))
	for i, op := range operators {
		names[i] = op.full
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

// asPathRegex: f: and repo: are regexes even without slashes, or globs when
// they read as one (*.go); quotes make them literal.
func asPathRegex(value string, form valueForm) (protocol.Match, string) {
	if form == formQuoted {
		return protocol.MatchPhrase, ""
	}
	problem := regexProblem(value)
	if looksLikeGlob(value, problem != "") {
		return protocol.MatchGlob, ""
	}
	return protocol.MatchRegex, problem
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

// durationPattern is a since: window: a count and a unit, which is min
// (minutes), h (hours), d (days), w (weeks), m (months) or y (years).
var durationPattern = regexp.MustCompile(`^([1-9]\d{0,4})(min|h|d|w|m|y)$`)

// The since: values that name a day rather than a length of time.
const (
	sinceToday     = "today"
	sinceYesterday = "yesterday"
)

// asDuration: since: takes today, yesterday, or <n> and a unit (see durationPattern).
func asDuration(value string, form valueForm) (protocol.Match, string) {
	day := strings.ToLower(value)
	if form != formBare || (day != sinceToday && day != sinceYesterday && !durationPattern.MatchString(value)) {
		return protocol.MatchLiteral, "since: takes today, yesterday, or a number and a unit: min, h, d, w, m (months) or y"
	}
	return protocol.MatchLiteral, ""
}

// monthsMeantAsMinutes returns the minutes reading of a since: value such
// as 30m, which means 30 months but was likely typed for 30 minutes: more
// than a year's worth of months. It returns "" for every other value.
func monthsMeantAsMinutes(value string) string {
	parts := durationPattern.FindStringSubmatch(value)
	if len(parts) != 3 || parts[2] != "m" {
		return ""
	}
	if months, _ := strconv.Atoi(parts[1]); months <= monthsInYear {
		return ""
	}
	return parts[1] + "min"
}

const monthsInYear = 12

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
