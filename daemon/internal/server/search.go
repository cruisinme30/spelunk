package server

// search/start: plans the query and streams the engine's results as
// search/batch notifications.

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

// batchSize is the most items one search/batch carries (Contract 3).
const batchSize = 200

func (s *Server) registerSearch() {
	s.conn.Handle(protocol.MethodSearchStart, s.search)
	s.conn.Handle(protocol.MethodPreviewGet, s.preview)
	s.conn.Handle(protocol.MethodOpenResolve, s.resolve)
}

// batcher sends results as search/batch notifications of at most batchSize.
type batcher struct {
	conn     *rpc.Conn
	searchID string
	items    []protocol.ResultItem
}

func (b *batcher) add(item protocol.ResultItem) {
	b.items = append(b.items, item)
	if len(b.items) >= batchSize {
		b.flush()
	}
}

func (b *batcher) flush() {
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
	if plan.Mode == protocol.ModeHistory {
		// The history engine comes with the commit index; until then a
		// history query has no results rather than wrong ones.
		return protocol.SearchResult{Ms: int(time.Since(started).Milliseconds())}, nil
	}

	planID := s.plans.remember(plan)
	batch := &batcher{conn: s.conn, searchID: params.SearchID}
	stats, err := trigram.Search(ctx, plan, s.index.Repos(), planID, batch.add)
	if err != nil {
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
