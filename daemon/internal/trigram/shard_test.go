package trigram

import (
	"context"
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/lang"
)

// fixedNow stamps test docs, so results don't depend on the clock.
var fixedNow = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)

// shardOf indexes in-memory files, as Build would index them from disk.
func shardOf(files map[string]string) *Shard {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	s := &Shard{BuiltAt: fixedNow, postings: map[uint32][]uint32{}}
	for _, path := range paths {
		content := []byte(files[path])
		s.add(Doc{Path: path, Lang: lang.Detect(path, content), ModTime: fixedNow, Content: content})
	}
	return s
}

func TestCandidates(t *testing.T) {
	shard := shardOf(map[string]string{
		"a.txt": "RetryPolicy here",
		"b.txt": "retrypolicy lower",
		"c.txt": "nothing relevant",
		"d.txt": "café au lait",
	})
	tests := []struct {
		literal       string
		caseSensitive bool
		want          []uint32
	}{
		{"RetryPolicy", false, []uint32{0, 1}},
		{"RetryPolicy", true, []uint32{0, 1}}, // trigrams are folded; the regex checks case
		{"absent", false, []uint32{}},
		{"ab", false, nil},           // shorter than a trigram: any doc
		{"café", false, []uint32{3}}, // ASCII trigrams "caf" narrow it
		{"é au", false, []uint32{3}}, // only " au" is ASCII
		{"éé", false, nil},           // no ASCII trigram: any doc
		{"desk", false, nil},         // k and s also match the KELVIN SIGN and LONG S: any doc
		{"desk", true, []uint32{}},   // matched exactly, so "des" narrows
		{"lait", true, []uint32{3}},
	}
	for _, tt := range tests {
		if got := shard.Candidates(tt.literal, tt.caseSensitive); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Candidates(%q, case %v) = %v, want %v", tt.literal, tt.caseSensitive, got, tt.want)
		}
	}
}

func TestIntersectAndUnion(t *testing.T) {
	a, b := []uint32{1, 3, 5, 7}, []uint32{3, 4, 7, 9}
	if got := intersect(a, b); !reflect.DeepEqual(got, []uint32{3, 7}) {
		t.Errorf("intersect = %v", got)
	}
	if got := union(a, b); !reflect.DeepEqual(got, []uint32{1, 3, 4, 5, 7, 9}) {
		t.Errorf("union = %v", got)
	}
}

func TestBuildReadsListedFiles(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n", "b.py": "print(1)\n"})
	var reports []float64
	shard, err := Build(context.Background(), root, []File{{Path: "a.go"}, {Path: "b.py"}, {Path: "deleted.txt"}}, func(done float64) {
		reports = append(reports, done)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(shard.Docs) != 2 || shard.Docs[0].Lang != "go" || shard.Docs[1].Lang != "python" {
		t.Errorf("docs = %+v, want a.go (go) and b.py (python); deleted files skipped", shard.Docs)
	}
	if len(reports) != 1 || reports[0] != 0 {
		t.Errorf("progress reports = %v, want [0] for a small build", reports)
	}
}

func TestShardSaveAndLoad(t *testing.T) {
	shard := shardOf(map[string]string{"a.txt": "hello world", "b.txt": "goodbye world"})
	path := filepath.Join(t.TempDir(), "shards", "repo.shard")
	if err := shard.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Candidates("world", false), []uint32{0, 1}) || len(loaded.Docs) != 2 {
		t.Errorf("loaded shard lost docs or postings: %+v", loaded.Docs)
	}
	if !loaded.BuiltAt.Equal(shard.BuiltAt) {
		t.Errorf("BuiltAt = %v, want %v", loaded.BuiltAt, shard.BuiltAt)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".shard-*"))
	if len(leftovers) != 0 {
		t.Errorf("Save left temp files: %v", leftovers)
	}
}

func TestLoadRejectsOtherFormatsAndCorruption(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.shard")
	shard := shardOf(map[string]string{"a.txt": "x"})
	if err := shard.Save(old); err != nil {
		t.Fatal(err)
	}
	// Re-save with another version number.
	saved := savedShard{Version: shardFormatVersion + 1}
	f, _ := os.Create(old)
	if err := gobEncode(f, saved); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := Load(old); !errors.Is(err, ErrStaleFormat) {
		t.Errorf("Load(other version) error = %v, want ErrStaleFormat", err)
	}

	corrupt := filepath.Join(dir, "corrupt.shard")
	os.WriteFile(corrupt, []byte("not a shard"), 0o644)
	if _, err := Load(corrupt); err == nil {
		t.Errorf("Load(corrupt) succeeded")
	}
}

func gobEncode(f *os.File, v any) error { return gob.NewEncoder(f).Encode(v) }
