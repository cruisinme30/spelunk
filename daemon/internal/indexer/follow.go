package indexer

import (
	"context"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// overlayKeep is how far before a loaded shard's BuiltAt a follower keeps
// re-read files: BuiltAt is when the writer's build finished, and a file
// saved while it ran may not be in it. Keeping a file the shard has is
// harmless; the overlay's copy is the current one.
const overlayKeep = 10 * time.Minute

// followJob is a follower repo whose saved indexes may need loading.
type followJob struct {
	id                      string
	root                    protocol.Root
	savedTree, savedHistory time.Time
}

// follow keeps every repo this indexer doesn't write up to date with its
// writer, on each commit check: it takes the write lock when the writer
// has gone (the window closed), or else loads indexes the writer saved
// since this repo last loaded them.
func (ix *Indexer) follow(ctx context.Context) {
	jobs := ix.followers()
	if len(jobs) == 0 {
		return
	}
	ix.mu.Lock()
	location, codeownersFiles := ix.settings.Location, ix.settings.CodeownersFiles
	ix.mu.Unlock()
	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		treeTime, historyTime := modTime(shardPath(location, job.root)), modTime(historyPath(location, job.root))
		newTree, newHistory := treeTime.After(job.savedTree), historyTime.After(job.savedHistory)
		if !newTree && !newHistory {
			continue
		}
		// Decoding a large index takes a while: no lock held meanwhile.
		saved := loadSaved(ctx, location, codeownersFiles, job.root, newTree, newHistory)
		ix.applyFollowed(job, saved, newTree, newHistory)
	}
}

// followers takes over the repos whose writer has gone and returns the
// others that follow a writer. Callers must not hold mu.
func (ix *Indexer) followers() []followJob {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var jobs []followJob
	took := false
	for _, id := range ix.order {
		r := ix.repos[id]
		if r.lock != nil {
			continue
		}
		if !ix.readOnly {
			if lock, err := tryLock(lockPath(ix.settings.Location, r.root)); err == nil {
				ix.takeOver(id, lock)
				took = true
				continue
			}
		}
		jobs = append(jobs, followJob{id: id, root: r.root, savedTree: r.savedTree, savedHistory: r.savedHistory})
	}
	if took {
		go ix.publishStatus()
	}
	return jobs
}

// takeOver makes this indexer a repo's writer once its writer has gone:
// it refreshes both indexes, quietly when it already has them. Callers
// hold mu.
func (ix *Indexer) takeOver(id string, lock *writeLock) {
	r := ix.repos[id]
	r.lock = lock
	if r.shard == nil {
		ix.restart(id)
	} else {
		ix.refresh(id)
	}
	if r.history == nil || r.historyFull {
		ix.restartHistory(id)
	} else {
		ix.historyQueue.add(id)
	}
}

// applyFollowed publishes what follow loaded, if the repo still follows
// the same root.
func (ix *Indexer) applyFollowed(job followJob, saved savedIndex, newTree, newHistory bool) {
	ix.mu.Lock()
	r, ok := ix.repos[job.id]
	if !ok || r.lock != nil || r.root.Path != job.root.Path {
		ix.mu.Unlock()
		return
	}
	changed := false
	if newTree {
		r.savedTree = saved.treeTime
		if saved.shard != nil {
			r.shard, r.owners = saved.shard, saved.owners
			r.overlay = r.overlay.since(saved.shard.BuiltAt.Add(-overlayKeep))
			r.state, r.message, r.visible = protocol.IndexStateReady, "", false
			changed = true
		}
	}
	if newHistory {
		r.savedHistory = saved.historyTime
		if saved.history != nil {
			r.history, r.historyState, r.historyMessage = saved.history, protocol.IndexStateReady, ""
			r.historyFull = saved.history.Key != historyKey(ix.settings)
			changed = true
		}
	}
	ix.mu.Unlock()
	if changed {
		ix.publishStatus()
	}
}
