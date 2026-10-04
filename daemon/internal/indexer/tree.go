package indexer

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

// treeWork builds queued repos' working-tree indexes, one at a time, until
// ctx ends.
func (ix *Indexer) treeWork(ctx context.Context) {
	defer ix.workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ix.treeQueue.wake:
		}
		for ctx.Err() == nil {
			job, ok := ix.nextBuild(ctx)
			if !ok {
				break
			}
			ix.build(job)
		}
	}
}

// restart abandons a repo's build in progress and queues a new one, shown
// as queued and then indexing. Callers hold mu.
func (ix *Indexer) restart(id string) {
	r := ix.repos[id]
	r.generation++
	if r.cancelBuild != nil {
		r.cancelBuild()
	}
	r.visible = true
	r.state, r.message = protocol.IndexStateQueued, ""
	ix.treeQueue.add(id)
}

// refresh queues a quiet rebuild: the repo stays "ready" meanwhile. It is
// how a large overlay is folded back into the shard. Callers hold mu.
func (ix *Indexer) refresh(id string) {
	r := ix.repos[id]
	if r.cancelBuild == nil {
		ix.treeQueue.add(id)
	}
}

// buildJob is a snapshot of what one build needs.
type buildJob struct {
	ctx        context.Context
	done       context.CancelFunc
	id         string
	root       protocol.Root
	generation int
	settings   protocol.Settings
	shardPath  string
	started    time.Time
	before     func(id string) // Indexer.beforeBuild
}

// nextBuild takes the first queued repo and marks it as building.
func (ix *Indexer) nextBuild(ctx context.Context) (buildJob, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for len(ix.treeQueue.ids) > 0 {
		id := ix.treeQueue.ids[0]
		ix.treeQueue.ids = ix.treeQueue.ids[1:]
		r, ok := ix.repos[id]
		if !ok {
			continue
		}
		buildCtx, cancel := context.WithCancel(ctx)
		r.cancelBuild = cancel
		if r.visible {
			// The message stays: a saved index that couldn't be read says
			// so until the rebuild is done.
			r.state, r.progress = protocol.IndexStateIndexing, 0
		}
		return buildJob{
			ctx: buildCtx, done: cancel, id: id, root: r.root, generation: r.generation,
			settings: ix.settings, shardPath: shardPath(ix.settings.Location, r.root), started: time.Now(),
			before: ix.beforeBuild,
		}, true
	}
	return buildJob{}, false
}

// build lists, indexes and saves one repo, then publishes the shard.
func (ix *Indexer) build(job buildJob) {
	defer job.done()
	ix.publishStatus()
	if job.before != nil {
		job.before(job.id)
	}
	shard, err := buildShard(job, func(progress float64) {
		visible := false
		ix.update(job, func(r *repo) { r.progress, visible = progress, r.visible })
		if visible {
			ix.publishStatus()
		}
	})
	if job.ctx.Err() != nil {
		return // superseded, dropped or shutting down
	}
	var saveErr error
	if err == nil {
		saveErr = shard.Save(job.shardPath)
	}
	ix.update(job, func(r *repo) {
		r.cancelBuild = nil
		if err != nil {
			r.state, r.message = protocol.IndexStateError, err.Error()
			return
		}
		// A shard that couldn't be saved still serves this session.
		r.shard, r.state, r.message, r.visible = shard, protocol.IndexStateReady, "", false
		if saveErr != nil {
			r.message = "Index not saved: " + saveErr.Error()
		}
		// The new shard read every file the overlay re-read before the
		// build started; later changes stay in the overlay.
		r.overlay = r.overlay.since(job.started)
	})
	ix.publishStatus()
}

// update applies change to the job's repo if the job is still current: a
// newer build, or a dropped root, means its result no longer matters.
func (ix *Indexer) update(job buildJob, change func(*repo)) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	r, ok := ix.repos[job.id]
	if !ok || r.generation != job.generation || r.root.Path != job.root.Path {
		return
	}
	change(r)
}

// buildShard lists the root's files and indexes them.
func buildShard(job buildJob, progress func(float64)) (*trigram.Shard, error) {
	info, err := os.Stat(job.root.Path)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("folder not found: %s", job.root.Path)
	}
	files, err := trigram.ListFiles(job.ctx, job.root.Path, walkOptions(job.settings))
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return trigram.Build(job.ctx, job.root.Path, files, trigram.BuildOptions{Symbols: job.settings.Symbols, Progress: progress})
}

// walkOptions are the index.* settings that decide which files are indexed.
func walkOptions(settings protocol.Settings) trigram.WalkOptions {
	return trigram.WalkOptions{
		Exclude:        trigram.NewExcluder(settings.Exclude),
		IncludeIgnored: settings.IncludeIgnored,
		MaxFileBytes:   int64(settings.MaxFileSizeKB) * 1024,
	}
}
