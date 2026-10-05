package trigram

import (
	"context"
	"encoding/gob"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

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
		t.Errorf("intersect = %v, want [3 7]", got)
	}
	if got := intersect([]uint32{1}, []uint32{2}); got == nil || len(got) != 0 {
		t.Errorf("intersect of disjoint lists = %#v, want an empty, non-nil list (nil means any doc)", got)
	}
	if got := union(a, b); !reflect.DeepEqual(got, []uint32{1, 3, 4, 5, 7, 9}) {
		t.Errorf("union = %v, want [1 3 4 5 7 9]", got)
	}
}

func TestBuildReadsListedFiles(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n\nfunc A() {}\n", "b.py": "print(1)\n"})
	var reports []float64
	files := []File{{Path: "a.go"}, {Path: "b.py"}, {Path: "deleted.txt"}}
	shard, err := Build(context.Background(), root, files, BuildOptions{Symbols: true, Progress: func(done float64) {
		reports = append(reports, done)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(shard.Docs) != 2 || shard.Docs[0].Lang != "go" || shard.Docs[1].Lang != "python" {
		t.Errorf("docs = %+v, want a.go (go) and b.py (python); deleted files skipped", shard.Docs)
	}
	if got := shard.Docs[0].Symbols; len(got) != 1 || got[0].Name != "A" || got[0].Line != 3 {
		t.Errorf("a.go symbols = %+v, want func A on line 3", got)
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
		t.Errorf("loaded docs = %+v, want both docs, each a candidate for world", loaded.Docs)
	}
	if !loaded.BuiltAt.Equal(shard.BuiltAt) {
		t.Errorf("BuiltAt = %v, want %v", loaded.BuiltAt, shard.BuiltAt)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".shard-*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files after Save = %v, want none", leftovers)
	}
}

func TestLoadRejectsOtherFormatsAndCorruption(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.shard")
	// A shard file written by another format version.
	saved := savedShard{Version: shardFormatVersion + 1}
	f, err := os.Create(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := gobEncode(f, saved); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(old); !errors.Is(err, ErrStaleFormat) {
		t.Errorf("Load(other version) error = %v, want ErrStaleFormat", err)
	}

	corrupt := filepath.Join(dir, "corrupt.shard")
	if err := os.WriteFile(corrupt, []byte("not a shard"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(corrupt); err == nil {
		t.Errorf("Load(corrupt) error = nil, want an error")
	}
}

func gobEncode(f *os.File, v any) error { return gob.NewEncoder(f).Encode(v) }

func TestLoadRejectsShardsThatDecodeButAreInconsistent(t *testing.T) {
	// @covers failure:index-corrupt
	docs := []Doc{{Path: "a.txt", Content: []byte("abc")}, {Path: "b.txt", Content: []byte("abc")}}
	key := trigramAt([]byte("abc"), 0)
	tests := []struct {
		name     string
		docs     []Doc
		postings map[uint32][]uint32
	}{
		{"posting_past_the_last_doc", docs, map[uint32][]uint32{key: {0, 2}}},
		{"postings_out_of_order", docs, map[uint32][]uint32{key: {1, 0}}},
		{"posting_listed_twice", docs, map[uint32][]uint32{key: {1, 1}}},
		{"path_outside_the_repo", []Doc{{Path: "../../etc/passwd"}}, nil},
		{"absolute_path", []Doc{{Path: "/etc/passwd"}}, nil},
		{"empty_path", []Doc{{Path: ""}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.shard")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := gobEncode(f, savedShard{Version: shardFormatVersion, Docs: tt.docs, Postings: tt.postings}); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			if shard, err := Load(path); err == nil {
				t.Errorf("Load = %d docs, nil error; want it rejected as corrupt", len(shard.Docs))
			}
		})
	}
}

func TestADamagedShardNeverPanicsASearch(t *testing.T) {
	// @covers failure:index-corrupt
	files := map[string]string{}
	for i := range 20 {
		files["src/f"+string(rune('a'+i))+".go"] = "package x\n\nfunc Retry() {}\n// hello world timeout\n"
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "good.shard")
	if err := shardOf(files).Save(path); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trials := 1_000
	if testing.Short() {
		trials = 200
	}
	random := rand.New(rand.NewSource(1))
	damaged := filepath.Join(dir, "damaged.shard")
	for trial := range trials {
		data := slices.Clone(good)
		if trial%3 == 0 {
			data = data[:random.Intn(len(data))] // truncated, as by a crash or a full disk
		} else {
			for range 1 + random.Intn(4) {
				data[random.Intn(len(data))] = byte(random.Intn(256))
			}
		}
		if err := os.WriteFile(damaged, data, 0o600); err != nil {
			t.Fatal(err)
		}
		shard, err := Load(damaged)
		if err != nil {
			continue
		}
		repo := Repo{ID: "r", Name: "r", Root: dir, Shard: shard}
		for _, text := range []string{"retry", "hello world", "sym:Retry", "f:go timeout"} {
			_, _ = Search(context.Background(), mustPlan(t, text, protocol.Settings{DefaultCount: 500, Symbols: true}, ""), []Repo{repo}, 1, func(protocol.ResultItem) {})
		}
	}
}

func TestLoadOfAnEmptyFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.shard")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load(empty file) error = %v, want a decode error (the index is rebuilt)", err)
	}
}

func TestSaveRemovesTempFilesACrashLeftBehind(t *testing.T) {
	dir := t.TempDir()
	stale, fresh := filepath.Join(dir, ".shard-111"), filepath.Join(dir, ".shard-222")
	for _, path := range []string{stale, fresh} {
		if err := os.WriteFile(path, []byte("half a shard"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * staleTempAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := shardOf(map[string]string{"a.txt": "alpha"}).Save(filepath.Join(dir, "repo.shard")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a temp file from an hour-old crashed save is still there (%v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a recent temp file, maybe another daemon's save in progress, was removed: %v", err)
	}
}
