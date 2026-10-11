package notebook

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// churn is a small nbformat 4 notebook: a markdown cell, a code cell whose
// source is one string with a CRLF, a code cell that printed text, one that
// drew a plot, a SQL cell, and a raw cell.
const churn = `{
 "cells": [
  {"cell_type": "markdown", "metadata": {}, "source": ["# Churn model\n", "\n", "Uses the shared retry_policy."]},
  {"cell_type": "code", "execution_count": 1, "metadata": {}, "outputs": [], "source": "import pandas as pd\r\nretry_policy = RetryPolicy(max_tries=5)"},
  {"cell_type": "code", "execution_count": 2, "metadata": {}, "source": ["print(\"loaded\")"],
   "outputs": [{"name": "stdout", "output_type": "stream", "text": ["loaded 182340 events\n"]}]},
  {"cell_type": "code", "execution_count": null, "metadata": {}, "source": ["plot()"],
   "outputs": [{"output_type": "display_data", "metadata": {}, "data": {"image/png": "iVBORw0KGgo=", "text/plain": ["<Figure>"]}},
               {"output_type": "execute_result", "execution_count": 4, "metadata": {}, "data": {"text/html": ["<table/>"], "text/plain": ["   a\n0  1"]}},
               {"output_type": "error", "traceback": []}]},
  {"cell_type": "code", "execution_count": 5, "metadata": {"vscode": {"languageId": "sql"}}, "outputs": [], "source": ["select 1"]},
  {"cell_type": "raw", "metadata": {}, "source": []}
 ],
 "metadata": {"kernelspec": {"display_name": "Python 3", "language": "python", "name": "python3"}, "language_info": {"name": "python"}},
 "nbformat": 4, "nbformat_minor": 5
}`

func TestReadMakesEachCellsSourceItsLines(t *testing.T) {
	nb, ok := Read([]byte(churn), false)
	if !ok {
		t.Fatal("Read: ok = false for an nbformat 4 notebook")
	}
	want := "# Churn model\n\nUses the shared retry_policy.\n" +
		"import pandas as pd\nretry_policy = RetryPolicy(max_tries=5)\n" +
		"print(\"loaded\")\n" +
		"plot()\n" +
		"select 1\n" +
		"\n"
	if string(nb.Text) != want {
		t.Errorf("Text =\n%q\nwant\n%q", nb.Text, want)
	}
	if nb.Lang != "python" {
		t.Errorf("Lang = %q, want python (the kernel's)", nb.Lang)
	}
	type place struct {
		number             int
		kind, lang         string
		start, lines, exec int
		outputs            string
	}
	var got []place
	for _, c := range nb.Cells {
		got = append(got, place{c.Number, c.Kind, c.Lang, c.StartLine, c.Lines, c.ExecutionCount, strings.Join(c.Outputs, ",")})
	}
	wantCells := []place{
		{1, KindMarkdown, "markdown", 1, 3, 0, ""},
		{2, KindCode, "python", 4, 2, 1, ""},
		{3, KindCode, "python", 6, 1, 2, "text"},
		{4, KindCode, "python", 7, 1, 0, "image,text,other"},
		{5, KindCode, "sql", 8, 1, 5, ""},
		{6, KindRaw, "", 9, 1, 0, ""},
	}
	if !slices.Equal(got, wantCells) {
		t.Errorf("Cells =\n%v\nwant\n%v", got, wantCells)
	}
}

func TestReadPutsTextOutputsAfterEveryCell(t *testing.T) {
	plain, _ := Read([]byte(churn), false)
	nb, _ := Read([]byte(churn), true)
	if !strings.HasPrefix(string(nb.Text), string(plain.Text)) {
		t.Fatal("with outputs, the cells' source isn't first and unchanged: its lines would number differently")
	}
	outputs := strings.TrimPrefix(string(nb.Text), string(plain.Text))
	// The plot is an image, never read; its table's text/plain is; the error isn't text.
	if want := "loaded 182340 events\n   a\n0  1\n"; outputs != want {
		t.Errorf("outputs = %q, want %q", outputs, want)
	}
	last := nb.Cells[len(nb.Cells)-2:]
	if last[0].Kind != KindOutput || last[0].Number != 3 || last[0].StartLine != 10 || last[0].Lines != 1 {
		t.Errorf("first output = %+v, want cell 3's, line 10, 1 line", last[0])
	}
	if last[1].Kind != KindOutput || last[1].Number != 4 || last[1].StartLine != 11 || last[1].Lines != 2 {
		t.Errorf("second output = %+v, want cell 4's, line 11, 2 lines", last[1])
	}
}

