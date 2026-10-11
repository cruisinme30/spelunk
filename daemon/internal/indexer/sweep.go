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
	changed := job.changedFiles(root, files)
	if len(changed) > 0 && ctx.Err() == nil {
		ix.reread(job.id, changed)
	}
}

// sweepJob is what one sweep compares the files on disk with.
type sweepJob struct {
	id       string
	root     string
	shard    *trigram.Shard
	overlay  overlay
	settings protocol.Settings
	nested   []string
}

// changedFiles compares files, as listed now, with the index: the paths
// edited, created or deleted since it was built.
func (job *sweepJob) changedFiles(root string, files []trigram.File) []string {
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
		at, ok := known[file.Path]
		delete(known, file.Path)
		switch {
		case ok && at.Equal(info.ModTime()):
			// Unchanged.
		case !ok && info.ModTime().Before(job.shard.BuiltAt) && !job.overlay.masked[file.Path]:
			// There when the index was built, but not in it: a binary or
			// oversized file the build read and left out.
		default:
			changed = append(changed, file.Path)
		}
	}
	for path := range known { // indexed, and no longer there
		changed = append(changed, path)
	}
	return changed
}

// writerSweepAfter and writerSweepEvery are when a window sweeps the repos
// it writes: soon after it starts, as a saved index may be older than files
// changed while VS Code was closed, then every few minutes, for the files
// VS Code doesn't watch (folders files.watcherExclude leaves out, or a disk
// it can't watch).
const (
	writerSweepAfter = 2 * time.Second
	writerSweepEvery = 2 * time.Minute
)

// sweepWork sweeps the repos this indexer writes, now and then, until ctx
// ends.
func (ix *Indexer) sweepWork(ctx context.Context) {
	defer ix.workers.Done()
	timer := time.NewTimer(writerSweepAfter)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			ix.sweepWriters(ctx)
			timer.Reset(writerSweepEvery)
		}
	}
}

// sweepWriters re-reads the files of each repo this indexer writes that
// changed on disk without the editor saying so, as Sweep does for a reader.
func (ix *Indexer) sweepWriters(ctx context.Context) {
	jobs := ix.writerSweepJobs()
	for i := range jobs {
		job := &jobs[i]
		files, err := trigram.ListFiles(ctx, job.root, walkOptions(job.settings, job.nested))
		if err != nil || ctx.Err() != nil {
			continue
		}
		if changed := job.changedFiles(job.root, files); len(changed) > 0 {
			ix.reread(job.id, changed)
		}
	}
}

// writerSweepJobs lists the repos this indexer writes that have an index.
func (ix *Indexer) writerSweepJobs() []sweepJob {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var jobs []sweepJob
	for _, id := range ix.order {
		r := ix.repos[id]
		if r.lock == nil || r.shard == nil {
			continue
		}
		r.swept = time.Now()
		jobs = append(jobs, sweepJob{id: id, root: r.root.Path, shard: r.shard, overlay: r.overlay, settings: ix.settings, nested: ix.nestedRoots(r.root.Path)})
	}
	return jobs
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
		return sweepJob{id: id, root: root, shard: r.shard, overlay: r.overlay, settings: ix.settings, nested: ix.nestedRoots(root)}, true
	}
	return sweepJob{}, false
}
