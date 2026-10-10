package server

// workspace/setRoots and workspace/didChangeFiles make the indexer read
// saved indexes, files and Git, which takes from milliseconds to seconds.
// Notifications run on the connection's read loop, so doing that work
// there held up every request behind it, $/cancelRequest included: twenty
// saves in a large repo kept a search waiting five seconds. The handlers
// only queue the event, and one goroutine applies the queue in order.

import (
	"context"
	"slices"
	"sync"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// indexEvent is one queued notification: the new workspace roots, or the
// files the editor saw change.
type indexEvent struct {
	roots   []protocol.Root
	isRoots bool
	changes []protocol.FileChange
}

// indexEvents is the queue of notifications for the indexer, and its
// worker. Safe for concurrent use.
type indexEvents struct {
	mu      sync.Mutex
	pending []indexEvent
	wake    chan struct{}
	// beforeApply, when tests set it, runs before each batch of events is
	// applied, which waits for it to return.
	beforeApply func(batch []indexEvent)
}

// add queues an event and wakes the worker.
func (q *indexEvents) add(event indexEvent) {
	q.mu.Lock()
	q.pending = append(q.pending, event)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// take returns the queued events, coalesced (see coalesce), and empties
// the queue.
func (q *indexEvents) take() []indexEvent {
	q.mu.Lock()
	defer q.mu.Unlock()
	batch := coalesce(q.pending)
	q.pending = nil
	return batch
}

// coalesce joins events that can be applied as one, keeping their order:
// file changes that follow each other become one list, so the indexer
// re-reads them and asks Git what is uncommitted once, and of roots that
// follow each other only the last counts.
func coalesce(events []indexEvent) []indexEvent {
	var batch []indexEvent
	for _, event := range events {
		if n := len(batch); n > 0 && batch[n-1].isRoots == event.isRoots {
			if event.isRoots {
				batch[n-1] = event
			} else {
				batch[n-1].changes = append(slices.Clip(batch[n-1].changes), event.changes...)
			}
			continue
		}
		batch = append(batch, event)
	}
	return batch
}

// applyIndexEvents applies queued events until ctx ends.
func (s *Server) applyIndexEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.events.wake:
		}
		batch := s.events.take()
		if len(batch) == 0 {
			continue
		}
		if hook := s.events.beforeApply; hook != nil {
			hook(batch)
		}
		for _, event := range batch {
			if ctx.Err() != nil {
				return
			}
			s.applyIndexEvent(event)
		}
	}
}

// applyIndexEvent applies one event. Like a notification handler on the
// read loop, a panic drops the event rather than taking the daemon down.
func (s *Server) applyIndexEvent(event indexEvent) {
	defer func() { _ = recover() }()
	if event.isRoots {
		s.applyRoots(event.roots)
	} else {
		s.index.FilesChanged(event.changes)
	}
}
