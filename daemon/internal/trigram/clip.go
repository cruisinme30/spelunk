package trigram

import (
	"unicode/utf8"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

const (
	// maxResultLineRunes is how much of a line a result shows; longer lines
	// (minified code, for example) are clipped to a window around the first
	// match.
	maxResultLineRunes = 400
	// clipLeadRunes is how many runes of a clipped line are kept before its
	// first match, so the match is shown with some of what leads up to it.
	clipLeadRunes = 80
	// ellipsis marks where a clipped line was cut.
	ellipsis = "…"
)

// clipLine converts byte hits to UTF-16 hits on the text shown, clipping
// long lines (such as minified code) to a window around the first match.
func clipLine(text string, hits []byteHit) (string, []protocol.Hit) {
	return clipWindow(text, hits, maxResultLineRunes)
}

// clipWindow converts byte hits, sorted by start, to UTF-16 hits on the
// text shown: all of text when it has at most maxRunes runes, or else a
// window of maxRunes runes from clipLeadRunes before the first hit (or
// from the start, without hits), with an ellipsis for each cut end.
func clipWindow(text string, hits []byteHit, maxRunes int) (string, []protocol.Hit) {
	start, end := 0, len(text)
	prefix, suffix := "", ""
	if len(text) > maxRunes && utf8.RuneCountInString(text) > maxRunes {
		if len(hits) > 0 {
			start = backRunes(text, hits[0].start, clipLeadRunes)
		}
		end = forwardRunes(text, start, maxRunes)
		if start > 0 {
			prefix = ellipsis
		}
		if end < len(text) {
			suffix = ellipsis
		}
	}
	shown := prefix + text[start:end] + suffix
	shift := utf16Len(prefix)
	var out []protocol.Hit
	for _, h := range hits {
		if h.end <= start || h.start >= end {
			continue // outside the clipped window
		}
		// A match that runs past an edge of the window is marked up to it.
		h.start, h.end = max(h.start, start), min(h.end, end)
		out = append(out, protocol.Hit{
			Start:     shift + utf16Len(text[start:h.start]),
			End:       shift + utf16Len(text[start:h.end]),
			TermIndex: h.termIndex,
		})
	}
	return shown, out
}

// backRunes moves n runes left from byte offset i.
func backRunes(text string, i, n int) int {
	for ; n > 0 && i > 0; n-- {
		_, size := utf8.DecodeLastRuneInString(text[:i])
		i -= size
	}
	return i
}

// forwardRunes moves n runes right from byte offset i.
func forwardRunes(text string, i, n int) int {
	for ; n > 0 && i < len(text); n-- {
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
	}
	return i
}

// utf16Len is the length of text in UTF-16 code units.
func utf16Len(text string) int {
	n := 0
	for _, r := range text {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// UTF16Range converts byte offsets in text to a UTF-16 Range, the offsets
// JavaScript and VS Code count in.
func UTF16Range(text string, start, end int) protocol.Range {
	return protocol.Range{Start: utf16Len(text[:start]), End: utf16Len(text[:end])}
}
