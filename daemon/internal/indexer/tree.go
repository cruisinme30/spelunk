package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
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
	nested     []string // the open roots inside this one
	shardPath  string
	started    time.Time
	before     func(id string) // Indexer.beforeBuild
}

// nextBuild takes the first queued repo and marks it as building.
func (ix *Indexer) nextBuild(ctx context.Context) (buildJob, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	id, r, ok := ix.popQueued(&ix.treeQueue)
	if !ok {
		return buildJob{}, false
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
		settings: ix.settings, nested: ix.nestedRoots(r.root.Path), shardPath: shardPath(ix.settings.Location, r.root), started: time.Now(),
		before: ix.beforeBuild,
	}, true
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
	var owners *codeowners.File
	if err == nil {
		saveErr = shard.Save(job.shardPath)
		owners = codeowners.Load(job.root.Path, job.settings.CodeownersFiles)
	}
	ix.update(job, func(r *repo) {
		r.cancelBuild = nil
		if err != nil {
			r.state, r.message = protocol.IndexStateError, err.Error()
			return
		}
		// A shard that couldn't be saved still serves this session.
		r.shard, r.state, r.message, r.visible = shard, protocol.IndexStateReady, "", false
		r.owners = owners
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
	walk := walkOptions(job.settings, job.nested)
	files, err := trigram.ListFiles(job.ctx, job.root.Path, walk)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return trigram.Build(job.ctx, job.root.Path, files, trigram.BuildOptions{
		Symbols: job.settings.Symbols, Progress: progress, MaxFileBytes: walk.MaxFileBytes,
	})
}

// walkOptions are the index.* settings that decide which files are
// indexed, leaving out the nested roots' folders.
func walkOptions(settings protocol.Settings, nested []string) trigram.WalkOptions {
	return trigram.WalkOptions{
		Exclude:        trigram.NewExcluder(settings.Exclude),
		IncludeIgnored: settings.IncludeIgnored,
		MaxFileBytes:   int64(settings.MaxFileSizeKB) * 1024,
		Nested:         nested,
	}
}

// nestedRoots lists the open roots inside root, as slash-separated paths
// relative to it, sorted. Callers hold mu.
func (ix *Indexer) nestedRoots(root string) []string {
	var nested []string
	for _, id := range ix.order {
		if relative, ok := inside(root, ix.repos[id].root.Path); ok {
			nested = append(nested, relative)
		}
	}
	slices.Sort(nested)
	return nested
}

// inside returns path relative to root, slash-separated, when path is
// strictly inside root.
func inside(root, path string) (string, bool) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}
