package trigram

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

func TestReplacementSourceUsesEditorLines(t *testing.T) {
	// @covers rpc:replace/plan
	cases := []struct{ text, hash string }{
		{"", "01ba4719c80b6fe911b091a7c05124b64eeece964e09c058ef8f9805daca546b"},
		{"a", "87428fc522803d31065e7bce3cf03fe475096631e5e07bbd7a0fde60c4cf25c7"},
		{"a\n", "a7da489976d0047490617adb4f7a1f27f7af8b52a5176fd002ffe471863520ab"},
		{"a\r\n", "a7da489976d0047490617adb4f7a1f27f7af8b52a5176fd002ffe471863520ab"},
	}
	for _, tt := range cases {
		hash, cells := replacementSource(&Doc{Content: []byte(tt.text)})
		if hash != tt.hash || cells != nil {
			t.Errorf("replacementSource(%q) = %q, %+v; want %q, nil", tt.text, hash, cells, tt.hash)
		}
	}
}

func TestReplacementOutputEligibilityUsesThePersistedGeneration(t *testing.T) {
	// @covers rpc:replace/plan setting:index.notebookOutputs
	before := `{"cells":[{"cell_type":"code","source":"Retry","outputs":[]}]}`
	after := `{"cells":[{"cell_type":"code","source":"Retry","outputs":[{"output_type":"stream","text":"forbidden\n"}]}]}`
	for _, indexed := range []bool{false, true} {
		doc, ok := DocFromBlob("a.ipynb", []byte(before), fixedNow, BuildOptions{NotebookOutputs: indexed})
		if !ok {
			t.Fatal("notebook was not indexed")
		}
		shard := &Shard{postings: map[uint32][]uint32{}}
		shard.add(doc)
		path := filepath.Join(t.TempDir(), "notebook.shard")
		if err := shard.Save(path); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Docs[0].NotebookOutputs != indexed {
			t.Fatalf("loaded NotebookOutputs = %v, want %v", loaded.Docs[0].NotebookOutputs, indexed)
		}
		plan, term := replacementTerm(t, "Retry -forbidden")
		repo := Repo{ID: "r", Name: "r", Root: t.TempDir(), Shard: loaded}
		out, err := Replacements(t.Context(), plan, []Repo{repo}, term, "Backoff", 10)
		if err != nil || len(out.Files) != 1 {
			t.Fatalf("initial plan = %+v, %v, want one file", out, err)
		}
		if (out.Files[0].OutputHash != "") != indexed {
			t.Errorf("output digest present = %v, want %v", out.Files[0].OutputHash != "", indexed)
		}
		changed, ok := DocFromBlob("a.ipynb", []byte(after), fixedNow, BuildOptions{NotebookOutputs: indexed})
		if !ok {
			t.Fatal("changed notebook was not indexed")
		}
		repo.Shard = &Shard{postings: map[uint32][]uint32{}}
		repo.Shard.add(changed)
		next, err := Replacements(t.Context(), plan, []Repo{repo}, term, "Backoff", 10)
		if err != nil || (len(next.Files) == 0) != indexed {
			t.Errorf("changed-output plan = %+v, %v; output indexing = %v", next, err, indexed)
		}
		if indexed && replacementOutputs(&changed) == out.Files[0].OutputHash {
			t.Error("adding the first output did not change the fingerprint")
		}
	}
}

func TestReplacementSourceExcludesNotebookOutputsAndKeepsCellBoundaries(t *testing.T) {
	// @covers rpc:replace/plan
	body := `{"metadata":{"language_info":{"name":"python"}},"cells":[{"cell_type":"markdown","source":""},{"cell_type":"code","source":["😀retry\r\n","end"],"outputs":[{"output_type":"stream","text":"output\n"}]}]}`
	doc, ok := DocFromBlob("a.ipynb", []byte(body), fixedNow, BuildOptions{NotebookOutputs: true})
	if !ok {
		t.Fatal("notebook was not indexed")
	}
	hash, cells := replacementSource(&doc)
	// The three source lines flatten to this editor view. The notebook's
	// final separator and its output are not editor source lines.
	wantHash, _ := replacementSource(&Doc{Content: []byte("\n😀retry\nend")})
	wantCells := []protocol.ReplaceCell{{Kind: "markdown", Language: "markdown", LineCount: 1}, {Kind: "code", Language: "python", LineCount: 2}}
	if hash != wantHash || !reflect.DeepEqual(cells, wantCells) {
		t.Errorf("notebook source = %q, %+v; want %q, %+v", hash, cells, wantHash, wantCells)
	}
}
