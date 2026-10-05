package trigram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildReadsFilesAsTheyAreNow(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "alpha\n", "grew.txt": "small\n", "now-binary.txt": "text\n"})
	outside := writeTree(t, map[string]string{"secret.txt": "secret\n"})
	// Each listed as a small text file, then changed before Build reads it.
	if err := os.WriteFile(filepath.Join(root, "grew.txt"), []byte(strings.Repeat("x", 4096)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "now-binary.txt"), []byte("bin\x00ary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	files := []File{{Path: "a.txt"}, {Path: "grew.txt"}, {Path: "link.txt"}, {Path: "now-binary.txt"}}
	shard, err := Build(context.Background(), root, files, BuildOptions{MaxFileBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, doc := range shard.Docs {
		got = append(got, doc.Path)
	}
	if want := []string{"a.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Build docs = %q, want %q: a file that grew past the limit, became binary or became a symlink is left out", got, want)
	}
}

func TestReadTextReportsWhyAFileIsNotRead(t *testing.T) {
	root := writeTree(t, map[string]string{"big.txt": strings.Repeat("x", 100), "bin.dat": "\x00"})
	if err := os.Symlink("big.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want error
	}{
		{"big.txt", errTooBig},
		{"bin.dat", errBinary},
		{"link", errNotRegular},
		{".", errNotRegular},
		{"missing", os.ErrNotExist},
	}
	for _, tt := range tests {
		if _, _, err := readText(filepath.Join(root, tt.path), 50); !errors.Is(err, tt.want) {
			t.Errorf("readText(%s) error = %v, want %v", tt.path, err, tt.want)
		}
	}
}
