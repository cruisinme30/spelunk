package query

import (
	"fmt"
	"slices"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// Diagnostic codes, one per kind of problem a query can have.
// scripts/specCoverage.mjs reads these constants: every code needs a test.
const (
	DiagUnclosedParen   = "unclosed_paren"
	DiagUnmatchedParen  = "unmatched_paren"
	DiagEmptyGroup      = "empty_group"
	DiagMissingOperand  = "missing_operand"
	DiagUnclosedQuote   = "unclosed_quote"
	DiagUnclosedRegex   = "unclosed_regex"
	DiagUnknownOperator = "unknown_operator"
	DiagBadValue        = "bad_value"
	DiagGlobalMisplaced = "global_misplaced"
	DiagDuplicateGlobal = "duplicate_global"
	DiagMixedModeOr     = "mixed_mode_or"
	DiagOpWrongMode     = "op_wrong_mode"
	DiagInvalidRegex    = "invalid_regex"
	DiagNoPositiveTerm  = "no_positive_term"
	DiagQueryTooLong    = "query_too_long"
	DiagHistoryFullScan = "history_full_scan"
	DiagPipeInWord      = "pipe_in_word"
)

const (
	// maxQueryLength is in UTF-16 units.
	maxQueryLength = 1000
	// maxDiagnostics caps the problems one query reports. A pasted megabyte
	// of ")" has a million, and nobody reads past the first few anyway.
	maxDiagnostics = 100
	// maxEditDistanceToFix is how far a typo may be from a name for us to offer that name.
	maxEditDistanceToFix = 2
)

// diagnostics collects problems in the order they are found.
type diagnostics struct {
	list []protocol.Diagnostic
}

func (d *diagnostics) add(severity protocol.Severity, code string, span protocol.Span, message string, fixes ...protocol.Fix) {
	if len(d.list) >= maxDiagnostics {
		return
	}
	if fixes == nil {
		fixes = []protocol.Fix{}
	}
	d.list = append(d.list, protocol.Diagnostic{Severity: severity, Code: code, Message: message, Span: span, Fixes: fixes})
}

func (d *diagnostics) errorf(code string, span protocol.Span, fixes []protocol.Fix, format string, args ...any) {
	if len(d.list) >= maxDiagnostics {
		return // skip formatting the message too
	}
	d.add(protocol.SeverityError, code, span, fmt.Sprintf(format, args...), fixes...)
}

// replaceFix replaces span with text.
func replaceFix(title string, span protocol.Span, text string) protocol.Fix {
	return protocol.Fix{Title: title, Edits: []protocol.TextEdit{{Span: span, NewText: text}}}
}

// insertFix inserts text at offset.
func insertFix(title string, offset int, text string) protocol.Fix {
	return replaceFix(title, protocol.Span{Start: offset, End: offset}, text)
}

// removeSpan says how to delete span: the span to replace, widened over
// one neighbouring space so no double space is left behind, and what to
// put there instead. It takes the space only where that doesn't join the
// span's other neighbour to the next word: the ) of "timeout -) x" is
// removed alone, as taking a space too would make "timeout -x". A span
// between two words, like the () of "a()b", becomes one space.
func removeSpan(src *source, span protocol.Span) (protocol.Span, string) {
	text := src.runes
	start, end := src.runeIndex(span.Start), src.runeIndex(span.End)
	openBefore := start == 0 || text[start-1] == ' ' || text[start-1] == '('
	openAfter := end == len(text) || text[end] == ' ' || text[end] == ')'
	filler := ""
	switch {
	case end < len(text) && text[end] == ' ' && openBefore:
		end++
	case start > 0 && text[start-1] == ' ' && openAfter:
		start--
	case !openBefore && !openAfter:
		filler = " "
	}
	return protocol.Span{Start: src.offsets[start], End: src.offsets[end]}, filler
}

// removeFix deletes span (and one neighbouring space).
func removeFix(title string, src *source, span protocol.Span) protocol.Fix {
	removed, filler := removeSpan(src, span)
	return replaceFix(title, removed, filler)
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	previous := make([]int, len(br)+1)
	current := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		current[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(br)]
}

// nearest returns the candidates within maxEditDistanceToFix of word,
// closest first (ties keep candidate order).
func nearest(word string, candidates []string) []string {
	type scored struct {
		candidate string
		distance  int
	}
	var matches []scored
	for _, c := range candidates {
		if d := editDistance(word, c); d <= maxEditDistanceToFix {
			matches = append(matches, scored{c, d})
		}
	}
	slices.SortStableFunc(matches, func(a, b scored) int { return a.distance - b.distance })
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m.candidate
	}
	return names
}
