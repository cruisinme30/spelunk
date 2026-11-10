package notebook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// OutputHash fingerprints exactly the output blocks Read included in text.
// Each block is framed by its decimal cell number and UTF-8 byte length,
// separated by colons, followed by the bytes including the final LF. An
// empty output list still has a digest, so a first output cannot go unnoticed.
func OutputHash(text []byte, cells []Cell) string {
	digest := sha256.New()
	line, offset := 1, 0
	for _, cell := range cells {
		if cell.Kind != KindOutput {
			continue
		}
		for line < cell.StartLine && offset < len(text) {
			offset = nextLine(text, offset)
			line++
		}
		start := offset
		for range cell.Lines {
			offset = nextLine(text, offset)
			line++
		}
		_, _ = digest.Write([]byte(strconv.Itoa(cell.Number) + ":" + strconv.Itoa(offset-start) + ":"))
		_, _ = digest.Write(text[start:offset])
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// nextLine advances through the line at offset, including its LF when present.
func nextLine(text []byte, offset int) int {
	if end := bytes.IndexByte(text[offset:], '\n'); end >= 0 {
		return offset + end + 1
	}
	return len(text)
}
