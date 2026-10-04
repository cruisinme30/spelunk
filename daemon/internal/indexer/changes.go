package indexer

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
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
	for _, doc := range docs {
		next.docs[doc.Path] = doc
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
// slash-separated paths relative to it. Paths inside .git are skipped.
func (ix *Indexer) changedPaths(changes []protocol.FileChange) map[string][]string {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	byRepo := map[string][]string{}
	for _, change := range changes {
		for _, id := range ix.order {
			root := ix.repos[id].root.Path
			relative, err := filepath.Rel(root, change.Path)
			if err != nil || !filepath.IsLocal(relative) {
				continue
			}
			relative = filepath.ToSlash(relative)
			if relative != ".git" && !strings.HasPrefix(relative, ".git/") {
				byRepo[id] = append(byRepo[id], relative)
			}
		}
	}
	return byRepo
}

// reread re-reads paths of one repo into its overlay. A deleted folder
// arrives as one path, so the shard's files under it are masked too.
func (ix *Indexer) reread(id string, paths []string) {
	ix.mu.Lock()
	r, ok := ix.repos[id]
	if !ok {
		ix.mu.Unlock()
		return
	}
	root, settings := r.root, ix.settings
	masked := withFilesUnder(paths, r.shard)
	ix.mu.Unlock()

	ctx := context.Background()
	files := trigram.SelectFiles(ctx, root.Path, masked, walkOptions(settings))
	read, err := trigram.Build(ctx, root.Path, files, trigram.BuildOptions{Symbols: settings.Symbols})
	if err != nil {
		return
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()
	r, ok = ix.repos[id]
	if !ok || r.root.Path != root.Path {
		return
	}
	r.overlay = r.overlay.with(masked, read.Docs, time.Now())
	if r.history != nil {
		r.history = r.history.MarkDirty(paths)
	}
	if len(r.overlay.masked) > maxOverlayFiles {
		ix.refresh(id)
	}
}

// withFilesUnder adds to paths the shard's files under any of them, for a
// folder that was deleted or renamed.
func withFilesUnder(paths []string, shard *trigram.Shard) []string {
	all := slices.Clone(paths)
	if shard == nil {
		return all
	}
	for _, path := range paths {
		prefix := path + "/"
		for i := range shard.Docs {
			if strings.HasPrefix(shard.Docs[i].Path, prefix) {
				all = append(all, shard.Docs[i].Path)
			}
		}
	}
	return all
}
