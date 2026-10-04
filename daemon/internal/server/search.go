package server

// search/start: plans the query and streams the engine's results as
// search/batch notifications.

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

const (
	// batchSize is the most items one search/batch notification carries.
	batchSize = 200
	// batchDelay is the longest a result waits for its batch to fill, so the
	// first results show while a long search continues.
	batchDelay = 20 * time.Millisecond
)

func (s *Server) registerSearch() {
	s.conn.Handle(protocol.MethodSearchStart, s.search)
	s.conn.Handle(protocol.MethodPreviewGet, s.preview)
	s.conn.Handle(protocol.MethodOpenResolve, s.resolve)
}

// batcher sends results as search/batch notifications of at most
// batchSize, or sooner when batchDelay passes. Safe for concurrent use.
type batcher struct {
	conn     *rpc.Conn
	searchID string

	mu    sync.Mutex
	items []protocol.ResultItem
	timer *time.Timer // pending flush of a part-filled batch
}

func (b *batcher) add(item protocol.ResultItem) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, item)
	if len(b.items) >= batchSize {
		b.flushLocked()
	} else if b.timer == nil {
		b.timer = time.AfterFunc(batchDelay, b.flush)
	}
}

// flush sends what is waiting. The search calls it last, before it
// answers, so every batch arrives before the search/start result.
func (b *batcher) flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushLocked()
}

// discard drops what is waiting and cancels the pending flush.
func (b *batcher) discard() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.items = nil
}

func (b *batcher) flushLocked() {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if len(b.items) == 0 {
		return
	}
	_ = b.conn.Notify(protocol.MethodSearchBatch, protocol.SearchBatchParams{SearchID: b.searchID, Items: b.items})
	b.items = nil
}

func (s *Server) search(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.SearchStartParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	started := time.Now() // wall clock: Options.Now may be frozen
	parsed := query.Parse(params.Text, s.resolver())
	plan, _, err := query.NewPlan(parsed, s.Settings(), s.now(), params.Cursor)
	if err != nil {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "%v", err)
	}
	if plan.Mode == protocol.ModeHistory || plan.Kinds[query.KindSymbol] {
		// Not built yet: history queries need the commit index and sym: the
		// symbol index. Until then they have no results rather than wrong ones.
		return protocol.SearchResult{Ms: int(time.Since(started).Milliseconds())}, nil
	}

	planID := s.plans.remember(plan)
	batch := &batcher{conn: s.conn, searchID: params.SearchID}
	stats, err := trigram.Search(ctx, plan, s.index.Repos(), planID, batch.add)
	if err != nil {
		batch.discard() // no batches may follow the error response
		return nil, err
	}
	batch.flush()
	result := protocol.SearchResult{
		Total: stats.Total, Truncated: stats.Truncated, Hidden: stats.Hidden,
		Ms: int(time.Since(started).Milliseconds()),
	}
	if stats.NextOffset > 0 {
		result.NextCursor = strconv.Itoa(stats.NextOffset)
	}
	return result, nil
}
