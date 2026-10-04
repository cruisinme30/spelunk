package indexer

import (
	"context"
	"errors"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/history"
	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

// headPollInterval is how often the indexer asks Git whether HEAD moved
// (a commit, pull, merge, rebase or branch switch), so new commits are
// searchable within a few seconds.
const headPollInterval = 5 * time.Second

// historyWork reads queued repos' histories, one at a time, and polls for
// new commits, until ctx ends.
func (ix *Indexer) historyWork(ctx context.Context) {
	defer ix.workers.Done()
	poll := time.NewTicker(headPollInterval)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ix.historyQueue.wake:
		case <-poll.C:
			ix.queueMovedHeads(ctx)
		}
		for ctx.Err() == nil {
			job, ok := ix.nextHistory(ctx)
			if !ok {
				break
			}
			ix.readHistory(job)
		}
	}
}

// restartHistory abandons a repo's history read and queues a full one,
// shown as queued and then indexing. Callers hold mu.
func (ix *Indexer) restartHistory(id string) {
	r := ix.repos[id]
	r.historyGeneration++
	if r.cancelHistory != nil {
		r.cancelHistory()
	}
	r.historyFull = true
	if r.historyState != protocol.IndexStateOff {
		r.historyState = protocol.IndexStateQueued
	}
	ix.historyQueue.add(id)
}

// queueMovedHeads queues an update for each repo whose HEAD is no longer
// the commit its history index is up to date with.
func (ix *Indexer) queueMovedHeads(ctx context.Context) {
	ix.mu.Lock()
	known := map[string]string{}
	roots := map[string]string{}
	for _, id := range ix.order {
		if r := ix.repos[id]; r.history != nil && r.cancelHistory == nil {
			known[id], roots[id] = r.history.Head, r.root.Path
		}
	}
	ix.mu.Unlock()
	for id, head := range known {
		if now, err := history.Head(ctx, roots[id]); err == nil && now != head {
			ix.mu.Lock()
			if _, ok := ix.repos[id]; ok {
				ix.historyQueue.add(id)
			}
			ix.mu.Unlock()
		}
	}
}

// historyJob is a snapshot of what one history read needs.
type historyJob struct {
	ctx        context.Context
	done       context.CancelFunc
	id         string
	root       protocol.Root
	generation int
	full       bool
	opts       history.Options
	store      *history.Store // the store to update; nil for a full read
	path       string
}

// nextHistory takes the first queued repo and marks its history as being read.
func (ix *Indexer) nextHistory(ctx context.Context) (historyJob, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for len(ix.historyQueue.ids) > 0 {
		id := ix.historyQueue.ids[0]
		ix.historyQueue.ids = ix.historyQueue.ids[1:]
		r, ok := ix.repos[id]
		if !ok {
			continue
		}
		jobCtx, cancel := context.WithCancel(ctx)
		r.cancelHistory = cancel
		job := historyJob{
			ctx: jobCtx, done: cancel, id: id, root: r.root, generation: r.historyGeneration,
			full: r.historyFull || r.history == nil, opts: historyOptions(ix.settings, time.Now()),
			store: r.history, path: historyPath(ix.settings.Location, r.root),
		}
		if job.full {
			r.historyState, r.historyProgress, r.historyMessage = protocol.IndexStateIndexing, 0, ""
		}
		return job, true
	}
	return historyJob{}, false
}

// readHistory reads or updates one repo's history, publishing the newest
// commits as soon as they are read, then saves it.
func (ix *Indexer) readHistory(job historyJob) {
	defer job.done()
	ix.publishStatus()
	var store *history.Store
	var err error
	if job.full {
		store, err = history.Ingest(job.ctx, job.root.Path, job.opts, func(progress float64) {
			ix.updateHistory(job, func(r *repo) { r.historyProgress = progress })
			ix.publishStatus()
		}, func(partial *history.Store) {
			ix.updateHistory(job, func(r *repo) { r.history = partial })
		})
	} else {
		store, err = history.Update(job.ctx, job.root.Path, job.store, job.opts)
	}
	if job.ctx.Err() != nil {
		return // superseded, dropped or shutting down
	}
	var saveErr error
	if err == nil && (job.full || store.Head != job.store.Head) {
		saveErr = store.Save(job.path)
	}
	ix.updateHistory(job, func(r *repo) {
		r.cancelHistory, r.historyFull = nil, false
		switch {
		case errors.Is(err, history.ErrNotGit):
			r.history, r.historyState = nil, protocol.IndexStateOff
			r.historyMessage = "Not a Git repository: history search is off for this folder."
		case err != nil:
			r.historyState, r.historyMessage = protocol.IndexStateError, "History not read: "+err.Error()
		default:
			r.history, r.historyState, r.historyMessage = store, protocol.IndexStateReady, ""
			if saveErr != nil {
				r.historyMessage = "History index not saved: " + saveErr.Error()
			}
		}
	})
	ix.publishStatus()
}

// updateHistory applies change to the job's repo if the job is still current.
func (ix *Indexer) updateHistory(job historyJob, change func(*repo)) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	r, ok := ix.repos[job.id]
	if !ok || r.historyGeneration != job.generation || r.root.Path != job.root.Path {
		return
	}
	change(r)
}

// historyOptions are what the settings say a history index keeps: the
// index.historyDepth window, and no changed lines of the files
// index.exclude leaves out of the working-tree index.
func historyOptions(settings protocol.Settings, now time.Time) history.Options {
	return history.Options{Since: depthStart(settings.HistoryDepth, now), Skip: trigram.NewExcluder(settings.Exclude).Excludes}
}

// depthStart is where the index.historyDepth window starts: "6m" and "2y"
// count back from now, and "all" (or anything else) reads everything.
func depthStart(depth string, now time.Time) time.Time {
	switch depth {
	case "6m":
		return now.AddDate(0, -6, 0)
	case "2y":
		return now.AddDate(-2, 0, 0)
	default:
		return time.Time{}
	}
}
