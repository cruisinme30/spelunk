package trigram

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

func replacementTerm(tb testing.TB, text string) (*query.Plan, *query.Content) {
	tb.Helper()
	plan := mustPlan(tb, text, defaultSettings, "")
	var term *query.Content
	query.VisitPositive(plan.Pred, func(c *query.Content) { term = c })
	if term == nil {
		tb.Fatalf("plan %q has no content term", text)
	}
	return plan, term
}

// replaceEdits exercises one line with the same shared budget as a plan.
func replaceEdits(ctx context.Context, term *query.Content, text, replacement string, keep func(int, int) bool, limit int) ([]protocol.ReplaceEdit, error) {
	builder := newReplacementBuilder(ctx, term, replacement)
	return builder.edits(text, keep, limit, func() error { return builder.budget.line(text) })
}

func TestReplaceEditsPreserveCapturesAndUTF16Offsets(t *testing.T) {
	// @covers rpc:replace/plan syntax:regex op:word
	cases := []struct {
		query, text, replacement string
		want                     []protocol.ReplaceEdit
	}{
		{`/(?P<name>retry)(\d)/`, "😀retry1 retry2", "${name}_$2", []protocol.ReplaceEdit{{Start: 2, End: 8, NewText: "retry_1"}, {Start: 9, End: 15, NewText: "retry_2"}}},
		{`/(x retry|retry)/ word:yes`, "ax retry retry", "$1", []protocol.ReplaceEdit{{Start: 3, End: 8, NewText: "retry"}, {Start: 9, End: 14, NewText: "retry"}}},
		{`/(?P<name>retry)/`, "retry", "${name}/$$/${", []protocol.ReplaceEdit{{Start: 0, End: 5, NewText: "retry/$/${"}}},
		{"retry", "retry", "$1", []protocol.ReplaceEdit{{Start: 0, End: 5, NewText: "$1"}}},
	}
	for _, tt := range cases {
		t.Run(tt.query+tt.replacement, func(t *testing.T) {
			_, term := replacementTerm(t, tt.query)
			if got, err := replaceEdits(t.Context(), term, tt.text, tt.replacement, nil, 100); err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("replaceEdits(%q, %q) = %+v, want %+v", tt.text, tt.replacement, got, tt.want)
			}
		})
	}
}

func TestReplacementsStopAfterProvingThePlanTooLarge(t *testing.T) {
	// @covers rpc:replace/plan
	plan, term := replacementTerm(t, "retry")
	repo := repoOf("r", map[string]string{"long.txt": strings.Repeat("retry ", 100_000)})
	got, err := Replacements(t.Context(), plan, []Repo{repo}, term, "next", 10)
	if err != nil || !got.Truncated || len(got.Files) != 0 || got.Matches != 11 {
		t.Fatalf("Replacements(limit=10) = %+v, %v, want no edits and lower-bound count 11", got, err)
	}
}

func BenchmarkReplaceEdits(b *testing.B) {
	_, term := replacementTerm(b, `/(retry)/`)
	for _, matches := range []int{1000, 4000} {
		b.Run(strconv.Itoa(matches), func(b *testing.B) {
			text := strings.Repeat("😀retry ", matches)
			b.ReportAllocs()
			for b.Loop() {
				edits, err := replaceEdits(b.Context(), term, text, "$1", nil, matches)
				if err != nil || len(edits) != matches {
					b.Fatalf("replacement = %d edits, %v; want %d", len(edits), err, matches)
				}
			}
		})
	}
}

func TestReplaceEditsStopWhenCancelledDuringExpansion(t *testing.T) {
	// @covers rpc:replace/plan
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, term := replacementTerm(t, `/(retry)/`)
	visited := 0
	keep := func(_, _ int) bool {
		visited++
		cancel()
		return true
	}
	edits, err := replaceEdits(ctx, term, strings.Repeat("retry ", 1000), "$1", keep, 1000)
	if (err != nil && !errors.Is(err, context.Canceled)) || visited != 1 || len(edits) > 1 || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("cancel during expansion: %d visited, %d edits, %v", visited, len(edits), ctx.Err())
	}
}

