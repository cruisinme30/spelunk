package trigram

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/notebook"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// replacementSource binds the plan to all the source its query evaluated,
// not just the lines being edited. Changing a comment opener, a negated
// term or a notebook boundary can change which untouched lines qualify.
// Each editor line is hashed as UTF-8 followed by LF. Notebook source
// cells already supply their own line counts, so neither the final
// separator's synthetic empty line nor outputs belong to the digest.
func replacementSource(doc *Doc) (string, []protocol.ReplaceCell) {
	var cells []protocol.ReplaceCell
	lineCount := -1
	if doc.Cells != nil {
		lineCount = 0
		cells = []protocol.ReplaceCell{}
		for _, cell := range doc.Cells {
			if cell.Kind == notebook.KindOutput {
				continue
			}
			lineCount += cell.Lines
			cells = append(cells, protocol.ReplaceCell{Kind: cell.Kind, Language: cell.Lang, LineCount: cell.Lines})
		}
	}
	digest := sha256.New()
	for line := range strings.SplitSeq(string(doc.Content), "\n") {
		if lineCount == 0 {
			break
		}
		lineCount--
		_, _ = digest.Write([]byte(strings.TrimSuffix(line, "\r")))
		_, _ = digest.Write([]byte{'\n'})
	}
	return hex.EncodeToString(digest.Sum(nil)), cells
}

// replacementOutputs binds output-dependent query eligibility to the indexed
// generation, even when it had no outputs. Source-only indexing needs no guard.
func replacementOutputs(doc *Doc) string {
	if doc.Cells == nil || !doc.NotebookOutputs {
		return ""
	}
	return notebook.OutputHash(doc.Content, doc.Cells)
}
