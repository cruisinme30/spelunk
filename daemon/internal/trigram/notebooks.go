package trigram

import (
	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/notebook"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/symbols"
)

// searchText is a file's decoded content as the index searches it: a
// Jupyter notebook as its cells' text, in its kernel's language, with the
// cells; any other file as it is, in the language its name says.
func searchText(path string, content []byte, notebookOutputs bool) (text []byte, language string, cells []notebook.Cell) {
	if notebook.IsNotebook(path) {
		if nb, ok := notebook.Read(content, notebookOutputs); ok {
			return nb.Text, nb.Lang, nb.Cells
		}
	}
	return content, lang.Detect(path, content), nil
}

// ReadLimit is how much of the file at path to read, given the index's
// size limit: a notebook is read up to notebook.MaxFileBytes, and the limit
// then applies to its cells' text (see notebook.MaxFileBytes).
func ReadLimit(path string, maxBytes int64) int64 {
	if maxBytes > 0 && notebook.IsNotebook(path) {
		return max(maxBytes, notebook.MaxFileBytes)
	}
	return maxBytes
}

// eachCell calls f with the text of each of a notebook's cells that holds
// code and the line its text starts on.
func eachCell(text []byte, cells []notebook.Cell, f func(cell notebook.Cell, cellText []byte)) {
	starts := lineStarts(text)
	for _, cell := range cells {
		if cell.Kind != notebook.KindCode || cell.StartLine > len(starts) {
			continue
		}
		end := len(text)
		if last := cell.StartLine - 1 + cell.Lines; last < len(starts) {
			end = starts[last]
		}
		f(cell, text[starts[cell.StartLine-1]:end])
	}
}

// symbolsOf finds a file's definitions: a notebook's in each code cell, by
// that cell's language, numbered by the notebook's text.
func symbolsOf(language string, text []byte, cells []notebook.Cell) []symbols.Symbol {
	if cells == nil {
		return symbols.Extract(language, text)
	}
	var found []symbols.Symbol
	eachCell(text, cells, func(cell notebook.Cell, cellText []byte) {
		for _, symbol := range symbols.Extract(cell.Lang, cellText) {
			symbol.Line += cell.StartLine - 1
			found = append(found, symbol)
		}
	})
	return found
}

// outlineOf is a file's outline: a notebook's is each code cell's in turn.
func outlineOf(language string, text []byte, cells []notebook.Cell) []symbols.Entry {
	if cells == nil {
		return symbols.Outline(language, text)
	}
	var entries []symbols.Entry
	eachCell(text, cells, func(cell notebook.Cell, cellText []byte) {
		for _, entry := range symbols.Outline(cell.Lang, cellText) {
			entry.Line += cell.StartLine - 1
			entries = append(entries, entry)
		}
	})
	return entries[:min(len(entries), symbols.MaxOutline)]
}

// cellPlace is where line (1-based) of a notebook's text is, or nil for
// any other file.
func cellPlace(cells []notebook.Cell, line int) *protocol.CellPlace {
	cell, inCell, ok := notebook.CellAt(cells, line)
	if !ok {
		return nil
	}
	return &protocol.CellPlace{Number: cell.Number, Line: inCell, Kind: cell.Kind, Language: cell.Lang}
}

// isOutputLine reports whether line of a notebook's text is a cell's
// output, which Replace never changes.
func isOutputLine(cells []notebook.Cell, line int) bool {
	cell, _, ok := notebook.CellAt(cells, line)
	return ok && cell.Kind == notebook.KindOutput
}

// previewCells is a notebook's cells as a preview lists them.
func previewCells(cells []notebook.Cell) []protocol.NotebookCell {
	if cells == nil {
		return nil
	}
	out := make([]protocol.NotebookCell, len(cells))
	for i, c := range cells {
		out[i] = protocol.NotebookCell{
			Number: c.Number, Kind: c.Kind, Language: c.Lang, StartLine: c.StartLine, LineCount: c.Lines,
			ExecutionCount: c.ExecutionCount, Outputs: c.Outputs,
		}
	}
	return out
}

// trimToCell drops the context lines around a match in a notebook that
// belong to another cell: the lines before line and after last.
func trimToCell(cells []notebook.Cell, line, last int, before, after []string) (inBefore, inAfter []string) {
	cell, inCell, ok := notebook.CellAt(cells, line)
	if !ok {
		return before, after
	}
	before = before[max(0, len(before)-(inCell-1)):]
	after = after[:min(len(after), max(0, cell.StartLine+cell.Lines-1-last))]
	return before, after
}
