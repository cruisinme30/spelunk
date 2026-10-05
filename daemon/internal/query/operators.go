package query

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
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
	// accepted too, so lang:, sym: and msg: keep working. content: has no
	// short name, since c: is case:.
	full, short string
	// aliases are other spellings people bring from other tools: path: for
	// file:, as GitHub and Sourcegraph spell it.
	aliases []string
	// global operators apply to the whole query and may appear once, at the top level.
	global bool
	scope  scope
	// summary is the one-line description shown in completions.
	summary string
	// examples are valid values, offered as fixes for a bad value.
	examples []string
	// interpret checks a value and says how it matches. A non-empty problem is a bad_value.
	interpret func(value string, form valueForm) (match protocol.Match, problem string)
}

// operators lists every operator in completion order, most used first. A
// word that is exactly a short name offers that operator first, so "s"
// offers symbol: before since:.
var operators = []operator{
	{name: protocol.OpNameF, full: "file", short: "f", aliases: []string{"path"}, summary: "File path, as a regex or a glob", examples: []string{"*.py", `\.py$`, "src/", "test"}, interpret: asPathRegex},
	{name: protocol.OpNameAuthor, full: "author", short: "a", scope: scopeHistoryOnly, summary: "Commits by this person", interpret: asName},
	{name: protocol.OpNameSince, full: "since", short: "d", summary: "Only changes inside a time window, or from a date on", examples: windowExamples, interpret: asWindow("since")},
	{name: protocol.OpNameUntil, full: "until", short: "u", summary: "Only changes older than a time window, or up to a date", examples: windowExamples, interpret: asWindow("until")},
	{name: protocol.OpNameSym, full: "symbol", short: "s", scope: scopeWorkingTreeOnly, summary: "Symbol definitions", interpret: asText},
	{name: protocol.OpNameKind, full: "kind", short: "k", scope: scopeWorkingTreeOnly, summary: "Only definitions of one kind", examples: SymbolKinds, interpret: oneOf(SymbolKinds...)},
	{name: protocol.OpNameRef, full: "ref", short: "x", scope: scopeWorkingTreeOnly, summary: "Whole-word uses of a name, without its definitions", interpret: asReference},
	{name: protocol.OpNameIs, full: "is", short: "i", summary: "Open, changed or test files", examples: []string{StateOpen, StateChanged, StateTest}, interpret: oneOf(StateOpen, StateChanged, StateTest)},
	{name: protocol.OpNameContent, full: "content", summary: "Text inside files only, never file names", examples: []string{"retry", `"read timeout"`, "/Retry(Policy)?/"}, interpret: asText},
	{name: protocol.OpNameLang, full: "language", short: "l", summary: "Programming language", examples: []string{"python", "go", "typescript"}, interpret: asLanguage},
	{name: protocol.OpNameRepo, full: "repo", short: "r", summary: "Repo name, as a regex or a glob", interpret: asPathRegex},
	{name: protocol.OpNameMsg, full: "message", short: "m", scope: scopeHistoryOnly, summary: "Words in the commit message", interpret: asText},
	{name: protocol.OpNameType, full: "type", short: "t", global: true, summary: "Only file names, code, commits, or lines commits added or removed", examples: []string{"file", "code", "commit", TypeAdded, TypeRemoved}, interpret: oneOf("file", "code", "commit", TypeAdded, TypeRemoved)},
	{name: protocol.OpNameCase, full: "case", short: "c", global: true, summary: "Match case (yes), ignore it (no), or match it when the query has a capital (smart)", examples: []string{"yes", "no", "smart"}, interpret: oneOf("yes", "no", "smart")},
	{name: protocol.OpNameWord, full: "word", short: "w", global: true, summary: "Match whole words only (yes) or parts of words too (no)", examples: []string{"yes", "no"}, interpret: oneOf("yes", "no")},
	{name: protocol.OpNameCount, full: "count", short: "n", global: true, summary: "How many results", examples: []string{"50", "200", "all"}, interpret: asCount},
	{name: protocol.OpNameOrder, full: "order", short: "o", global: true, scope: scopeWorkingTreeOnly, summary: "Best match first, or by path", examples: []string{"best", "path"}, interpret: oneOf("best", "path")},
}

// The values of is:, the file states it can match.
const (
	StateOpen    = "open"    // open in an editor tab
	StateChanged = "changed" // uncommitted changes
	StateTest    = "test"    // holds tests (lang.IsTest)
)

// The values of type: that search commits for lines on one side of their
// diffs: lines a commit added, or lines it removed.
const (
	TypeAdded   = "added"
	TypeRemoved = "removed"
)

// historyType reports whether a type: value searches commits.
func historyType(value string) bool {
	return value == "commit" || value == TypeAdded || value == TypeRemoved
}

// SymbolKinds are the values of kind:, the kinds of definition that
// internal/symbols tells apart.
var SymbolKinds = []string{
	protocol.SymbolKindFunction, protocol.SymbolKindMethod, protocol.SymbolKindClass,
	protocol.SymbolKindInterface, protocol.SymbolKindType, protocol.SymbolKindOther,
}