func TestReplacementsCountOnlyEditableExcludedNotebookSource(t *testing.T) {
	// @covers rpc:replace/plan op:excludes
	repo := notebookRepo(t, map[string]string{"churn.ipynb": churnNotebook}, BuildOptions{NotebookOutputs: true})
	settings := defaultSettings
	settings.EditorExcludes = []protocol.RootExcludes{{RootID: repo.ID, Patterns: []protocol.ExcludePattern{{Glob: "*.ipynb"}}}}
	plan := mustPlan(t, "182340", settings, "")
	out, err := Replacements(t.Context(), plan, []Repo{repo}, plan.Terms[0], "1", 100)
	if err != nil || out.Matches != 0 || out.Excluded != 0 || len(out.Files) != 0 {
		t.Fatalf("replacing excluded output = %+v, %v; want no editable matches", out, err)
	}
	plan = mustPlan(t, "retry_policy", settings, "")
	out, err = Replacements(t.Context(), plan, []Repo{repo}, plan.Terms[0], "policy", 100)
	if err != nil || out.Matches != 0 || out.Excluded != 5 || len(out.Files) != 0 {
		t.Fatalf("replacing excluded source = %+v, %v; want 5 editable excluded matches", out, err)
	}
}

func TestReplacementCapturesKeepOriginalLineContext(t *testing.T) {
	// @covers rpc:replace/plan syntax:regex
	_, term := replacementTerm(t, `/(a$)|(a)/`)
	got, err := replaceEdits(t.Context(), term, "a b a", "$1/$2", nil, 2)
	want := []protocol.ReplaceEdit{{Start: 0, End: 1, NewText: "/a"}, {Start: 4, End: 5, NewText: "a/"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored capture replacement = %+v, %v; want %+v", got, err, want)
	}
	// A place filter can skip many raw matches before the first edit. The
	// bounded capture prefix grows to that match without losing its context.
	got, err = replaceEdits(t.Context(), term, "a a a b", "$1/$2", func(start, _ int) bool { return start >= 4 }, 1)
	want = []protocol.ReplaceEdit{{Start: 4, End: 5, NewText: "/a"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered anchored capture = %+v, %v; want %+v", got, err, want)
	}
}

func TestReplacementCapturesMaterializeOnlyTheNeededPrefix(t *testing.T) {
	_, term := replacementTerm(t, "/"+strings.Repeat("()", 64)+"(a)/")
	captures := newCaptureExpander(term, strings.Repeat("a", 2000), 1)
	got, err := captures.expand(t.Context(), "$65", []int{0, 1}, newCaptureReferences(term.Re), newReplacementBudget())
	if err != nil || got != "a" || len(captures.groups) != 1 {
		t.Fatalf("one requested capture = %q, %v with %d materialized matches; want a single match", got, err, len(captures.groups))
	}
	if got := len(captures.groups[0]); got != 132 {
		t.Fatalf("capture indexes = %d, want only one match's 132 indexes", got)
	}
}

func TestReplacementsFailWholePlanWhenCapturesExceedBudget(t *testing.T) {
	// @covers rpc:replace/plan
	plan, term := replacementTerm(t, "/"+strings.Repeat("()", 64)+"(a)/")
	repo := repoOf("r", map[string]string{"a.txt": "a", "z.txt": strings.Repeat("a", 2000)})
	got, err := Replacements(t.Context(), plan, []Repo{repo}, term, "$65", 3000)
	if !errors.Is(err, ErrCaptureBudget) || len(got.Files) != 0 || got.Matches != 0 {
		t.Fatalf("capture budget = %+v, %v; want explicit failure and no partial plan", got, err)
	}
}

func TestReplacementsRejectPredicateExhaustionBeforeEdits(t *testing.T) {
	plan, term := replacementTerm(t, `keep -content:/a*/`)
	repo := repoOf("r", map[string]string{"dense.txt": "keep\n" + strings.Repeat("b", query.MaxMatchPositions+10)})
	got, err := Replacements(t.Context(), plan, []Repo{repo}, term, "next", 100)
	if !errors.Is(err, ErrMatchBudget) || len(got.Files) != 0 || got.Matches != 0 {
		t.Fatalf("predicate exhaustion = %+v, %v; want explicit failure and no partial plan", got, err)
	}
}