func TestReadRefusesWhatIsntAnNbformat4Notebook(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":             "<<<<<<< HEAD\n{\"cells\": []}",
		"nbformat 3":           `{"worksheets": [{"cells": []}], "nbformat": 3}`,
		"a JSON list":          `[1, 2]`,
		"cells of wrong shape": `{"cells": "none"}`,
	} {
		if _, ok := Read([]byte(content), false); ok {
			t.Errorf("Read(%s): ok = true, want false so the file is searched as text", name)
		}
	}
	if nb, ok := Read([]byte(`{"cells": [], "metadata": {}}`), false); !ok || len(nb.Text) != 0 || nb.Lang != "" {
		t.Errorf("an empty notebook = %+v, %v; want no text, no language, ok", nb, ok)
	}
}

func TestKernelLanguageFallsBackToTheKernelspec(t *testing.T) {
	nb, _ := Read([]byte(`{"cells": [{"cell_type": "code", "source": "x <- 1", "outputs": []}],
		"metadata": {"kernelspec": {"language": "R"}}}`), false)
	if nb.Lang != "r" || nb.Cells[0].Lang != "r" {
		t.Errorf("Lang = %q, cell = %q; want r from the kernelspec", nb.Lang, nb.Cells[0].Lang)
	}
	nb, _ = Read([]byte(`{"cells": [{"cell_type": "code", "source": "x", "outputs": []}], "metadata": {"kernelspec": {"language": "cobol"}}}`), false)
	if nb.Lang != "" {
		t.Errorf("Lang = %q for an unknown kernel, want empty", nb.Lang)
	}
}

func TestCellAtAndSameCell(t *testing.T) {
	nb, _ := Read([]byte(churn), false)
	for _, c := range []struct{ line, cell, inCell int }{{1, 1, 1}, {3, 1, 3}, {4, 2, 1}, {5, 2, 2}, {9, 6, 1}} {
		cell, inCell, ok := CellAt(nb.Cells, c.line)
		if !ok || cell.Number != c.cell || inCell != c.inCell {
			t.Errorf("CellAt(%d) = cell %d line %d, %v; want cell %d line %d", c.line, cell.Number, inCell, ok, c.cell, c.inCell)
		}
	}
	if _, _, ok := CellAt(nb.Cells, 10); ok {
		t.Error("CellAt past the end: ok = true")
	}
	if !SameCell(nb.Cells, 4, 5) || SameCell(nb.Cells, 3, 4) {
		t.Error("SameCell: lines 4–5 are cell 2, lines 3–4 run from cell 1 into cell 2")
	}
}

func TestDiffTextLeavesOutOutputsAndCounts(t *testing.T) {
	before := strings.Replace(churn, `"execution_count": 2`, `"execution_count": 7`, 1)
	before = strings.Replace(before, "loaded 182340", "loaded 99", 1)
	if !bytes.Equal(DiffText([]byte(before)), DiffText([]byte(churn))) {
		t.Error("re-running a cell changed its diff text; only source belongs in it")
	}
	if got := string(DiffText([]byte("not a notebook"))); got != "not a notebook" {
		t.Errorf("DiffText of other text = %q, want it unchanged", got)
	}
}

func TestIsNotebook(t *testing.T) {
	for p, want := range map[string]bool{"a/b.ipynb": true, "B.IPYNB": true, "a.ipynb.bak": false, "ipynb": false, "a.py": false} {
		if IsNotebook(p) != want {
			t.Errorf("IsNotebook(%q) = %v, want %v", p, !want, want)
		}
	}
}
