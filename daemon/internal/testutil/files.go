package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// WriteFiles writes files (slash-separated paths to contents) under root,
// making their folders.
func WriteFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// WriteTree writes files under a new temporary folder and returns it.
func WriteTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	WriteFiles(t, root, files)
	return root
}
