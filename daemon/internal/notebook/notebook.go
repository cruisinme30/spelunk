package notebook

import (
	"bytes"
	"encoding/json"
	"path"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/lang"
)

// Extension is a notebook file's extension, as lowercase.
const Extension = ".ipynb"

// MaxFileBytes bounds how much of a notebook file is read. A notebook's
// size is mostly its outputs, plots above all, which are never searched,
// so the index's size limit applies to its cells' text instead; this only
// keeps a runaway file from filling memory.
const MaxFileBytes = 64 << 20

// Cell kinds: a notebook's code, markdown and raw cells, and a code cell's
// text outputs when they are searched.
const (
	KindCode     = "code"
	KindMarkdown = "markdown"
	KindRaw      = "raw"
	KindOutput   = "output"
)

// Output kinds, as a cell's outputs are summarized: printed or returned
// text, an image, or anything else (HTML, a widget, an error).
const (
	OutputText  = "text"
	OutputImage = "image"
	OutputOther = "other"
)

// Cell is one cell's place in a notebook's text.
type Cell struct {
	Number    int    // the cell's number in the notebook, 1-based; an output belongs to its cell's number
	Kind      string // KindCode, KindMarkdown, KindRaw or KindOutput
	Lang      string // the canonical language of its text, "" for raw cells, outputs and unknown languages
	StartLine int    // its first line in the text, 1-based
	Lines     int    // how many lines of the text it is
	// ExecutionCount is the number Jupyter shows beside a code cell that ran, 0 when it hasn't run.
	ExecutionCount int
	Outputs        []string // a code cell's outputs, in order: OutputText, OutputImage or OutputOther
}

// Notebook is a notebook read as text.
type Notebook struct {
	Lang  string // the notebook's language, from its kernel; "" when unknown
	Text  []byte
	Cells []Cell
}

// IsNotebook reports whether the file at filePath (slash-separated) is a
// Jupyter notebook, by its extension.
func IsNotebook(filePath string) bool {
	return strings.EqualFold(path.Ext(filePath), Extension)
}

// raw is the part of the nbformat 4 JSON that Read uses.
type raw struct {
	Cells []struct {
		Type           string          `json:"cell_type"`
		Source         json.RawMessage `json:"source"`
		ExecutionCount *int            `json:"execution_count"`
		Metadata       struct {
			VSCode struct {
				LanguageID string `json:"languageId"`
			} `json:"vscode"`
		} `json:"metadata"`
		Outputs []struct {
			Type string                     `json:"output_type"`
			Text json.RawMessage            `json:"text"`
			Data map[string]json.RawMessage `json:"data"`
		} `json:"outputs"`
	} `json:"cells"`
	Metadata struct {
		Kernel struct {
			Language string `json:"language"`
		} `json:"kernelspec"`
		LanguageInfo struct {
			Name string `json:"name"`
		} `json:"language_info"`
	} `json:"metadata"`
}

// Read reads content, a notebook file's text, as its cells' text, with each
// code cell's text outputs after them when withOutputs is set. ok is false
// for content that isn't an nbformat 4 notebook (it doesn't parse, or has
// no cells list, as an nbformat 3 notebook doesn't): the caller then treats
// the file as plain text, as it would any other.
func Read(content []byte, withOutputs bool) (nb Notebook, ok bool) {
	var r raw
	if err := json.Unmarshal(content, &r); err != nil || r.Cells == nil {
		return Notebook{}, false
	}
	nb.Lang = kernelLang(r.Metadata.LanguageInfo.Name, r.Metadata.Kernel.Language)
	var text bytes.Buffer
	line := 1
	add := func(cell Cell, body string) {
		cell.StartLine = line
		cell.Lines = strings.Count(body, "\n") + 1
		text.WriteString(body)
		text.WriteByte('\n')
		line += cell.Lines
		nb.Cells = append(nb.Cells, cell)
	}
	var outputs []Cell
	var outputText []string
	for i, c := range r.Cells {
		cell := Cell{Number: i + 1, Kind: c.Type}
		switch c.Type {
		case KindMarkdown:
			cell.Lang = "markdown"
		case KindCode:
			cell.Lang = nb.Lang
			if id, known := lang.Resolve(c.Metadata.VSCode.LanguageID); known {
				cell.Lang = id // a cell VS Code switched to another language, such as SQL
			}
		default:
			cell.Kind = KindRaw
		}
		if c.ExecutionCount != nil {
			cell.ExecutionCount = *c.ExecutionCount
		}
		var printed []string
		for _, o := range c.Outputs {
			kind, words := summarize(o.Type, o.Text, o.Data)
			cell.Outputs = append(cell.Outputs, kind)
			if words != "" {
				printed = append(printed, words)
			}
		}
		add(cell, lineEnds(joined(c.Source)))
		if withOutputs && cell.Kind == KindCode && len(printed) > 0 {
			outputs = append(outputs, Cell{Number: cell.Number, Kind: KindOutput})
			outputText = append(outputText, lineEnds(strings.TrimSuffix(strings.Join(printed, ""), "\n")))
		}
	}
	for i, output := range outputs {
		add(output, outputText[i])
	}
	nb.Text = text.Bytes()
	return nb, true
}

// summarize says what kind of output one is, and the text it printed or
// returned when it is text. An image wins over its text/plain caption.
func summarize(outputType string, streamText json.RawMessage, data map[string]json.RawMessage) (kind, text string) {
	if outputType == "stream" {
		return OutputText, joined(streamText)
	}
	for mime := range data {
		if strings.HasPrefix(mime, "image/") {
			return OutputImage, ""
		}
	}
	if plain, found := data["text/plain"]; found {
		return OutputText, joined(plain) // a returned value, or a table's plain-text form beside its HTML
	}
	return OutputOther, ""
}

// joined is a multiline string as nbformat writes it: one string, or a
// list of lines that each keep their line end.
func joined(value json.RawMessage) string {
	var whole string
	if json.Unmarshal(value, &whole) == nil {
		return whole
	}
	var lines []string
	if json.Unmarshal(value, &lines) == nil {
		return strings.Join(lines, "")
	}
	return ""
}

// lineEnds makes a cell's text end its lines as the editor does: "\r\n"
// and a lone "\r" both end a line, so both become "\n".
func lineEnds(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// kernelLang is the canonical name of a notebook's language, from the
// language_info Jupyter records once it ran, else the kernelspec's.
func kernelLang(names ...string) string {
	for _, name := range names {
		if canonical, known := lang.Resolve(name); known {
			return canonical
		}
	}
	return ""
}

// CellAt returns the cell that line (1-based) of a notebook's text is in,
// and the line's number in that cell, 1-based; ok is false past its end.
func CellAt(cells []Cell, line int) (cell Cell, lineInCell int, ok bool) {
	for _, c := range cells {
		if line >= c.StartLine && line < c.StartLine+c.Lines {
			return c, line - c.StartLine + 1, true
		}
	}
	return Cell{}, 0, false
}

// SameCell reports whether lines first and last (1-based) of a notebook's
// text are in one cell: a match that runs from one cell into the next isn't
// one people could have written.
func SameCell(cells []Cell, first, last int) bool {
	a, _, okA := CellAt(cells, first)
	b, _, okB := CellAt(cells, last)
	return okA && okB && a.StartLine == b.StartLine
}

// DiffText is a notebook as history diffs it: its cells' source, never its
// outputs or execution counts, which change each time it runs. Content that
// isn't a notebook comes back as it is.
func DiffText(content []byte) []byte {
	nb, ok := Read(content, false)
	if !ok {
		return content
	}
	return nb.Text
}
