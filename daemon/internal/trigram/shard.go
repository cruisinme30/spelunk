package trigram

import (
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/lang"
)

// shardFormatVersion changes whenever the saved layout does; an index
// written by another version is rebuilt rather than migrated.
const shardFormatVersion = 1

// progressEvery is how many files Build reads between progress reports.
const progressEvery = 500

// Doc is one indexed file.
type Doc struct {
	Path    string // slash-separated, relative to the repo root
	Lang    string // canonical language name, "" if unknown
	ModTime time.Time
	Content []byte
}

// Shard is the immutable index of one repo's current files.
type Shard struct {
	Docs    []Doc
	BuiltAt time.Time
	// postings maps a trigram to the ascending ids (indexes into Docs) of
	// the docs that contain it.
	postings map[uint32][]uint32
}

// foldASCII lowercases ASCII letters and leaves every other byte alone.
func foldASCII(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// asciiFoldable reports whether every character that ignore-case matching
// equates with b is ASCII, so folding ASCII letters finds them all.
func asciiFoldable(b byte) bool {
	switch foldASCII(b) {
	case 'k', 's':
		return false
	}
	return b < 0x80
}

func trigramAt(text []byte, i int) uint32 {
	return uint32(foldASCII(text[i]))<<16 | uint32(foldASCII(text[i+1]))<<8 | uint32(foldASCII(text[i+2]))
}

// Build reads files from root and indexes them. progress, if not nil, is
// called now and then with the fraction of files read so far.
func Build(ctx context.Context, root string, files []File, progress func(float64)) (*Shard, error) {
	s := &Shard{BuiltAt: time.Now(), postings: map[uint32][]uint32{}}
	for i, file := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if progress != nil && i%progressEvery == 0 {
			progress(float64(i) / float64(len(files)))
		}
		full := filepath.Join(root, filepath.FromSlash(file.Path))
		content, err := os.ReadFile(full)
		if err != nil {
			continue // deleted since it was listed
		}
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		s.add(Doc{Path: file.Path, Lang: lang.Detect(file.Path, content), ModTime: info.ModTime(), Content: content})
	}
	return s, nil
}

// add appends a doc and records its trigrams.
func (s *Shard) add(doc Doc) {
	id := uint32(len(s.Docs))
	s.Docs = append(s.Docs, doc)
	for i := 0; i+3 <= len(doc.Content); i++ {
		key := trigramAt(doc.Content, i)
		list := s.postings[key]
		if n := len(list); n == 0 || list[n-1] != id { // once per doc
			s.postings[key] = append(list, id)
		}
	}
}

// Candidates returns the ids of docs that may contain literal, or nil when
// every doc may (the literal is too short to narrow anything).
//
// Case-insensitive searches only use trigrams that ASCII folding fully
// covers: none with a non-ASCII byte (RE2's Unicode folding means "é" also
// matches "É"), and none with k or s, which also fold to the KELVIN SIGN
// and LONG S.
func (s *Shard) Candidates(literal string, caseSensitive bool) []uint32 {
	text := []byte(literal)
	var lists [][]uint32
	for i := 0; i+3 <= len(text); i++ {
		if !caseSensitive && !(asciiFoldable(text[i]) && asciiFoldable(text[i+1]) && asciiFoldable(text[i+2])) {
			continue
		}
		list, ok := s.postings[trigramAt(text, i)]
		if !ok {
			return []uint32{} // a required trigram appears nowhere
		}
		lists = append(lists, list)
	}
	if len(lists) == 0 {
		return nil
	}
	sort.Slice(lists, func(i, j int) bool { return len(lists[i]) < len(lists[j]) })
	result := lists[0]
	for _, list := range lists[1:] {
		result = intersect(result, list)
		if len(result) == 0 {
			break
		}
	}
	return result
}

// intersect returns the ids in both ascending lists.
func intersect(a, b []uint32) []uint32 {
	var out []uint32
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return out
}

// union returns the ids in either ascending list.
func union(a, b []uint32) []uint32 {
	out := make([]uint32, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j == len(b) || i < len(a) && a[i] < b[j]:
			out = append(out, a[i])
			i++
		case i == len(a) || b[j] < a[i]:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}

// savedShard is the on-disk form of a Shard.
type savedShard struct {
	Version  int
	BuiltAt  time.Time
	Docs     []Doc
	Postings map[uint32][]uint32
}

// Save writes the shard to path atomically: readers see the old file or
// the new one, never half of one.
func (s *Shard) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".shard-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name()) // no-op after a successful rename
	if err := gob.NewEncoder(temp).Encode(savedShard{Version: shardFormatVersion, BuiltAt: s.BuiltAt, Docs: s.Docs, Postings: s.postings}); err != nil {
		temp.Close()
		return fmt.Errorf("encode shard: %w", err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// ErrStaleFormat means a saved shard was written by another format version.
var ErrStaleFormat = fmt.Errorf("shard format is not version %d", shardFormatVersion)

// Load reads a shard written by Save.
func Load(path string) (*Shard, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var saved savedShard
	if err := gob.NewDecoder(f).Decode(&saved); err != nil {
		return nil, fmt.Errorf("decode shard %s: %w", path, err)
	}
	if saved.Version != shardFormatVersion {
		return nil, ErrStaleFormat
	}
	if saved.Postings == nil {
		saved.Postings = map[uint32][]uint32{}
	}
	return &Shard{Docs: saved.Docs, BuiltAt: saved.BuiltAt, postings: saved.Postings}, nil
}
