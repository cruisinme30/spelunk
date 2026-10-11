package server

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// churnAt is a notebook whose second cell sets max_tries.
func churnAt(maxTries string) string {
	return `{
 "cells": [
  {"cell_type": "markdown", "metadata": {}, "source": ["# Churn model"]},
  {"cell_type": "code", "execution_count": 1, "metadata": {}, "outputs": [],
   "source": ["retry_policy = RetryPolicy(max_tries=` + maxTries + `)"]}
 ],
 "metadata": {"kernelspec": {"language": "python"}},
 "nbformat": 4, "nbformat_minor": 5
}
`
}

// A notebook at a revision is searched and previewed by cell, opens at its
// cell, and revision/text gives its JSON for the notebook editor.
func TestANotebookAtARevisionOpensAsANotebook(t *testing.T) {
	// @covers screen:notebook-cells op:at
	first := gitCommit{author: "Maya", subject: "Add churn", at: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
		files: map[string]string{"churn.ipynb": churnAt("3")}}
	second := gitCommit{author: "Maya", subject: "Retry more", at: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
		files: map[string]string{"churn.ipynb": churnAt("5")}}
	root := gitRepo(t, first, second)
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "ml"})

	_, items := searchAt(t, client, batches, "s1", "at:2026-09-15 max_tries")
	if len(items) != 1 || items[0].Cell == nil || *items[0].Cell != (protocol.CellPlace{Number: 2, Line: 1, Kind: "code", Language: "python"}) ||
		items[0].Text != "retry_policy = RetryPolicy(max_tries=3)" {
		t.Fatalf("at: results = %+v, want cell 2's line as it was", items)
	}
	var preview protocol.Preview
	if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: items[0].Ref, ContextLines: 3}, &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Cells) != 2 || !slices.Equal(preview.Lines, []string{"# Churn model", "retry_policy = RetryPolicy(max_tries=3)", ""}) {
		t.Errorf("preview = %d cells, lines %q; want both cells' text as it was", len(preview.Cells), preview.Lines)
	}
	var target protocol.OpenTarget
	if err := client.call(protocol.MethodOpenResolve, protocol.OpenResolveParams{Ref: items[0].Ref}, &target); err != nil {
		t.Fatal(err)
	}
	if target.Cell == nil || target.Cell.Number != 2 || target.Revision == "" {
		t.Errorf("open target = %+v, want cell 2 at the revision", target)
	}
	var text protocol.RevisionText
	if err := client.call(protocol.MethodRevisionText, protocol.RevisionTextParams{Ref: items[0].Ref}, &text); err != nil {
		t.Fatal(err)
	}
	if text.Text != churnAt("3") {
		t.Errorf("revision/text = %q, want the notebook's JSON as it was", text.Text)
	}
	// Today's notebook is read by cell too.
	_, items = searchAt(t, client, batches, "s2", "max_tries")
	if len(items) != 1 || items[0].Cell == nil || !strings.HasSuffix(items[0].Text, "max_tries=5)") {
		t.Errorf("results now = %+v", items)
	}
}
