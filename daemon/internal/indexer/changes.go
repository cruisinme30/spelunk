package indexer

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// maxOverlayFiles is how many re-read files a repo's overlay holds before
// the indexer rebuilds the shard, quietly, to fold them in.
const maxOverlayFiles = 500

// overlay is the files of one repo re-read since its shard was built:
// saved, created or deleted. It is immutable; changes make a new one.
type overlay struct {
	shard  *trigram.Shard         // the current content of those that exist; nil when empty
	masked map[string]bool        // the shard's paths that are out of date or gone
	docs   map[string]trigram.Doc // by path, the overlay's files
	readAt map[string]time.Time   // when each masked path was re-read
}

// with returns the overlay after re-reading changed paths: docs are their
// current content (paths without a doc were deleted, or are no longer
// indexed), and masked are the shard paths they replace.
func (o overlay) with(masked []string, docs []trigram.Doc, now time.Time) overlay {
	next := overlay{
		masked: maps.Clone(o.masked), docs: maps.Clone(o.docs), readAt: maps.Clone(o.readAt),
	}
	if next.masked == nil {
		next.masked, next.docs, next.readAt = map[string]bool{}, map[string]trigram.Doc{}, map[string]time.Time{}
	}
	for _, path := range masked {
		next.masked[path] = true
		next.readAt[path] = now
		delete(next.docs, path)
	}
	for i := range docs {
		next.docs[docs[i].Path] = docs[i]
	}
	return next.indexed()
}

// since keeps only what was re-read at or after t: a shard built from t on
// already has the rest.
func (o overlay) since(t time.Time) overlay {
	next := overlay{masked: map[string]bool{}, docs: map[string]trigram.Doc{}, readAt: map[string]time.Time{}}
	for path, at := range o.readAt {
		if at.Before(t) {
			continue
		}
		next.masked[path], next.readAt[path] = true, at
		if doc, ok := o.docs[path]; ok {
			next.docs[path] = doc
		}
	}
	return next.indexed()
}

// indexed builds the overlay's shard from its docs, in path order.
func (o overlay) indexed() overlay {
	if len(o.masked) == 0 {
		return overlay{}
	}
	paths := make([]string, 0, len(o.docs))
	for path := range o.docs {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	docs := make([]trigram.Doc, len(paths))
	for i, path := range paths {
		docs[i] = o.docs[path]
	}
	o.shard = trigram.NewShard(docs)
	return o
}

// FilesChanged re-reads files the editor saved, created or deleted, so
// searches see them within a second without rebuilding the repo's index.
// Paths are absolute; changes outside the open roots are ignored.
func (ix *Indexer) FilesChanged(changes []protocol.FileChange) {
	for id, paths := range ix.changedPaths(changes) {
		ix.reread(id, paths)
	}
}

// changedPaths groups changed paths by the root they are in, as
// slash-separated paths relative to it: the innermost root, when open
// roots nest, since a file is indexed only there. Paths inside .git are
// skipped.
func (ix *Indexer) changedPaths(changes []protocol.FileChange) map[string][]string {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	byRepo := map[string][]string{}
	for _, change := range changes {
		owner, path, depth := "", "", -1
		for _, id := range ix.order {
			root := ix.repos[id].root.Path
			relative, err := filepath.Rel(root, change.Path)
			if err != nil || !filepath.IsLocal(relative) {
				continue
			}
			if d := len(filepath.Clean(root)); d > depth {
				owner, path, depth = id, filepath.ToSlash(relative), d
			}
		}
		if owner != "" && path != ".git" && !strings.HasPrefix(path, ".git/") {
			byRepo[owner] = append(byRepo[owner], path)
		}
	}
	return byRepo
}

// reread re-reads paths of one repo into its overlay. A folder that is
// deleted, created or renamed arrives as one path, so the indexed files
// under it are masked too, and the files now in it are read.
func (ix *Indexer) reread(id string, paths []string) {
	ix.mu.Lock()
	r, ok := ix.repos[id]
	if !ok {
		ix.mu.Unlock()
		return
	}
	root, settings, inGit, nested := r.root, ix.settings, r.history != nil, ix.nestedRoots(r.root.Path)
	shards := []*trigram.Shard{r.shard, r.overlay.shard}
	ix.mu.Unlock()
	masked := withFilesUnder(paths, shards...) // shards are immutable: no lock needed

	ctx := ix.life
	walk := walkOptions(settings, nested)
	files := trigram.SelectFiles(ctx, root.Path, masked, walk)
	for _, file := range files {
		// A folder's path stands for the files read from it, which the
		// overlay must remember as re-read too (see overlay.since).
		masked = append(masked, file.Path)
	}
	read, err := trigram.Build(ctx, root.Path, files, trigram.BuildOptions{
		Symbols: settings.Symbols, MaxFileBytes: walk.MaxFileBytes, NotebookOutputs: settings.NotebookOutputs,
	})
	if err != nil {
		return
	}
	ownersChanged := changesOwners(paths, settings.CodeownersFiles)
	var owners *codeowners.File
	if ownersChanged {
		owners = codeowners.Load(root.Path, settings.CodeownersFiles)
	}
	// Ask Git again which files differ from HEAD: a save can undo a change
	// as well as make one, and so can a stash or a checkout of a file.
	var dirty map[string]bool
	if inGit {
		dirty = history.UncommittedPaths(ctx, root.Path)
	}
	if ctx.Err() != nil {
		return
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()
	r, ok = ix.repos[id]
	if !ok || r.root.Path != root.Path {
		return
	}
	r.overlay = r.overlay.with(masked, read.Docs, time.Now())
	if ownersChanged {
		r.owners = owners
	}
	if r.history != nil && dirty != nil {
		r.history = r.history.WithDirty(dirty)
	}
	if len(r.overlay.masked) > maxOverlayFiles {
		ix.refresh(id)
	}
}

// changesOwners reports whether a change to paths can change who owns
// what: one of them is where a CODEOWNERS file goes (one of locations, nil
// for the default ones), or a folder that holds one, deleted or renamed.
func changesOwners(paths, locations []string) bool {
	for _, path := range paths {
		for _, location := range codeowners.LocationsOr(locations) {
			if location == path || strings.HasPrefix(location, path+"/") {
				return true
			}
		}
	}
	return false
}

// withFilesUnder adds to paths the files of shards (the built shard and
// the overlay; either may be nil) under any of them, for a folder that was
// deleted or renamed, each path once. Each path costs a binary search of
// a shard and its own files: a checkout's thousands of changed paths in a
// large repo don't each scan every file.
func withFilesUnder(paths []string, shards ...*trigram.Shard) []string {
	all := slices.Clone(paths)
	for _, shard := range shards {
		if shard == nil {
			continue
		}
		for _, path := range paths {
			all = append(all, shard.PathsUnder(path)...)
		}
	}
	slices.Sort(all)
	return slices.Compact(all)
}
