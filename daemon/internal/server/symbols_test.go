package server

import (
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// symbolWorkspace writes the repos of the symbols mock: RetryPolicy and
// names that start with it, defined in three repos and used in a fourth file.
func symbolWorkspace(t *testing.T) []protocol.Root {
	t.Helper()
	files := map[string]string{
		"payments-api/src/payments/retry_policy.py": "from payments.http import RetryConfig\n\n" +
			"class RetryPolicy:\n    \"\"\"Exponential backoff.\"\"\"\n\n    def __init__(self, max_attempts=3):\n" +
			"        self.max_attempts = max_attempts\n\n    def next_delay(self, attempt):\n        return 2 ** attempt\n",
		"payments-api/src/payments/errors.py": "class RetryPolicyError(Exception):\n    pass\n",
		"payments-api/src/payments/client.py": "from payments.retry_policy import RetryPolicy\n\npolicy = RetryPolicy()\n",
		"shared-libs/http/types.py":           "class RetryPolicyConfig:\n    attempts = 3\n",
		"web-checkout/src/api/types.ts":       "export interface RetryPolicy {\n  maxAttempts: number;\n}\n",
		"web-checkout/src/api/checkout.ts":    "import type { RetryPolicy } from \"./types\";\n",
	}
	root := testutil.WriteTree(t, files)
	var roots []protocol.Root
	for _, name := range []string{"payments-api", "web-checkout", "shared-libs"} {
		roots = append(roots, protocol.Root{ID: name, Path: filepath.Join(root, name), Name: name})
	}
	return roots
}

// search runs text and returns its results and summary.
func search(t *testing.T, client *testClient, text string) ([]protocol.ResultItem, protocol.SearchResult) {
	t.Helper()
	batches := collectBatches(client)
	var result protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: text, Text: text}, &result); err != nil {
		t.Fatal(err)
	}
	return batches.of(text), result
}

func TestSymbolSearchFindsDefinitionsAndOffersTheTextSearch(t *testing.T) {
	// @covers screen:symbol-definitions
	client := newTestClient(t)
	client.mustInitialize(t, symbolWorkspace(t)...)

	items, result := search(t, client, "sym:RetryPolicy")
	var got []string
	for _, item := range items {
		got = append(got, item.SymbolKind+" "+item.Name+" "+item.RepoID+"/"+item.Path)
	}
	want := []string{
		"class RetryPolicyError payments-api/src/payments/errors.py",
		"class RetryPolicy payments-api/src/payments/retry_policy.py",
		"interface RetryPolicy web-checkout/src/api/types.ts",
		"class RetryPolicyConfig shared-libs/http/types.py",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sym:RetryPolicy =\n%q\nwant\n%q", got, want)
	}
	if hits := items[1].Hits; len(hits) != 1 || hits[0].Start != 0 || hits[0].End != len("RetryPolicy") {
		t.Errorf("hits on %s = %+v, want the whole name", items[1].Name, hits)
	}

	var preview protocol.Preview
	if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: items[1].Ref, ContextLines: 3}, &preview); err != nil {
		t.Fatal(err)
	}
	var members []string
	for _, symbol := range preview.Symbols {
		members = append(members, symbol.Name)
	}
	if preview.FocusLine != 3 || !reflect.DeepEqual(members, []string{"__init__", "next_delay"}) {
		t.Errorf("preview = focus %d, members %q; want line 3 with __init__ and next_delay", preview.FocusLine, members)
	}

	if len(result.Hidden) != 1 || result.Hidden[0].Reason != "symbol" || result.Hidden[0].Undo.Title != "Search RetryPolicy as text" {
		t.Fatalf("hidden = %+v, want the text search", result.Hidden)
	}
	_, asText := search(t, client, query.ApplyFix("sym:RetryPolicy", result.Hidden[0].Undo))
	if result.Hidden[0].Count != asText.Total || asText.Total == 0 {
		t.Errorf("text search count = %d, but running it finds %d", result.Hidden[0].Count, asText.Total)
	}
}

func TestEverySuggestionForNoResultsCountsWhatItFinds(t *testing.T) {
	// @covers screen:no-results-while-indexing
	client := newTestClient(t)
	client.mustInitialize(t, symbolWorkspace(t)...)
	const text = "case:yes sym:retrypolicy"

	items, result := search(t, client, text)
	if len(items) != 0 || result.Total != 0 {
		t.Fatalf("%s = %d results, want none", text, result.Total)
	}
	reasons := map[string]int{}
	for _, note := range result.Hidden {
		reasons[note.Reason] = note.Count
		_, ran := search(t, client, query.ApplyFix(text, note.Undo))
		if ran.Total != note.Count {
			t.Errorf("%q says %d %s, but running %q finds %d", note.Undo.Title, note.Count, note.Unit, query.ApplyFix(text, note.Undo), ran.Total)
		}
	}
	if reasons["case"] != 4 {
		t.Errorf("hidden = %+v, want Ignore case offering the 4 definitions", result.Hidden)
	}
}

func TestKindKeepsOneKindOfDefinitionAndCountsTheRest(t *testing.T) {
	// @covers screen:symbol-kinds
	client := newTestClient(t)
	client.mustInitialize(t, symbolWorkspace(t)...)
	const text = "sym:RetryPolicy kind:interface"

	items, result := search(t, client, text)
	if len(items) != 1 || items[0].Name != "RetryPolicy" || items[0].SymbolKind != protocol.SymbolKindInterface {
		t.Fatalf("%s = %+v, want the RetryPolicy interface alone", text, items)
	}
	var note *protocol.HiddenNote
	for i := range result.Hidden {
		if result.Hidden[i].Reason == "kind" {
			note = &result.Hidden[i]
		}
	}
	if note == nil || note.Count != 3 || note.Unit != "definitions" || note.Filter != "kind:interface" {
		t.Fatalf("hidden = %+v, want 3 definitions hidden by kind:interface", result.Hidden)
	}
	if _, all := search(t, client, query.ApplyFix(text, note.Undo)); all.Total != 4 {
		t.Errorf("%q finds %d definitions, want 4", note.Undo.Title, all.Total)
	}
}

func TestRefFindsUsesOfANameButNotItsDefinitions(t *testing.T) {
	// @covers screen:symbol-kinds
	client := newTestClient(t)
	client.mustInitialize(t, symbolWorkspace(t)...)

	items, _ := search(t, client, "ref:RetryPolicy")
	var got []string
	for _, item := range items {
		got = append(got, item.RepoID+"/"+item.Path+":"+strconv.Itoa(item.Line))
	}
	slices.Sort(got)
	// The class and interface lines define it, and RetryPolicyError,
	// RetryPolicyConfig and retry_policy are other names.
	want := []string{
		"payments-api/src/payments/client.py:1",
		"payments-api/src/payments/client.py:3",
		"web-checkout/src/api/checkout.ts:1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ref:RetryPolicy =\n%q\nwant\n%q", got, want)
	}
}
