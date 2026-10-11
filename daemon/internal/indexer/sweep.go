package indexer

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// sweepEvery is the least time between two sweeps of one repo: a search
// right after another doesn't list the files again.
const sweepEvery = 5 * time.Second

// Sweep re-reads the files of the repo at root that changed on disk since
// its saved index was built: edited, created or deleted. A VS Code window
// hears of changes from the editor; the spelunk command's reader has no
// editor, so it sweeps before it searches. Nothing is saved. It does
// nothing for a repo this indexer writes, one without an index yet, or
// one swept in the last few seconds.
func (ix *Indexer) Sweep(ctx context.Context, root string) {
	job, ok := ix.sweepStart(root)
	if !ok {
		return
	}
	files, err := trigram.ListFiles(ctx, root, walkOptions(job.settings, job.nested))
	if err != nil {
		return
	}
	known := map[string]time.Time{}
	for i := range job.shard.Docs {
		if !job.overlay.masked[job.shard.Docs[i].Path] {
			known[job.shard.Docs[i].Path] = job.shard.Docs[i].ModTime
		}
	}
	for path := range job.overlay.docs {
		known[path] = job.overlay.docs[path].ModTime
	}
	var changed []string
	for _, file := range files {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			continue
		}
		if at, ok := known[file.Path]; !ok || !at.Equal(info.ModTime()) {
			changed = append(changed, file.Path)
		}
		delete(known, file.Path)
	}
	for path := range known { // indexed, and no longer there
		changed = append(changed, path)
	}
	if len(changed) > 0 && ctx.Err() == nil {
		ix.reread(job.id, changed)
	}
}

// sweepJob is what one sweep compares the files on disk with.
type sweepJob struct {
	id       string
	shard    *trigram.Shard
	overlay  overlay
	settings protocol.Settings
	nested   []string
}

// sweepStart finds the repo at root and marks it swept, if it is due.
func (ix *Indexer) sweepStart(root string) (sweepJob, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for _, id := range ix.order {
		r := ix.repos[id]
		if r.root.Path != root || r.lock != nil || r.shard == nil || time.Since(r.swept) < sweepEvery {
			continue
		}
		r.swept = time.Now()
		return sweepJob{id: id, shard: r.shard, overlay: r.overlay, settings: ix.settings, nested: ix.nestedRoots(root)}, true
	}
	return sweepJob{}, false
}
