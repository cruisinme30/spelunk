package server

// search/start: plans the query and streams the engine's results as
// search/batch notifications.

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

const (
	// batchSize is the most items one search/batch notification carries.
	batchSize = 200
	// batchDelay is the longest a result waits for its batch to fill, so the
	// first results show while a long search continues.
	batchDelay = 20 * time.Millisecond
)

// registerSearch registers the handlers for searching and for what the
// client does with results: preview, open and replace.
func (s *Server) registerSearch() {
	s.handle(protocol.MethodSearchStart, s.search)
	s.handle(protocol.MethodPreviewGet, s.preview)
	s.handle(protocol.MethodOpenResolve, s.openResolve)
	s.handle(protocol.MethodReplacePlan, s.replacePlan)
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

// add queues item, sending the batch once it is full.
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

// flushLocked sends what is waiting. Callers hold mu.
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

// search answers search/start: it plans the query, streams the results as
// search/batch notifications, and answers with the totals and the cursor
// of the next page.
func (s *Server) search(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.SearchStartParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	started := time.Now() // wall clock: Options.Now may be frozen
	parsed := query.Parse(params.Text, s.resolver(params.OpenFiles))
	plan, _, err := query.NewPlan(parsed, s.Settings(), s.now(), params.Cursor)
	if err != nil {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "%v", err)
	}
	planID := s.plans.remember(plan)
	batch := &batcher{conn: s.conn, searchID: params.SearchID}
	repos := trigram.WithOpenFiles(s.index.Repos(), params.OpenFiles)
	var stats engine.Stats
	if plan.Mode == protocol.ModeHistory {
		stats, err = history.Search(ctx, plan, s.index.HistoryRepos(), planID, batch.add)
	} else {
		stats, err = trigram.Search(ctx, plan, repos, planID, batch.add)
	}
	if err != nil {
		batch.discard() // no batches may follow the error response
		return nil, err
	}
	batch.flush()
	if plan.SymbolFilter != nil {
		if note, ok := s.symbolsAsText(ctx, params.Text, repos, plan.SymbolFilter); ok {
			stats.Hidden = append(stats.Hidden, note)
		}
	}
	result := protocol.SearchResult{
		Total: stats.Total, Truncated: stats.Truncated, Hidden: stats.Hidden,
		Ms: int(time.Since(started).Milliseconds()),
	}
	if stats.NextOffset > 0 {
		result.NextCursor = strconv.Itoa(stats.NextOffset)
	}
	return result, nil
}

// symbolsAsText counts what the query finds with its sym: names searched as
// text instead, for the "Search RetryPolicy as text" suggestion: the count
// is what running the suggestion returns. repos are the search's own, with
// the files open in the editor.
func (s *Server) symbolsAsText(ctx context.Context, text string, repos []trigram.Repo, filter *query.Filter) (protocol.HiddenNote, bool) {
	parsed := query.Parse(query.ApplyFix(text, filter.Undo), s.resolver(nil))
	plan, _, err := query.NewPlan(parsed, s.Settings(), s.now(), "")
	if err != nil || plan.Mode != protocol.ModeWorkingTree {
		return protocol.HiddenNote{}, false
	}
	plan.Limit = 0 // count only
	stats, err := trigram.Search(ctx, plan, repos, 0, func(protocol.ResultItem) {})
	if err != nil {
		return protocol.HiddenNote{}, false
	}
	return filter.Note(stats.Total, "matches"), true
}
