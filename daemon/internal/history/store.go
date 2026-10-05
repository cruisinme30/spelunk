package history

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// storeFormatVersion changes whenever the saved layout does; a store written
// by another version is read again from Git rather than migrated.
const storeFormatVersion = 2

// Segment sizes: the first segment is small, so the newest commits are
// searchable within seconds; later ones are larger, so there are few.
const (
	firstSegmentCommits = 200
	segmentCommits      = 2_000
)

// Store is the history index of one repo. It is immutable: Ingest and
// Update return new stores, so a search never sees one half updated.
type Store struct {
	// Head is the commit the store is up to date with; "" for a repo
	// without commits.
	Head string
	// Since is where the history window starts (zero: all of it).
	Since time.Time
	// segments hold the commits, newest first.
	segments []*segment
	// touches maps each path to the newest commit that changed it.
	touches map[string]Touch
	// dirty holds the paths with uncommitted changes.
	dirty map[string]bool
}

// Touch is the newest commit that changed a file.
type Touch struct {
	SHA    string
	Author string
	At     time.Time
}

// segment is a run of consecutive commits with their trigram indexes.
type segment struct {
	commits  []Commit
	diffs    *trigram.TextIndex // each commit's changed lines
	messages *trigram.TextIndex // each commit's subject and body
}

// newSegment indexes commits.
func newSegment(commits []Commit) *segment {
	diffs := make([][]byte, len(commits))
	messages := make([][]byte, len(commits))
	for i := range commits {
		diffs[i] = changedText(&commits[i])
		messages[i] = []byte(commits[i].Subject + "\n" + commits[i].Body)
	}
	return &segment{commits: commits, diffs: trigram.NewTextIndex(diffs), messages: trigram.NewTextIndex(messages)}
}

// changedText joins the changed lines of a commit's files, for indexing.
func changedText(c *Commit) []byte {
	if len(c.Files) == 1 {
		return c.Files[0].Text
	}
	var b bytes.Buffer
	for _, file := range c.Files {
		b.Write(file.Text)
	}
	return b.Bytes()
}

// Commits counts the commits in the store.
func (s *Store) Commits() int {
	n := 0
	for _, seg := range s.segments {
		n += len(seg.commits)
	}
	return n
}

// Commit finds a commit by sha.
func (s *Store) Commit(sha string) (*Commit, bool) {
	for _, seg := range s.segments {
		for i := range seg.commits {
			if seg.commits[i].SHA == sha {
				return &seg.commits[i], true
			}
		}
	}
	return nil, false
}

// LastCommit returns the newest commit that changed path, if the history
// window holds one.
func (s *Store) LastCommit(path string) (Touch, bool) {
	touch, ok := s.touches[path]
	return touch, ok
}

// Dirty reports whether path has uncommitted changes.
func (s *Store) Dirty(path string) bool { return s.dirty[path] }

// withDirty returns a copy of the store with a new set of uncommitted paths.
func (s *Store) withDirty(dirty map[string]bool) *Store {
	next := *s
	next.dirty = dirty
	return &next
}

// MarkDirty returns a copy of the store in which paths have uncommitted
// changes, for files saved since the store was read.
func (s *Store) MarkDirty(paths []string) *Store {
	dirty := make(map[string]bool, len(s.dirty)+len(paths))
	for path := range s.dirty {
		dirty[path] = true
	}
	for _, path := range paths {
		dirty[path] = true
	}
	return s.withDirty(dirty)
}

// ------------------------------------------------------------ building

// builder collects commits newest first into segments.
type builder struct {
	store   *Store
	pending []Commit
}

// add appends a commit, closing the segment when it is full. It reports
// whether a segment was closed, which is when the store is worth publishing.
func (b *builder) add(c Commit) bool {
	b.pending = append(b.pending, c)
	for _, file := range c.Files {
		// Commits arrive newest first, so the first touch of a path is its newest.
		if _, newer := b.store.touches[file.Path]; !newer {
			b.store.touches[file.Path] = Touch{SHA: c.SHA, Author: c.AuthorName, At: c.At}
		}
	}
	size := segmentCommits
	if len(b.store.segments) == 0 {
		size = firstSegmentCommits
	}
	if len(b.pending) < size {
		return false
	}
	b.flush()
	return true
}

// flush closes the segment being filled.
func (b *builder) flush() {
	if len(b.pending) > 0 {
		b.store.segments = append(b.store.segments, newSegment(b.pending))
		b.pending = nil
	}
}

// snapshot returns an immutable copy of what has been built.
func (b *builder) snapshot() *Store {
	next := *b.store
	next.segments = slices.Clone(b.store.segments)
	next.touches = make(map[string]Touch, len(b.store.touches))
	for path, touch := range b.store.touches {
		next.touches[path] = touch
	}
	return &next
}

