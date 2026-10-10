package trigram

import "bytes"

// lineStarts returns the byte offset where each line of content begins. A
// line end at the very end finishes the last line; it doesn't start an
// empty one.
func lineStarts(content []byte) []int {
	starts := []int{0}
	for at := 0; ; {
		i := bytes.IndexByte(content[at:], '\n')
		if i < 0 {
			return starts
		}
		at += i + 1
		if at == len(content) {
			return starts
		}
		starts = append(starts, at)
	}
}

// contextAround returns the lines around line number (1-based) of content
// that a code result shows dimmed: up to n before it, top first, and up to
// n after it, fewer at the ends of the file. starts is lineStarts(content).
// Long lines are clipped from their start, as a result's line would be
// without a match.
func contextAround(content []byte, starts []int, number, n int) (before, after []string) {
	at := number - 1
	if at < 0 || at >= len(starts) {
		return nil, nil
	}
	for i := max(at-n, 0); i < at; i++ {
		before = append(before, lineText(content, starts, i))
	}
	for i := at + 1; i <= min(at+n, len(starts)-1); i++ {
		after = append(after, lineText(content, starts, i))
	}
	return before, after
}

// lineText returns line i (0-based) of content without its line end,
// clipped to what a result shows.
func lineText(content []byte, starts []int, i int) string {
	end := len(content)
	if i+1 < len(starts) {
		end = starts[i+1]
	}
	line := bytes.TrimSuffix(bytes.TrimSuffix(content[starts[i]:end], []byte("\n")), []byte("\r"))
	text, _ := clipWindow(string(line), nil, maxResultLineRunes)
	return text
}
