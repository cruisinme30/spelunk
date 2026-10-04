package query

import (
	"fmt"
	"strings"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// maxValueCompletions caps value suggestions: f: offers up to five file
// types and five folders.
const maxValueCompletions = 10

// Complete suggests operators or operator values for the word at cursor
// (a UTF-16 offset): "s" offers since: and sym:; "author:ja" offers
// matching authors. Accepting a suggestion applies its
// insert edit. now is used to say how long ago an author last committed.
func Complete(text string, cursor int, resolver Resolver, now time.Time) []protocol.Completion {
	if resolver == nil {
		resolver = noResolver{}
	}
	src := newSource(text)
	for _, t := range lex(src) {
		if cursor < t.start || cursor > t.end || t.start == t.end {
			continue
		}
		switch t.kind {
		case tokenTerm:
			if t.form == formBare && cursor == t.end {
				return operatorCompletions(t)
			}
		case tokenOperator:
			if cursor >= t.valueStart && t.form != formRegex {
				return valueCompletions(t, resolver, now)
			}
		default:
			// Parentheses, minus signs and AND / OR have nothing to complete.
		}
	}
	return []protocol.Completion{}
}

// operatorCompletions offers operators whose names start with the word.
func operatorCompletions(word token) []protocol.Completion {
	prefix := strings.ToLower(word.value)
	completions := []protocol.Completion{}
	for _, op := range operators {
		if strings.HasPrefix(op.name, prefix) {
			completions = append(completions, protocol.Completion{
				Label:  op.name + ":",
				Detail: op.summary,
				Insert: replaceFix("Insert "+op.name+":", protocol.Span{Start: word.start, End: word.end}, op.name+":"),
				Group:  "operator",
			})
		}
	}
	return completions
}

// valueCompletions offers values for the operator at the cursor.
func valueCompletions(t token, resolver Resolver, now time.Time) []protocol.Completion {
	op, known := lookupOperator(t.name)
	if !known {
		return []protocol.Completion{}
	}
	whole := protocol.Span{Start: t.start, End: t.end}
	completions := []protocol.Completion{}
	for _, candidate := range valueCandidates(op, strings.ToLower(t.value), resolver, now) {
		if len(completions) == maxValueCompletions {
			break
		}
		if strings.EqualFold(candidate.value, t.value) {
			continue // already typed in full: nothing to complete
		}
		written := op.name + ":" + quoteIfNeeded(candidate.value)
		completions = append(completions, protocol.Completion{
			Label:   candidate.value,
			Detail:  candidate.detail,
			Context: candidate.context,
			Note:    candidate.note,
			Section: candidate.section,
			Insert:  replaceFix("Insert "+written, whole, written+" "),
			Group:   candidate.group,
		})
	}
	return completions
}

// plural renders a count with its unit: "1 commit", "214 commits".
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// timeAgo renders a duration in its largest whole unit: "3 days ago".
func timeAgo(d time.Duration) string {
	day := 24 * time.Hour
	units := []struct {
		size time.Duration
		name string
	}{{365 * day, "year"}, {30 * day, "month"}, {7 * day, "week"}, {day, "day"}, {time.Hour, "hour"}, {time.Minute, "minute"}}
	for _, u := range units {
		if n := int(d / u.size); n >= 1 {
			return plural(n, u.name) + " ago"
		}
	}
	return "just now"
}

// quoteIfNeeded quotes a value that contains a space or a quote.
func quoteIfNeeded(value string) string {
	if strings.ContainsAny(value, " \t\"()") {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return value
}
