package trigram

import (
	"context"
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/symbols"
)

// shardFormatVersion changes whenever the saved layout does; an index
// written by another version is rebuilt rather than migrated.
const shardFormatVersion = 2

// maxDocs bounds a shard's files, so doc ids fit in a uint32.
const maxDocs int64 = math.MaxUint32

// progressEvery is how many files Build reads between progress reports.
const progressEvery = 500

// Doc is one indexed file.
type Doc struct {
	Path    string // slash-separated, relative to the repo root
	Lang    string // canonical language name, "" if unknown
	ModTime time.Time
	Content []byte
	// Symbols are the file's definitions, when the index keeps them
	// (index.symbols), in line order.
	Symbols []symbols.Symbol
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

// allASCIIFoldable reports whether ASCII folding covers every byte.
func allASCIIFoldable(chars []byte) bool {
	for _, b := range chars {
		if !asciiFoldable(b) {
			return false
		}
	}
	return true
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

// BuildOptions are what Build does besides indexing each file's text.
type BuildOptions struct {
	// Symbols finds each file's definitions, for sym:.
	Symbols bool
	// Progress, if not nil, is called now and then with the fraction of
	// files read so far.
	Progress func(float64)
	// MaxFileBytes, when positive, leaves out a file that grew past it
	// since it was listed (see WalkOptions.MaxFileBytes).
	MaxFileBytes int64
}

// Build reads files from root and indexes them. A file that changed since
// it was listed is read as it is now; one that is gone, or is now a
// symlink, a FIFO or another special file, binary or too big, is left out.
func Build(ctx context.Context, root string, files []File, opts BuildOptions) (*Shard, error) {
	if int64(len(files)) >= maxDocs {
		return nil, fmt.Errorf("%d files is more than one index can hold (%d)", len(files), maxDocs)
	}
	s := &Shard{BuiltAt: time.Now(), postings: map[uint32][]uint32{}}
	for i, file := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if opts.Progress != nil && i%progressEvery == 0 {
			opts.Progress(float64(i) / float64(len(files)))
		}
		content, info, err := readText(filepath.Join(root, filepath.FromSlash(file.Path)), opts.MaxFileBytes)
		if err != nil {
			continue // deleted or replaced since it was listed
		}
		doc := Doc{Path: file.Path, Lang: lang.Detect(file.Path, content), ModTime: info.ModTime(), Content: content}
		if opts.Symbols {
			doc.Symbols = symbols.Extract(doc.Lang, content)
		}
		s.add(doc)
	}
	return s, nil
}

// NewShard indexes docs that are already read, keeping their order. The
// indexer builds its overlay of re-read files with it.
func NewShard(docs []Doc) *Shard {
	s := &Shard{BuiltAt: time.Now(), postings: map[uint32][]uint32{}}
	for _, doc := range docs {
		s.add(doc)
	}
	return s
}

// allDocIDs returns the id of every doc, in path order.
func (s *Shard) allDocIDs() []uint32 {
	ids := make([]uint32, len(s.Docs))
	for i := range ids {
		ids[i] = uint32(i) //nolint:gosec // G115: Build keeps a shard under maxDocs docs
	}
	return ids
}

// add appends a doc and records its trigrams.
func (s *Shard) add(doc Doc) {
	id := uint32(len(s.Docs)) //nolint:gosec // G115: Build keeps a shard under maxDocs docs
	s.Docs = append(s.Docs, doc)
	addTrigrams(s.postings, id, doc.Content)
}

// addTrigrams records that text id contains each of text's trigrams. Ids
// must arrive in ascending order, so every posting list stays sorted.
func addTrigrams(postings map[uint32][]uint32, id uint32, text []byte) {
	for i := 0; i+3 <= len(text); i++ {
		key := trigramAt(text, i)
		list := postings[key]
		if n := len(list); n == 0 || list[n-1] != id { // once per text
			postings[key] = append(list, id)
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
	return candidates(s.postings, literal, caseSensitive)
}

// candidates is Candidates over any posting lists.
func candidates(postings map[uint32][]uint32, literal string, caseSensitive bool) []uint32 {
	text := []byte(literal)
	var lists [][]uint32
	for i := 0; i+3 <= len(text); i++ {
		if !caseSensitive && !allASCIIFoldable(text[i:i+3]) {
			continue
		}
		list, ok := postings[trigramAt(text, i)]
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

// intersect returns the ids in both ascending lists. The result is never
// nil: nil means "any doc" to callers, and no common id means "no doc".
func intersect(a, b []uint32) []uint32 {
	out := []uint32{}
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

// Union returns the ids in either ascending list.
func Union(a, b []uint32) []uint32 {
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

// tempPattern names the temporary file Save writes before renaming it.
const tempPattern = ".shard-*"

// staleTempAge is how old a temporary file must be before Save treats it as
// left behind by a crash and removes it. Another daemon (one runs per VS
// Code window) may be writing a newer one into the same folder.
const staleTempAge = time.Hour

// Save writes the shard to path atomically: readers see the old file or
// the new one, never half of one, even after a crash. It also removes the
// temporary files of saves a crash interrupted.
func (s *Shard) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	RemoveStaleTemps(dir, tempPattern, time.Now().Add(-staleTempAge))
	temp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return err
	}
	// Removing fails harmlessly after a successful rename: the name is gone.
	defer func() { _ = os.Remove(temp.Name()) }()
	saved := savedShard{Version: shardFormatVersion, BuiltAt: s.BuiltAt, Docs: s.Docs, Postings: s.postings}
	if err := gob.NewEncoder(temp).Encode(saved); err != nil {
		_ = temp.Close() // the encode error is the one to report
		return fmt.Errorf("encode shard: %w", err)
	}
	// Flushed before the rename, so a crash can't leave a renamed but empty file.
	if err := temp.Sync(); err != nil {
		_ = temp.Close() // the sync error is the one to report
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// RemoveStaleTemps removes the files in dir that match pattern and were
// last written before cutoff: what saves interrupted by a crash left. The
// history store saves its index the same way. Errors are ignored; a file
// that can't be removed is tried again next time.
func RemoveStaleTemps(dir, pattern string, cutoff time.Time) {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	for _, match := range matches {
		if info, err := os.Lstat(match); err == nil && info.Mode().IsRegular() && info.ModTime().Before(cutoff) {
			_ = os.Remove(match)
		}
	}
}

// ErrStaleFormat means a saved shard was written by another format version.
var ErrStaleFormat = fmt.Errorf("shard format is not version %d", shardFormatVersion)

// Load reads a shard written by Save.
func Load(path string) (*Shard, error) {
	f, err := os.Open(path) //nolint:gosec // G304: path is under the index directory the user configured
	if err != nil {
		return nil, err
	}
	defer closeReadOnly(f)
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
	if err := saved.check(); err != nil {
		return nil, fmt.Errorf("corrupt shard %s: %w", path, err)
	}
	return &Shard{Docs: saved.Docs, BuiltAt: saved.BuiltAt, postings: saved.Postings}, nil
}

// check reports what makes a decoded shard unsafe to search: a doc path
// outside the repo, or a posting list that names a doc that doesn't exist
// or isn't in ascending order. A damaged file can decode without error.
func (s *savedShard) check() error {
	for i := range s.Docs {
		if !filepath.IsLocal(filepath.FromSlash(s.Docs[i].Path)) {
			return fmt.Errorf("doc %d has path %q", i, s.Docs[i].Path)
		}
	}
	for key, list := range s.Postings {
		for i, id := range list {
			if int64(id) >= int64(len(s.Docs)) || i > 0 && list[i-1] >= id {
				return fmt.Errorf("trigram %06x lists doc %d out of place", key, id)
			}
		}
	}
	return nil
}
