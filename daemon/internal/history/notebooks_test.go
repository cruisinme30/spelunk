package history

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// notebookAt is a two-cell notebook whose first cell sets max_tries, with
// the execution count and printed output of a run.
func notebookAt(maxTries, run int) string {
	return fmt.Sprintf(`{
 "cells": [
  {"cell_type": "markdown", "metadata": {}, "source": ["# Churn model"]},
  {"cell_type": "code", "execution_count": %d, "metadata": {},
   "outputs": [{"name": "stdout", "output_type": "stream", "text": ["run %d\n"]}],
   "source": ["import pandas as pd\n", "\n", "retry_policy = RetryPolicy(max_tries=%d)"]}
 ],
 "metadata": {"kernelspec": {"language": "python"}},
 "nbformat": 4, "nbformat_minor": 5
}
`, run, run, maxTries)
}

func TestNotebookCommitsDiffByTheirCellsSource(t *testing.T) {
	// @covers screen:notebook-cells
	r := newTestRepo(t)
	r.commit("Maya <maya@acme.dev>", "Add the churn notebook", day(9), map[string]string{"churn.ipynb": notebookAt(3, 1)})
	rerun := r.commit("Maya <maya@acme.dev>", "Run it again", day(5), map[string]string{"churn.ipynb": notebookAt(3, 2)})
	edit := r.commit("Maya <maya@acme.dev>", "Retry five times", day(2), map[string]string{"churn.ipynb": notebookAt(5, 3)})
	repo := Repo{ID: "r1", Name: "ml", Root: r.root, Store: r.ingest()}

	subjects, items := runSearch(t, "max_tries type:added", repo)
	if !slices.Equal(subjects, []string{"Retry five times", "Add the churn notebook"}) {
		t.Errorf("max_tries type:added = %v; want the edit and the commit that added it, not the re-run", subjects)
	}
	if len(items) > 0 && (len(items[0].Files) != 1 || items[0].Files[0].Added != 1 || items[0].Files[0].Removed != 1) {
		t.Errorf("the edit's files = %+v, want churn.ipynb +1 −1: outputs and execution counts aren't changes", items[0].Files)
	}
	if subjects, _ := runSearch(t, `type:added "run 2"`, repo); len(subjects) != 0 {
		t.Errorf("printed output matched in history: %v", subjects)
	}
	if subjects, _ := runSearch(t, "type:added execution_count", repo); len(subjects) != 0 {
		t.Errorf("notebook JSON matched in history: %v", subjects)
	}

	preview, err := Preview(context.Background(), &repo, Ref{RepoID: "r1", SHA: edit}, plan(t, "max_tries"), 3)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, hunk := range preview.Hunks {
		for _, line := range hunk.Lines {
			where := "-"
			if line.Cell != nil {
				where = fmt.Sprintf("cell %d:%d %s", line.Cell.Number, line.Cell.Line, line.Cell.Kind)
			}
			lines = append(lines, line.Kind+" "+where+" "+line.Text)
		}
	}
	want := []string{
		"ctx cell 1:1 markdown # Churn model",
		"ctx cell 2:1 code import pandas as pd",
		"ctx cell 2:2 code ",
		"del cell 2:3 code retry_policy = RetryPolicy(max_tries=3)",
		"add cell 2:3 code retry_policy = RetryPolicy(max_tries=5)",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("preview lines =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if preview, err := Preview(context.Background(), &repo, Ref{RepoID: "r1", SHA: rerun}, nil, 3); err != nil || len(preview.Hunks) != 0 {
		t.Errorf("a re-run's preview has hunks %+v (%v), want none: nothing in its cells changed", preview.Hunks, err)
	}
}

func TestPrintNotebookTextNeverFails(t *testing.T) {
	var out bytes.Buffer
	PrintNotebookText(t.TempDir()+"/missing.ipynb", &out)
	if out.Len() != 0 {
		t.Errorf("a missing file printed %q", out.String())
	}
	path := filepath.Join(t.TempDir(), "broken.ipynb")
	if err := os.WriteFile(path, []byte("<<<<<<< HEAD\n{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	PrintNotebookText(path, &out)
	if out.String() != "<<<<<<< HEAD\n{}" {
		t.Errorf("a broken notebook printed %q, want it as it is", out.String())
	}
}
