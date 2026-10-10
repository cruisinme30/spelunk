package trigram

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// atRevision is payments-api at a past commit (whose tree NewShard holds,
// as the server builds it) beside its current files.
func atRevision(t *testing.T) Repo {
	t.Helper()
	repo := repoOf("payments-api", map[string]string{
		"src/backoff.py": "def parse_retry_after(header):\n    return int(header)\n",
		"src/client.py":  "wait = backoff.parse_retry_after(h)\n",
		"README.md":      "parse_retry_after is gone\n",
	})
	now := repoOf("payments-api", map[string]string{
		"src/client.py": "wait = backoff.parse_retry_after(h, cap=30)\n",
		"README.md":     "parse_retry_after is gone\n",
	})
	repo.At = &AtRevision{SHA: sha40, Current: now.CurrentFiles()}
	return repo
}

// A result at a revision carries the revision in its ref, and says how its
// file differs now: deleted, changed, or not at all.
func TestResultsAtARevisionSayHowTheirFileDiffersNow(t *testing.T) {
	// @covers op:at
	items, _ := runItems(t, "at:v1.0.0 retry_after", defaultSettings, "", atRevision(t))
	var got []string
	for _, item := range items {
		ref, ok := ParseRef(item.Ref)
		if !ok || ref.Revision != sha40 {
			t.Errorf("%s:%d has ref %q, want one at the revision", item.Path, item.Line, item.Ref)
		}
		got = append(got, item.Kind+" "+item.Path+" "+item.SinceRevision)
	}
	want := []string{"line README.md ", "line src/backoff.py deleted", "line src/client.py changed"}
	if !slices.Equal(got, want) {
		t.Errorf("results = %q\nwant %q", got, want)
	}
	items, _ = runItems(t, "at:v1.0.0 type:file backoff", defaultSettings, "", atRevision(t))
	if len(items) != 1 || items[0].SinceRevision != protocol.SinceRevisionDeleted {
		t.Errorf("file-name results = %+v, want backoff.py, deleted", items)
	}
}

func TestDefinitionsAtARevisionSayHowTheirFileDiffersNow(t *testing.T) {
	// @covers op:at
	repo := atRevision(t)
	for i := range repo.Shard.Docs {
		if doc := &repo.Shard.Docs[i]; doc.Path == "src/backoff.py" {
			blob, _ := DocFromBlob(doc.Path, doc.Content, fixedNow, BuildOptions{Symbols: true})
			doc.Symbols = blob.Symbols
		}
	}
	items, _ := runItems(t, "at:v1.0.0 sym:parse_retry", defaultSettings, "", repo)
	if len(items) != 1 || items[0].Kind != query.KindSymbol || items[0].SinceRevision != protocol.SinceRevisionDeleted {
		t.Fatalf("definitions = %+v, want parse_retry_after in a deleted file", items)
	}
	if ref, _ := ParseRef(items[0].Ref); ref.Revision != sha40 {
		t.Errorf("definition ref %q isn't at the revision", items[0].Ref)
	}
}

// The current files' results say nothing about a revision.
func TestCurrentResultsHaveNoRevision(t *testing.T) {
	items, _ := runItems(t, "retry_after", defaultSettings, "", repoOf("r", map[string]string{"a.py": "retry_after\n"}))
	for _, item := range items {
		if ref, _ := ParseRef(item.Ref); ref.Revision != "" || item.SinceRevision != "" {
			t.Errorf("current result %+v has a revision", item)
		}
	}
	// Nor does a repo at a revision without the current files to compare.
	repo := atRevision(t)
	repo.At.Current = nil
	items, _ = runItems(t, "at:v1.0.0 retry_after", defaultSettings, "", repo)
	for _, item := range items {
		if item.SinceRevision != "" {
			t.Errorf("result %s:%d says %s without current files", item.Path, item.Line, item.SinceRevision)
		}
	}
}

func TestCurrentFilesAreTheOverlaysVersion(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.py": "old\n", "b.py": "b\n", "gone.py": "x\n"})
	repo.Overlay = shardOf(map[string]string{"a.py": "new\n"})
	repo.Masked = map[string]bool{"a.py": true, "gone.py": true}
	files := repo.CurrentFiles()
	if len(files) != 2 || string(files["a.py"]) != "new\n" || string(files["b.py"]) != "b\n" {
		t.Errorf("CurrentFiles = %q, want a.py's new text and b.py, without gone.py", files)
	}
}

func TestDocFromBlobReadsTextAsBuildDoes(t *testing.T) {
	bom := string(bomUTF8)
	at := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	doc, ok := DocFromBlob("src/a.py", []byte(bom+"class Retry:\r    pass\n"), at, BuildOptions{Symbols: true})
	if !ok {
		t.Fatal("DocFromBlob refused a text file")
	}
	if string(doc.Content) != "class Retry:\n    pass\n" || doc.Lang != "python" || !doc.ModTime.Equal(at) {
		t.Errorf("doc = %q %s %v, want the BOM dropped, the lone CR a newline, python, and the given time", doc.Content, doc.Lang, doc.ModTime)
	}
	if len(doc.Symbols) != 1 || doc.Symbols[0].Name != "Retry" {
		t.Errorf("symbols = %+v, want Retry", doc.Symbols)
	}
	if doc, _ := DocFromBlob("a.py", []byte("class Retry:\n"), at, BuildOptions{}); doc.Symbols != nil {
		t.Errorf("symbols without BuildOptions.Symbols = %+v", doc.Symbols)
	}
	if _, ok := DocFromBlob("logo.png", []byte("\x89PNG\x00\x00"), at, BuildOptions{}); ok {
		t.Error("DocFromBlob took a binary file")
	}
	if doc, ok := DocFromBlob("empty.txt", nil, at, BuildOptions{}); !ok || len(doc.Content) != 0 {
		t.Errorf("DocFromBlob(empty) = %+v, %v", doc, ok)
	}
}

// A preview of a result at a revision reads the text it's given, with the
// query's terms marked, and opening it goes to the file at that revision.
func TestPreviewAndOpenAtARevision(t *testing.T) {
	// @covers op:at
	repo := atRevision(t)
	plan := mustPlan(t, "at:v1.0.0 retry_after", defaultSettings, "")
	ref := Ref{PlanID: 7, RepoID: repo.ID, Path: "src/backoff.py", Line: 1, Column: 10, Length: 11, Revision: sha40}
	text := []byte("def parse_retry_after(header):\n    return int(header)\n")
	preview, err := PreviewText(&repo, ref, plan, 3, text, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if preview.FocusLine != 1 || len(preview.Lines) != 3 || len(preview.Hits) != 1 || preview.Hits[0].Line != 1 {
		t.Errorf("preview = %+v, want the file's lines with the term marked on line 1", preview)
	}
	target := RevisionTarget(&repo, ref, "v1.0.0")
	if target.Path != "/repos/payments-api/src/backoff.py" || target.Revision != sha40 || target.RevisionLabel != "v1.0.0" ||
		target.Line != 1 || target.Column != 11 || target.Length != 11 {
		t.Errorf("RevisionTarget = %+v", target)
	}
	if _, err := PreviewText(&repo, Ref{RepoID: repo.ID, Path: "a", Line: 9, Revision: sha40}, nil, 3, text, fixedNow); !errors.Is(err, ErrStale) {
		t.Errorf("a line past the end = %v, want ErrStale", err)
	}
}
