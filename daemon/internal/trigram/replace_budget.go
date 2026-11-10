package trigram

import (
	"errors"
	"unicode/utf8"

	"github.com/cruisinme30/spelunk/daemon/internal/notebook"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// ErrReplaceBudget means the complete plan cannot fit the replacement
// payload budget. No partial plan may be returned when this happens.
var ErrReplaceBudget = errors.New("replacement plan exceeds the byte budget; narrow the search or shorten the replacement")

// maxReplacementPlanBytes bounds the JSON plan, well below the RPC frame
// limit. Text is charged with JSON escaping; fixed reserves cover field
// names, punctuation, hashes, commas and maximum-width integer values.
const maxReplacementPlanBytes = 16 << 20

type replacementBudget struct{ remaining int }

func newReplacementBudget() *replacementBudget {
	return &replacementBudget{remaining: maxReplacementPlanBytes - 128}
}

func (b *replacementBudget) charge(size int) error {
	if size < 0 || size > b.remaining {
		return ErrReplaceBudget
	}
	b.remaining -= size
	return nil
}

func (b *replacementBudget) text(text string) error {
	count, ok := jsonTextBytes(text, b.remaining)
	if !ok {
		return ErrReplaceBudget
	}
	return b.charge(count)
}

func (b *replacementBudget) file(file *protocol.ReplaceFile, doc *Doc) error {
	if err := b.charge(256); err != nil {
		return err
	}
	for _, text := range []string{file.RepoID, file.Path, file.File} {
		if err := b.text(text); err != nil {
			return err
		}
	}
	for _, cell := range doc.Cells {
		if cell.Kind == notebook.KindOutput {
			continue
		}
		if err := b.charge(128); err != nil {
			return err
		}
		if err := b.text(cell.Kind); err != nil {
			return err
		}
		if err := b.text(cell.Lang); err != nil {
			return err
		}
	}
	return nil
}

func (b *replacementBudget) line(text string) error {
	if err := b.charge(96); err != nil {
		return err
	}
	return b.text(text)
}

// jsonTextBytes counts the bytes encoding/json adds inside a quoted string,
// including HTML and U+2028/U+2029 escaping and invalid UTF-8 replacement.
// It stops at limit without allocating a serialized copy of the string.
func jsonTextBytes(text string, limit int) (int, bool) {
	count := 0
	for i := 0; i < len(text); {
		width, size := jsonRuneBytes(text[i:])
		if size > limit-count {
			return count, false
		}
		count += size
		i += width
	}
	return count, true
}

// jsonRuneBytes gives the first rune's source width and JSON-escaped size.
func jsonRuneBytes(text string) (width, escaped int) {
	switch text[0] {
	case '"', '\\', '\b', '\f', '\n', '\r', '\t':
		return 1, 2
	case '<', '>', '&':
		return 1, 6
	}
	if text[0] < 0x20 {
		return 1, 6
	}
	if text[0] < utf8.RuneSelf {
		return 1, 1
	}
	r, width := utf8.DecodeRuneInString(text)
	if (r == utf8.RuneError && width == 1) || r == '\u2028' || r == '\u2029' {
		return width, 6
	}
	return width, width
}
