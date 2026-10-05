package trigram

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

const previewFile = "package retry\n\n// RetryPolicy retries with a timeout.\ntype RetryPolicy struct {\n\tTimeout int\n}\n"

func previewRepo(t *testing.T) *Repo {
	t.Helper()
	root := testutil.WriteTree(t, map[string]string{"retry.go": previewFile})
	return &Repo{ID: "r", Name: "r", Root: root}
}

func TestPreviewHighlightsEveryContributingTerm(t *testing.T) {
	// @covers rpc:preview/get
	repo := previewRepo(t)
	plan := mustPlan(t, "RetryPolicy timeout -zebra", defaultSettings, "")
	ref := Ref{PlanID: 1, RepoID: "r", Path: "retry.go", Line: 4, Column: 5, Length: 11}
	got, err := Preview(repo, ref, plan, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstLine != 2 || got.FocusLine != 4 || len(got.Lines) != 5 || got.Lines[1] != "// RetryPolicy retries with a timeout." {
		t.Errorf("preview window = first %d, focus %d, lines %q; want lines 2-6 focused on 4", got.FirstLine, got.FocusLine, got.Lines)
	}
	want := []protocol.LineHits{
		{Line: 3, Ranges: []protocol.Hit{{Start: 3, End: 14, TermIndex: 0}, {Start: 30, End: 37, TermIndex: 1}}},
		{Line: 4, Ranges: []protocol.Hit{{Start: 5, End: 16, TermIndex: 0}}},
		{Line: 5, Ranges: []protocol.Hit{{Start: 1, End: 8, TermIndex: 1}}},
	}
	if !reflect.DeepEqual(got.Hits, want) {
		t.Errorf("hits = %+v\nwant %+v", got.Hits, want)
	}
}

func TestPreviewWithoutThePlanShowsTheResultsOwnMatch(t *testing.T) {
	repo := previewRepo(t)
	ref := Ref{PlanID: 1, RepoID: "r", Path: "retry.go", Line: 4, Column: 5, Length: 11}
	got, err := Preview(repo, ref, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.LineHits{{Line: 4, Ranges: []protocol.Hit{{Start: 5, End: 16}}}}
	if !reflect.DeepEqual(got.Hits, want) {
		t.Errorf("hits = %+v, want %+v", got.Hits, want)
	}
}

func TestPreviewOfAFileNameShowsTheTop(t *testing.T) {
	repo := previewRepo(t)
	got, err := Preview(repo, Ref{RepoID: "r", Path: "retry.go"}, mustPlan(t, "retry", defaultSettings, ""), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstLine != 1 || got.FocusLine != 1 || len(got.Lines) != 3 {
		t.Errorf("preview = first %d, focus %d, %d lines; want the first 3 lines", got.FirstLine, got.FocusLine, len(got.Lines))
	}
}

func TestPreviewOfAFileNameOnlyResultMarksNothingInTheText(t *testing.T) {
	repo := previewRepo(t)
	got, err := Preview(repo, Ref{RepoID: "r", Path: "retry.go"}, mustPlan(t, "type:file retry", defaultSettings, ""), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Hits) != 0 {
		t.Errorf("hits = %+v, want none: type:file matched the name", got.Hits)
	}
}

func TestPreviewAndOpenTargetReportStaleRefs(t *testing.T) {
	// @covers failure:ref-stale
	repo := previewRepo(t)
	if _, err := Preview(repo, Ref{RepoID: "r", Path: "gone.go", Line: 1}, nil, 3); !errors.Is(err, ErrStale) {
		t.Errorf("Preview(deleted file) error = %v, want ErrStale", err)
	}
	if _, err := Preview(repo, Ref{RepoID: "r", Path: "retry.go", Line: 99}, nil, 3); !errors.Is(err, ErrStale) {
		t.Errorf("Preview(line past the end) error = %v, want ErrStale", err)
	}
	if _, err := OpenTarget(repo, Ref{RepoID: "r", Path: "gone.go", Line: 1}); !errors.Is(err, ErrStale) {
		t.Errorf("OpenTarget(deleted file) error = %v, want ErrStale", err)
	}
}

func TestOpenTargetIsTheMatch(t *testing.T) {
	// @covers rpc:open/resolve
	repo := previewRepo(t)
	got, err := OpenTarget(repo, Ref{RepoID: "r", Path: "retry.go", Line: 4, Column: 5, Length: 11})
	if err != nil {
		t.Fatal(err)
	}
	want := protocol.OpenTarget{Path: filepath.Join(repo.Root, "retry.go"), Line: 4, Column: 6, Length: 11}
	if got != want {
		t.Errorf("OpenTarget = %+v, want %+v", got, want)
	}
	got, _ = OpenTarget(repo, Ref{RepoID: "r", Path: "retry.go"})
	if got.Line != 1 || got.Column != 1 {
		t.Errorf("OpenTarget(file name) = %+v, want the top of the file", got)
	}
}

func TestPreviewClipsALongLineAroundItsMatch(t *testing.T) {
	long := strings.Repeat("x", 500_000) + "needle" + strings.Repeat("y", 500_000)
	root := testutil.WriteTree(t, map[string]string{"min.js": "first\n" + long + "\n" + strings.Repeat("z", 10_000) + "\n"})
	repo := &Repo{ID: "r", Name: "r", Root: root}
	plan := mustPlan(t, "needle", defaultSettings, "")
	got, err := Preview(repo, Ref{RepoID: "r", Path: "min.js", Line: 2, Column: 500_000, Length: 6}, plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range got.Lines {
		if n := len([]rune(line)); n > maxPreviewLineRunes+2 {
			t.Errorf("line %d has %d runes, want at most %d and the ellipses", got.FirstLine+i, n, maxPreviewLineRunes)
		}
	}
	shown := []rune(got.Lines[1])
	if len(got.Hits) != 1 || len(got.Hits[0].Ranges) != 1 {
		t.Fatalf("hits = %+v, want needle marked on line 2", got.Hits)
	}
	if r := got.Hits[0].Ranges[0]; string(shown[r.Start:r.End]) != "needle" {
		t.Errorf("hit %+v covers %q, want needle", r, string(shown[r.Start:r.End]))
	}
	// Without the plan, the ref's own match is marked in the clipped line.
	got, err = Preview(repo, Ref{RepoID: "r", Path: "min.js", Line: 2, Column: 500_000, Length: 6}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	shown = []rune(got.Lines[0])
	if r := got.Hits[0].Ranges[0]; len(shown) > maxPreviewLineRunes+2 || string(shown[r.Start:r.End]) != "needle" {
		t.Errorf("preview without the plan = %d runes, hit %+v; want a clipped line with needle marked", len(shown), r)
	}
}
