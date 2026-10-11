package trigram

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/notebook"
	"github.com/cruisinme30/spelunk/daemon/internal/symbols"
)

// shardFormatVersion changes whenever the saved layout does; an index
// written by another version is rebuilt rather than migrated.
const shardFormatVersion = 4

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
	// Cells are a Jupyter notebook's cells, whose text Content is (see
	// notebook.Read); nil for any other file.
	Cells []notebook.Cell
}

// Shard is the immutable index of one repo's current files.
type Shard struct {
	// Docs are the files, in ascending path order (see PathsUnder).
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
	// NotebookOutputs also indexes the text a notebook's code cells printed
	// (index.notebookOutputs).
	NotebookOutputs bool
}

// Build reads files from root, which are in path order, and indexes them
// in that order. A file that changed since it was listed is read as it is
// now; one that is gone, or is now a symlink, a FIFO or another special
// file, binary or too big, is left out. Files are read buildWorkers at a
// time, since reading, decoding and finding definitions wait on the disk
// and run apart; they are indexed one by one, in order, so the shard is
// the same however the reads interleave.
func Build(ctx context.Context, root string, files []File, opts BuildOptions) (*Shard, error) {
	if int64(len(files)) >= maxDocs {
		return nil, fmt.Errorf("%d files is more than one index can hold (%d)", len(files), maxDocs)
	}
	s := &Shard{BuiltAt: time.Now(), postings: map[uint32][]uint32{}}
	reads := readDocs(ctx, root, files, opts)
	i := 0
	for read := range reads {
		<-read.done
		if ctx.Err() != nil {
			continue // let the reads already started finish before returning
		}
		if opts.Progress != nil && i%progressEvery == 0 {
			opts.Progress(float64(i) / float64(len(files)))
		}
		i++
		if read.ok {
			s.add(read.doc)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// buildWorkers is how many files Build reads at once, and readAhead how
// many it may have read, or be reading, ahead of the one it indexes.
var (
	buildWorkers = min(runtime.GOMAXPROCS(0), 8)
	readAhead    = 4 * buildWorkers
)

// docRead is one file Build reads: done closes once doc and ok are set.
type docRead struct {
	doc  Doc
	ok   bool
	done chan struct{}
}

// readDocs reads files buildWorkers at a time and sends each one's read,
// in the files' order, as soon as it starts; the receiver waits on its
// done. It stops starting reads when ctx ends, and closes the channel
// once it has sent every read it started.
func readDocs(ctx context.Context, root string, files []File, opts BuildOptions) <-chan *docRead {
	reads := make(chan *docRead, readAhead)
	go func() {
		defer close(reads)
		workers := make(chan struct{}, buildWorkers)
		for _, file := range files {
			if ctx.Err() != nil {
				return
			}
			read := &docRead{done: make(chan struct{})}
			reads <- read // waits while readAhead reads are ahead of the indexing
			workers <- struct{}{}
			go func() {
				defer func() { <-workers }()
				defer close(read.done)
				read.doc, read.ok = readDoc(root, file.Path, opts)
			}()
		}
	}()
	return reads
}

// readDoc reads the file at path (relative to root) as Build indexes it;
// ok is false when the file is left out (see Build).
func readDoc(root, path string, opts BuildOptions) (Doc, bool) {
	content, info, err := readText(filepath.Join(root, filepath.FromSlash(path)), ReadLimit(path, opts.MaxFileBytes))
	if err != nil {
		return Doc{}, false // deleted or replaced since it was listed, binary, or too big
	}
	return docOf(path, content, info.ModTime(), opts)
}

// docOf makes the doc of a file's decoded content: a notebook's is its
// cells' text, which opts.MaxFileBytes then bounds.
func docOf(path string, content []byte, modTime time.Time, opts BuildOptions) (Doc, bool) {
	text, language, cells := searchText(path, content, opts.NotebookOutputs)
	if opts.MaxFileBytes > 0 && int64(len(text)) > opts.MaxFileBytes {
		return Doc{}, false
	}
	doc := Doc{Path: path, Lang: language, ModTime: modTime, Content: text, Cells: cells}
	if opts.Symbols {
		doc.Symbols = symbolsOf(doc.Lang, text, cells)
	}
	return doc, true
}

// DocFromBlob makes the doc of a file read from somewhere other than disk,
// such as a commit's tree, as Build would read it: ok is false for a
// binary file, which Build leaves out, and for one past opts.MaxFileBytes.
// modTime stands in for the file's modification time; opts.Symbols finds
// its definitions.
func DocFromBlob(path string, raw []byte, modTime time.Time, opts BuildOptions) (Doc, bool) {
	content, ok := DecodeBlob(raw)
	if !ok {
		return Doc{}, false
	}
	return docOf(path, content, modTime, opts)
}

// DecodeBlob decodes a file's bytes read from somewhere other than disk as
// Build decodes a file it reads (see decodeText); ok is false when they are
// binary.
func DecodeBlob(raw []byte) ([]byte, bool) {
	if isBinary(raw[:min(len(raw), binarySniffBytes)]) {
		return nil, false
	}
	return decodeText(raw), true
}

// NewShard indexes docs that are already read, keeping their order. The
// indexer builds its overlay of re-read files with it.
func NewShard(docs []Doc) *Shard {
	s := &Shard{BuiltAt: time.Now(), postings: map[uint32][]uint32{}}
	for i := range docs {
		s.add(docs[i])
	}
	return s
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
		result = engine.Intersect(result, list)
		if len(result) == 0 {
			break
		}
	}
	return result
}

// PathsUnder returns the paths of the docs inside folder, a
// slash-separated path relative to the repo root, in path order. They are
// next to each other in Docs, so it takes a binary search and a doc each.
func (s *Shard) PathsUnder(folder string) []string {
	prefix := folder + "/"
	var paths []string
	for i := sort.Search(len(s.Docs), func(i int) bool { return s.Docs[i].Path >= prefix }); i < len(s.Docs) && strings.HasPrefix(s.Docs[i].Path, prefix); i++ {
		paths = append(paths, s.Docs[i].Path)
	}
	return paths
}

// savedShard is the on-disk form of a Shard.
type savedShard struct {
	Version  int
	BuiltAt  time.Time
	Docs     []Doc
	Postings map[uint32][]uint32
}

// Save writes the shard to path atomically (see SaveGob).
func (s *Shard) Save(path string) error {
	saved := savedShard{Version: shardFormatVersion, BuiltAt: s.BuiltAt, Docs: s.Docs, Postings: s.postings}
	return SaveGob(path, ".shard-*", "shard", saved)
}

// ErrStaleFormat means a saved shard was written by another format version.
var ErrStaleFormat = fmt.Errorf("shard format is not version %d", shardFormatVersion)

// Load reads a shard written by Save.
func Load(path string) (*Shard, error) {
	var saved savedShard
	if err := LoadGob(path, "shard", &saved); err != nil {
		return nil, err
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
// outside the repo or out of path order, or a posting list that names a doc that doesn't exist
// or isn't in ascending order. A damaged file can decode without error.
func (s *savedShard) check() error {
	for i := range s.Docs {
		if !filepath.IsLocal(filepath.FromSlash(s.Docs[i].Path)) {
			return fmt.Errorf("doc %d has path %q", i, s.Docs[i].Path)
		}
		if i > 0 && s.Docs[i-1].Path >= s.Docs[i].Path {
			return fmt.Errorf("doc %d (%q) is out of path order", i, s.Docs[i].Path)
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