// currentState reports whether an is: value describes files as they are
// now (open, changed), which no commit can match. is:test is a rule about
// paths, so in a history query it picks the commits' test files, like f:.
func currentState(value string) bool {
	return value == StateOpen || value == StateChanged
}

// lookupOperator finds an operator by any of its spellings: f, file.
func lookupOperator(name string) (operator, bool) {
	for i := range operators {
		if op := &operators[i]; op.accepts(name) {
			return *op, true
		}
	}
	return operator{}, false
}

// spellings are the names an operator answers to, full name first.
func (op operator) spellings() []string {
	names := []string{op.full, op.name}
	if op.short != "" {
		names = append(names, op.short)
	}
	return append(names, op.aliases...)
}

func (op operator) accepts(name string) bool {
	return slices.Contains(op.spellings(), name)
}

// operatorNames are the full names, which unknown-operator fixes offer.
func operatorNames() []string {
	names := make([]string, len(operators))
	for i := range operators {
		names[i] = operators[i].full
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

// asText: sym:, msg: and content: take a literal, a "phrase" or a /regex/.
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

// asReference: ref: takes a name, or a "quoted name"; uses are found as
// whole words, so a regex would have nothing to anchor.
func asReference(_ string, form valueForm) (protocol.Match, string) {
	switch form {
	case formQuoted:
		return protocol.MatchPhrase, ""
	case formRegex:
		return protocol.MatchLiteral, "ref: takes a name, not a regex"
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

// durationPattern is a since: or until: window: a count and a unit, which
// is min (minutes), h (hours), d (days), w (weeks), m (months) or y (years).
var durationPattern = regexp.MustCompile(`^([1-9]\d{0,4})(min|h|d|w|m|y)$`)

// datePattern is a since: or until: date: a day (2026-09-30) or a month
// (2026-09). asWindow checks that it names a real one.
var datePattern = regexp.MustCompile(`^(\d{4})-(\d{2})(?:-(\d{2}))?$`)

// The since: and until: values that name a day rather than a length of time.
const (
	windowToday     = "today"
	windowYesterday = "yesterday"
)

// isWindowOp reports whether name is since: or until:, which take the same
// time windows.
func isWindowOp(name protocol.OpName) bool {
	return name == protocol.OpNameSince || name == protocol.OpNameUntil
}

// midnight is when now's day started, in now's time zone.
func midnight(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// windowExamples are the valid since: and until: values offered as fixes
// for a bad one, the first one first.
var windowExamples = []string{"30d", "2w", "6m", "1y", "today", "yesterday", "2h"}

// asWindow: since: and until: (named name) take today, yesterday, a date
// (see datePattern), or <n> and a unit (see durationPattern).
func asWindow(name string) func(string, valueForm) (protocol.Match, string) {
	return func(value string, form valueForm) (protocol.Match, string) {
		day := strings.ToLower(value)
		switch {
		case form != formBare:
		case day == windowToday || day == windowYesterday || durationPattern.MatchString(value):
			return protocol.MatchLiteral, ""
		case datePattern.MatchString(value):
			return protocol.MatchLiteral, dateProblem(value)
		}
		return protocol.MatchLiteral, name + ": takes today, yesterday, a date (2026-09-30 or 2026-09), or a number and a unit: min, h, d, w, m (months) or y"
	}
}

// dateProblem says why a value of datePattern's shape isn't a real day or
// month ("" when it is one): 2026-13, 2026-02-30.
func dateProblem(value string) string {
	year, month, day, hasDay := dateParts(value)
	switch {
	case month < 1 || month > monthsInYear:
		return value + " isn't a date: months go from 01 to 12"
	case hasDay && (day < 1 || day > daysIn(year, time.Month(month))):
		return fmt.Sprintf("%s isn't a date: %s %d has %d days", value, time.Month(month), year, daysIn(year, time.Month(month)))
	default:
		return ""
	}
}

// nearestDate turns a value of datePattern's shape into the nearest real
// date, keeping its form: 2026-02-30 → 2026-02-28, 2026-13 → 2026-12.
// It returns "" for any other value.
func nearestDate(value string) string {
	if !datePattern.MatchString(value) {
		return ""
	}
	year, month, day, hasDay := dateParts(value)
	month = min(max(month, 1), monthsInYear)
	if !hasDay {
		return fmt.Sprintf("%04d-%02d", year, month)
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, min(max(day, 1), daysIn(year, time.Month(month))))
}

// dateParts splits a value of datePattern's shape into numbers, with
// hasDay false for a month (2026-09).
func dateParts(value string) (year, month, day int, hasDay bool) {
	parts := datePattern.FindStringSubmatch(value)
	year, _ = strconv.Atoi(parts[1])
	month, _ = strconv.Atoi(parts[2])
	if parts[3] == "" {
		return year, month, 1, false
	}
	day, _ = strconv.Atoi(parts[3])
	return year, month, day, true
}

// daysIn is the number of days in a month: 28 for February 2026.
func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// monthsMeantAsMinutes returns the minutes reading of a since: or until: value such
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
