package trigram

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/notebook"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

func TestReplacementJSONTextAccountingMatchesTheEncoder(t *testing.T) {
	var allBytes []byte
	for i := range 256 {
		allBytes = append(allBytes, byte(i))
	}
	for _, text := range []string{"", "plain", "\"\\\b\f\n\r\t<>&", "😀é\u2028\u2029", string(allBytes)} {
		encoded, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		want := len(encoded) - 2
		got, ok := jsonTextBytes(text, want)
		if !ok || got != want {
			t.Errorf("JSON accounting %q = %d, %v; want %d", text, got, ok, want)
		}
		if want > 0 {
			if _, ok := jsonTextBytes(text, want-1); ok {
				t.Errorf("JSON accounting accepted %d bytes with limit %d", want, want-1)
			}
		}
	}
}

func TestReplacementBudgetIncludesJSONStructureAndMetadata(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	file := protocol.ReplaceFile{
		RepoID: "<r>", Path: "a\".py", File: "/a\\b.py", SourceHash: strings.Repeat("f", 64), OutputHash: strings.Repeat("e", 64),
		Cells: []protocol.ReplaceCell{{Kind: "code", Language: "python", LineCount: maxInt}},
		Lines: []protocol.ReplaceLine{{Line: maxInt, Text: "<😀>\t", Edits: []protocol.ReplaceEdit{{Start: maxInt, End: maxInt, NewText: "\x00&\u2028"}}}},
	}
	budget := newReplacementBudget()
	doc := &Doc{Cells: []notebook.Cell{{Kind: "code", Lang: "python"}}}
	for _, err := range []error{budget.file(&file, doc), budget.line(file.Lines[0].Text), budget.charge(96), budget.text(file.Lines[0].Edits[0].NewText)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(protocol.ReplacePlan{Matches: maxInt, Excluded: maxInt, Files: []protocol.ReplaceFile{file}})
	if err != nil {
		t.Fatal(err)
	}
	charged := maxReplacementPlanBytes - budget.remaining
	if len(encoded) > charged {
		t.Fatalf("serialized plan = %d bytes, accounted %d", len(encoded), charged)
	}
}

func TestReplacementPayloadBudgetSpansFilesAndEscapes(t *testing.T) {
	// @covers rpc:replace/plan
	plan, term := replacementTerm(t, "retry")
	for _, tt := range []struct {
		name, replacement string
		count             int
	}{
		{"across files", strings.Repeat("x", 64<<10), 128},
		{"JSON escaping", strings.Repeat("\x00", 32<<10), 50},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text := strings.Repeat("retry ", tt.count)
			repo := repoOf("r", map[string]string{"a.txt": text, "b.txt": text})
			got, err := Replacements(t.Context(), plan, []Repo{repo}, term, tt.replacement, 10_000)
			if !errors.Is(err, ErrReplaceBudget) || len(got.Files) != 0 || got.Matches != 0 {
				t.Fatalf("payload budget = %+v, %v; want explicit failure with no partial plan", got, err)
			}
		})
	}
}

func TestReplacementBudgetStopsRepeatedCaptureBeforeHugeExpansion(t *testing.T) {
	_, term := replacementTerm(t, `/(a+)/`)
	// The unbounded expansion would exceed 6 GiB; inputs total only 264 KiB.
	got, err := replaceEdits(t.Context(), term, strings.Repeat("a", 64<<10), strings.Repeat("$1", 100_000), nil, 1)
	if !errors.Is(err, ErrReplaceBudget) || len(got) != 0 {
		t.Fatalf("oversized captured expansion = %d edits, %v; want no partial edit", len(got), err)
	}
}

func TestReplacementValidationBoundsTemplatesAndCancellation(t *testing.T) {
	_, term := replacementTerm(t, `/(a)/`)
	if err := ValidateReplacement(t.Context(), term, strings.Repeat("x", maxReplacementPlanBytes+1)); !errors.Is(err, ErrReplaceBudget) {
		t.Fatalf("oversized template = %v, want payload budget error", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ValidateReplacement(ctx, term, "$1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled template validation = %v", err)
	}
}

func TestReplacementPositionsStopAtTheRequestedPrefixOrFailExplicitly(t *testing.T) {
	for _, text := range []string{`/a/`, `/\ba\b/ word:yes`} {
		_, term := replacementTerm(t, text)
		line := []byte(strings.Repeat("a ", maxReplacementPositions+1))
		got, err := replaceEdits(t.Context(), term, string(line), "next", func(_, _ int) bool { return false }, 1)
		if !errors.Is(err, ErrMatchBudget) || len(got) != 0 {
			t.Errorf("bounded positions %q = %d edits, %v; want explicit failure", text, len(got), err)
		}
	}
	_, term := replacementTerm(t, `/a/`)
	got, err := replaceEdits(t.Context(), term, strings.Repeat("a", 100_000), "next", nil, 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("one edit from a dense line = %d, %v; want one bounded prefix", len(got), err)
	}
}
