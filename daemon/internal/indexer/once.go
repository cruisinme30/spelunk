package indexer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// BuildOnce builds and saves root's indexes without a running editor. It uses
// the same settings, saved snapshots and exclusive writer lock as a window.
// With rebuild false, history updates from the saved snapshot; true reads it
// again in full. It returns only after both indexes have been saved, or history
// has been disabled because root is outside Git. Another writer is an error.
func BuildOnce(ctx context.Context, settings protocol.Settings, root protocol.Root, rebuild bool) (protocol.RepoStatus, error) {
	if err := ctx.Err(); err != nil {
		return protocol.RepoStatus{}, err
	}
	ix := newIndexer(ctx, settings, nil, true)
	defer ix.closeWriter()
	ix.SetRoots([]protocol.Root{root})
	// Verify ownership before Rebuild could queue work. SetRoots deliberately
	// leaves a failed strict writer unqueued, even if an old snapshot loaded.
	if _, err := ix.savedWorkDone(root.ID); err != nil {
		return protocol.RepoStatus{}, err
	}
	if rebuild {
		if err := ix.Rebuild(root.ID); err != nil {
			return protocol.RepoStatus{}, err
		}
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return protocol.RepoStatus{}, err
		}
		done, err := ix.savedWorkDone(root.ID)
		if err != nil {
			return protocol.RepoStatus{}, err
		}
		if done {
			return ix.Status()[0], nil
		}
		select {
		case <-ctx.Done():
			return protocol.RepoStatus{}, ctx.Err()
		case <-tick.C:
		}
	}
}

// savedWorkDone checks the queues as well as active workers: a loaded snapshot
// can already be ready while its refresh is still queued or running.
func (ix *Indexer) savedWorkDone(id string) (bool, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	r := ix.repos[id]
	if r.lock == nil {
		return false, ErrNotWriter
	}
	if r.lock.file == nil {
		return false, errWriteLock
	}
	if slices.Contains(ix.treeQueue.ids, id) || slices.Contains(ix.historyQueue.ids, id) || r.cancelBuild != nil || r.cancelHistory != nil {
		return false, nil
	}
	if r.state != protocol.IndexStateReady || r.message != "" {
		return false, fmt.Errorf("working-tree index: %s", r.message)
	}
	if r.historyState != protocol.IndexStateOff && (r.historyState != protocol.IndexStateReady || r.historyMessage != "") {
		return false, fmt.Errorf("history index: %s", r.historyMessage)
	}
	return true, nil
}

// errWriteLock means the one-shot command cannot safely save an index.
var errWriteLock = errors.New("could not acquire the index write lock; check the index location permissions")

// closeWriter drains even a canceled save before releasing its lock. Unlike a
// window's bounded Close, a one-shot command cannot leave unfinished writes
// racing a subsequent invocation after it exits.
func (ix *Indexer) closeWriter() {
	ix.stop()
	ix.workers.Wait()
	ix.releaseLocks()
}
