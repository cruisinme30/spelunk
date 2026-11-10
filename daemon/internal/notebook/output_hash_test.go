package notebook

import (
	"encoding/json"
	"os"
	"testing"
)

func TestOutputHashMatchesTheEditorGoldenVectors(t *testing.T) {
	// @covers rpc:replace/plan setting:index.notebookOutputs
	body, err := os.ReadFile("../../../testdata/notebook-output-hashes.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string
		Notebook json.RawMessage
		Hash     string
	}
	if err := json.Unmarshal(body, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			nb, ok := Read(tc.Notebook, true)
			if !ok {
				t.Fatal("fixture is not a notebook")
			}
			if got := OutputHash(nb.Text, nb.Cells); got != tc.Hash {
				t.Errorf("OutputHash() = %q, want %q", got, tc.Hash)
			}
		})
	}
}
