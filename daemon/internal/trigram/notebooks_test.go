package trigram

import (
	"context"
	"encoding/base64"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// churnNotebook is a notebook as Jupyter writes it: markdown, then code
// cells, one that printed text and one that drew a plot.
const churnNotebook = `{
 "cells": [
  {"cell_type": "markdown", "metadata": {}, "source": ["# Churn model\n", "\n", "Retries use the shared retry_policy."]},
  {"cell_type": "code", "execution_count": 1, "metadata": {}, "outputs": [],
   "source": ["from payments.retry_policy import RetryPolicy\n", "\n", "retry_policy = RetryPolicy(max_tries=5, backoff=0.5)"]},
  {"cell_type": "code", "execution_count": 2, "metadata": {}, "outputs": [],
   "source": ["def load_events(path, policy=retry_policy):\n", "    \"\"\"Reads payment events.\"\"\"\n", "    return policy.call(path)"]},
  {"cell_type": "code", "execution_count": 3, "metadata": {},
   "outputs": [{"name": "stdout", "output_type": "stream", "text": ["loaded 182340 events with retry_policy\n"]}],
   "source": ["events = load_events(\"s3://acme/events\")\n", "print(f\"loaded {len(events)} events with retry_policy\")"]},
  {"cell_type": "code", "execution_count": 4, "metadata": {},
   "outputs": [{"output_type": "display_data", "metadata": {}, "data": {"image/png": "PLOT", "text/plain": ["<Figure>"]}}],
   "source": ["class ChurnModel:\n", "    def fit(self, events):\n", "        return self"]}
 ],
 "metadata": {"kernelspec": {"language": "python", "name": "python3"}, "language_info": {"name": "python"}},
 "nbformat": 4, "nbformat_minor": 5
}
`

// notebookRepo builds a repo from files on disk, as the index reads them,
// with opts (Symbols always on).
func notebookRepo(t *testing.T, files map[string]string, opts BuildOptions) Repo {
	t.Helper()
	root := testutil.WriteTree(t, files)
	listed, err := ListFiles(context.Background(), root, WalkOptions{MaxFileBytes: opts.MaxFileBytes})
	if err != nil {
		t.Fatal(err)
	}
	opts.Symbols = true
	shard, err := Build(context.Background(), root, listed, opts)
	if err != nil {
		t.Fatal(err)
	}
	return Repo{ID: "ml", Name: "ml-research", Root: root, Shard: shard}
}

// cellsOf summarizes results as "path cell N:line text" ("path:line text"
// outside notebooks).
func cellsOf(items []protocol.ResultItem) []string {
	out := []string{}
	for _, item := range items {
		where := item.Path
		if item.Cell != nil {
			where += " cell " + strconv.Itoa(item.Cell.Number) + ":" + strconv.Itoa(item.Cell.Line) + " " + item.Cell.Kind
		} else {
			where += ":" + strconv.Itoa(item.Line)
		}
		text := item.Text
		if item.Kind == "symbol" {
			text = item.SymbolKind + " " + item.Name
		}
		out = append(out, where+" "+text)
	}
	return out
}

func TestANotebookIsSearchedByItsCellsSource(t *testing.T) {
	// @covers screen:notebook-cells
	repo := notebookRepo(t, map[string]string{"notebooks/churn.ipynb": churnNotebook, "src/features.py": "retry_policy = 1\n"}, BuildOptions{})
	items, _ := runItems(t, "retry_policy", defaultSettings, "", repo)
	want := []string{
		"notebooks/churn.ipynb cell 1:3 markdown Retries use the shared retry_policy.",
		"notebooks/churn.ipynb cell 2:1 code from payments.retry_policy import RetryPolicy",
		"notebooks/churn.ipynb cell 2:3 code retry_policy = RetryPolicy(max_tries=5, backoff=0.5)",
		"notebooks/churn.ipynb cell 3:1 code def load_events(path, policy=retry_policy):",
		`notebooks/churn.ipynb cell 4:2 code print(f"loaded {len(events)} events with retry_policy")`,
		"src/features.py:1 retry_policy = 1",
	}
	if got := cellsOf(items); !slices.Equal(got, want) {
		t.Errorf("results =\n%s\nwant (code, unescaped, by cell; never the printed output)\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestANotebooksCodeCellsHaveDefinitionsInTheKernelsLanguage(t *testing.T) {
	// @covers screen:notebook-cells
	repo := notebookRepo(t, map[string]string{"churn.ipynb": churnNotebook}, BuildOptions{})
	items, _ := runItems(t, "sym:load_events", defaultSettings, "", repo)
	if got := cellsOf(items); !slices.Equal(got, []string{"churn.ipynb cell 3:1 code function load_events"}) {
		t.Errorf("sym:load_events = %v, want the function in cell 3", got)
	}
	items, _ = runItems(t, "kind:class Churn", defaultSettings, "", repo)
	if got := cellsOf(items); !slices.Equal(got, []string{"churn.ipynb cell 5:1 code class ChurnModel"}) {
		t.Errorf("kind:class Churn = %v, want the class in cell 5", got)
	}
	items, _ = runItems(t, "lang:python load_events", defaultSettings, "", repo)
	if len(items) != 2 {
		t.Errorf("lang:python load_events = %v, want both uses: a notebook is in its kernel's language", cellsOf(items))
	}
	// ref: leaves out the cell that defines the name.
	items, _ = runItems(t, "ref:load_events", defaultSettings, "", repo)
	if got := cellsOf(items); !slices.Equal(got, []string{`churn.ipynb cell 4:1 code events = load_events("s3://acme/events")`}) {
		t.Errorf("ref:load_events = %v, want only its use in cell 4", got)
	}
}

func TestTextOutputsAreSearchedOnlyWhenTheSettingSaysSo(t *testing.T) {
	// @covers setting:index.notebookOutputs
	files := map[string]string{"churn.ipynb": churnNotebook}
	if items, _ := runItems(t, "182340", defaultSettings, "", notebookRepo(t, files, BuildOptions{})); len(items) != 0 {
		t.Errorf("printed output found by default: %v", cellsOf(items))
	}
	items, _ := runItems(t, "182340", defaultSettings, "", notebookRepo(t, files, BuildOptions{NotebookOutputs: true}))
	if got := cellsOf(items); !slices.Equal(got, []string{"churn.ipynb cell 4:1 output loaded 182340 events with retry_policy"}) {
		t.Errorf("with outputs = %v, want cell 4's printed line, as an output", got)
	}
	// The source lines number the same either way.
	withOutputs := notebookRepo(t, files, BuildOptions{NotebookOutputs: true})
	items, _ = runItems(t, "max_tries", defaultSettings, "", withOutputs)
	if len(items) != 1 || items[0].Line != 6 {
		t.Errorf("max_tries with outputs = %+v, want line 6 as without", items)
	}
}

func TestPlotsInANotebookAreNeverSearchedNorCountedTowardTheSizeLimit(t *testing.T) {
	plot := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("\x89PNG plot pixels ", 20_000))) // ~450 KB
	big := strings.Replace(churnNotebook, `"PLOT"`, `"`+plot+`"`, 1)
	repo := notebookRepo(t, map[string]string{"big.ipynb": big}, BuildOptions{MaxFileBytes: 64 * 1024})
	if items, _ := runItems(t, "ChurnModel", defaultSettings, "", repo); len(items) != 1 {
		t.Errorf("a notebook past the size limit only by its plot isn't searched: %v", cellsOf(items))
	}
	if items, _ := runItems(t, plot[100:130], defaultSettings, "", repo); len(items) != 0 {
		t.Errorf("a query matched inside a plot's data: %v", cellsOf(items))
	}
	// A notebook whose code itself is past the limit is still left out, as any file is.
	huge := strings.Replace(churnNotebook, `"class ChurnModel:\n"`, `"`+strings.Repeat("x = 1\\n", 20_000)+`"`, 1)
	repo = notebookRepo(t, map[string]string{"huge.ipynb": huge}, BuildOptions{MaxFileBytes: 64 * 1024})
	if len(repo.Shard.Docs) != 0 {
		t.Errorf("a notebook with 120 KB of code was indexed under a 64 KB limit")
	}
}

func TestABrokenNotebookIsSearchedAsText(t *testing.T) {
	conflicted := "<<<<<<< HEAD\n" + churnNotebook + "=======\n{}\n>>>>>>> theirs\n"
	repo := notebookRepo(t, map[string]string{"merge.ipynb": conflicted}, BuildOptions{})
	items, _ := runItems(t, "max_tries", defaultSettings, "", repo)
	if len(items) != 1 || items[0].Cell != nil || !strings.Contains(items[0].Text, `"retry_policy = RetryPolicy(max_tries=5`) {
		t.Errorf("results = %v, want the raw JSON line, as before notebooks were read by cell", cellsOf(items))
	}
}

func TestContextLinesAndMatchesStayInsideACell(t *testing.T) {
	repo := notebookRepo(t, map[string]string{"churn.ipynb": churnNotebook}, BuildOptions{})
	settings := defaultSettings
	settings.ContextLines = 3
	items, _ := runItems(t, "load_events", settings, "", repo)
	if len(items) != 2 {
		t.Fatalf("load_events = %v, want 2", cellsOf(items))
	}
	def, use := items[0], items[1]
	if len(def.ContextBefore) != 0 || len(def.ContextAfter) != 2 {
		t.Errorf("cell 3's match has context %q / %q, want none before (it starts the cell) and its 2 lines after", def.ContextBefore, def.ContextAfter)
	}
	if len(use.ContextBefore) != 0 || !slices.Equal(use.ContextAfter, []string{`print(f"loaded {len(events)} events with retry_policy")`}) {
		t.Errorf("cell 4's match has context %q / %q, want only cell 4's next line", use.ContextBefore, use.ContextAfter)
	}
	// A regex across lines never runs from one cell into the next.
	items, _ = runItems(t, `/return policy.call\(path\)\nevents/`, settings, "", repo)
	if len(items) != 0 {
		t.Errorf("a match ran across cells 3 and 4: %v", cellsOf(items))
	}
	if items, _ = runItems(t, `/def load_events.*\n.*Reads/`, settings, "", repo); len(items) != 1 {
		t.Errorf("a match across lines of one cell = %v, want it", cellsOf(items))
	}
}

func TestPreviewOfANotebookListsItsCellsAndOutline(t *testing.T) {
	repo := notebookRepo(t, map[string]string{"churn.ipynb": churnNotebook}, BuildOptions{})
	ref := Ref{RepoID: repo.ID, Path: "churn.ipynb", Line: 7, Column: 4, Length: 11}
	preview, err := Preview(&repo, ref, mustPlan(t, "load_events", defaultSettings, ""), 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if preview.FirstLine != 5 || preview.Lines[2] != "def load_events(path, policy=retry_policy):" {
		t.Errorf("preview lines from %d = %q, want cell text from line 5", preview.FirstLine, preview.Lines)
	}
	var cells []string
	for _, c := range preview.Cells {
		cells = append(cells, strconv.Itoa(c.Number)+" "+c.Kind+" "+c.Language+" "+strconv.Itoa(c.StartLine)+"+"+strconv.Itoa(c.LineCount)+" ["+strconv.Itoa(c.ExecutionCount)+"] "+strings.Join(c.Outputs, ","))
	}
	want := []string{"1 markdown markdown 1+3 [0] ", "2 code python 4+3 [1] ", "3 code python 7+3 [2] ", "4 code python 10+2 [3] text", "5 code python 12+3 [4] image"}
	if !slices.Equal(cells, want) {
		t.Errorf("cells =\n%q\nwant\n%q", cells, want)
	}
	var names []string
	for _, s := range preview.Symbols {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"load_events", "ChurnModel", "fit"}) {
		t.Errorf("outline = %v, want each code cell's definitions", names)
	}
	target, err := OpenTarget(&repo, ref)
	if err != nil || target.Cell == nil || *target.Cell != (protocol.CellPlace{Number: 3, Line: 1, Kind: "code", Language: "python"}) {
		t.Errorf("OpenTarget = %+v, %v; want cell 3, line 1", target, err)
	}
	// A file that isn't a notebook has neither.
	plain := notebookRepo(t, map[string]string{"a.py": "def load_events():\n    pass\n"}, BuildOptions{})
	preview, _ = Preview(&plain, Ref{RepoID: plain.ID, Path: "a.py", Line: 1}, nil, 2, 0)
	target, _ = OpenTarget(&plain, Ref{RepoID: plain.ID, Path: "a.py", Line: 1})
	if preview.Cells != nil || target.Cell != nil {
		t.Errorf("a .py file previewed with cells %v, opened at cell %v", preview.Cells, target.Cell)
	}
}

func TestPreviewOfAnOutputReadsTheOutputs(t *testing.T) {
	repo := notebookRepo(t, map[string]string{"churn.ipynb": churnNotebook}, BuildOptions{NotebookOutputs: true})
	items, _ := runItems(t, "182340", defaultSettings, "", repo)
	ref, ok := ParseRef(items[0].Ref)
	if !ok {
		t.Fatal("bad ref")
	}
	preview, err := Preview(&repo, ref, nil, 0, 0)
	if err != nil || preview.Lines[0] != "loaded 182340 events with retry_policy" {
		t.Errorf("preview of an output = %q, %v", preview.Lines, err)
	}
}

func TestReplaceNeverChangesWhatACellPrinted(t *testing.T) {
	repo := notebookRepo(t, map[string]string{"churn.ipynb": churnNotebook}, BuildOptions{NotebookOutputs: true})
	plan := mustPlan(t, "182340", defaultSettings, "")
	out, err := Replacements(context.Background(), plan, []Repo{repo}, plan.Terms[0], "1", 100)
	if err != nil || out.Matches != 0 || len(out.Files) != 0 {
		t.Errorf("replacing printed text = %+v, %v; want nothing: running the cell writes its output", out, err)
	}
	plan = mustPlan(t, "retry_policy", defaultSettings, "")
	out, _ = Replacements(context.Background(), plan, []Repo{repo}, plan.Terms[0], "policy", 100)
	var lines []int
	for _, line := range out.Files[0].Lines {
		lines = append(lines, line.Line)
	}
	if !slices.Equal(lines, []int{3, 4, 6, 7, 11}) {
		t.Errorf("replace lines = %v, want the cells' source lines only, as the notebook's text numbers them", lines)
	}
}

func TestANotebookAtARevisionIsReadByCell(t *testing.T) {
	doc, ok := DocFromBlob("churn.ipynb", []byte(churnNotebook), fixedNow, BuildOptions{Symbols: true, MaxFileBytes: 4096})
	if !ok || doc.Lang != "python" || len(doc.Cells) != 5 || !strings.HasPrefix(string(doc.Content), "# Churn model\n") || len(doc.Symbols) != 3 {
		t.Errorf("DocFromBlob = lang %q, %d cells, %d symbols, %q…; want it read by cell", doc.Lang, len(doc.Cells), len(doc.Symbols), doc.Content[:20])
	}
}