// Ingest reads root's history from scratch: the first-parent commits in
// the window opts sets, newest first. publish, if not nil, receives a
// growing store each time a segment is complete, so recent history is
// searchable while older history is still being read. progress receives
// the fraction read. Ingest returns ErrNotGit for a folder outside Git.
func Ingest(ctx context.Context, root string, opts Options, progress func(float64), publish func(*Store)) (*Store, error) {
	head, err := Head(ctx, root)
	if err != nil {
		return nil, err
	}
	b := &builder{store: &Store{Head: head, Since: opts.Since, touches: map[string]Touch{}}}
	if head != "" {
		if err := b.read(ctx, root, head, opts, progress, publish); err != nil {
			return nil, err
		}
	}
	b.flush()
	store := b.snapshot()
	return store.withDirty(uncommittedPaths(ctx, root)), nil
}

// read adds the commits of revisions to the builder.
func (b *builder) read(ctx context.Context, root, revisions string, opts Options, progress func(float64), publish func(*Store)) error {
	total := countCommits(ctx, root, revisions, opts)
	read := 0
	return readLog(ctx, root, revisions, opts, func(c Commit) error {
		read++
		if progress != nil && total > 0 && read%100 == 0 {
			progress(float64(read) / float64(total))
		}
		if b.add(c) && publish != nil {
			publish(b.snapshot())
		}
		return ctx.Err()
	})
}

// Update brings store up to date with root's HEAD. When HEAD moved forward,
// only the new commits are read, as a new newest segment; when history was
// rewritten (a rebase, a reset, another branch), all of it is read again.
// Either way the uncommitted paths are read again. opts should be the ones
// the store was read with.
func Update(ctx context.Context, root string, store *Store, opts Options) (*Store, error) {
	head, err := Head(ctx, root)
	if err != nil {
		return nil, err
	}
	if head == store.Head {
		return store.withDirty(uncommittedPaths(ctx, root)), nil
	}
	if store.Head == "" || !isAncestor(ctx, root, store.Head) {
		return Ingest(ctx, root, opts, nil, nil)
	}
	var newer []Commit
	if err := readLog(ctx, root, store.Head+".."+head, opts, func(c Commit) error {
		newer = append(newer, c)
		return ctx.Err()
	}); err != nil {
		return nil, err
	}
	next := &Store{Head: head, Since: store.Since, touches: make(map[string]Touch, len(store.touches))}
	for path, touch := range store.touches {
		next.touches[path] = touch
	}
	for i := len(newer) - 1; i >= 0; i-- { // oldest first, so the newest touch wins
		for _, file := range newer[i].Files {
			next.touches[file.Path] = Touch{SHA: newer[i].SHA, Author: newer[i].AuthorName, At: newer[i].At}
		}
	}
	next.segments = append([]*segment{newSegment(newer)}, store.segments...)
	return next.withDirty(uncommittedPaths(ctx, root)), nil
}

// uncommittedPaths lists root's files that differ from HEAD: modified,
// staged, or new and not ignored.
func uncommittedPaths(ctx context.Context, root string) map[string]bool {
	dirty := map[string]bool{}
	for _, args := range [][]string{
		{"ls-files", "--modified", "--others", "--exclude-standard"},
		{"diff", "--name-only", "--cached", "--relative"},
	} {
		out, err := git(ctx, root, args...)
		if err != nil {
			continue
		}
		for _, path := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if path != "" {
				dirty[unquote(path)] = true
			}
		}
	}
	return dirty
}

// ------------------------------------------------------------ saving

// savedStore is the on-disk form of a Store: the commits, newest first.
// The trigram indexes and touches are rebuilt on load.
type savedStore struct {
	Version int
	Head    string
	Since   time.Time
	Commits []Commit
}

// Save writes the store to path atomically (see trigram.SaveGob).
func (s *Store) Save(path string) error {
	saved := savedStore{Version: storeFormatVersion, Head: s.Head, Since: s.Since}
	for _, seg := range s.segments {
		saved.Commits = append(saved.Commits, seg.commits...)
	}
	return trigram.SaveGob(path, ".history-*", "history", saved)
}

// ErrStaleFormat means a saved store was written by another format version.
var ErrStaleFormat = errors.New("history format changed")

// Load reads a store written by Save. Its uncommitted paths are empty until
// the next Update.
func Load(path string) (*Store, error) {
	var saved savedStore
	if err := trigram.LoadGob(path, "history", &saved); err != nil {
		return nil, err
	}
	if saved.Version != storeFormatVersion {
		return nil, ErrStaleFormat
	}
	b := &builder{store: &Store{Head: saved.Head, Since: saved.Since, touches: map[string]Touch{}}}
	for i := range saved.Commits {
		b.add(saved.Commits[i])
	}
	b.flush()
	return b.snapshot().withDirty(map[string]bool{}), nil
}
