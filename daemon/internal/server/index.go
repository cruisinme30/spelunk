package server

// index/status and index/rebuild, and keeping the indexer in step with
// the workspace roots and settings.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

// shutdownGrace is how long shutdown waits for index work to stop
// (Contract 3 lifecycle: flush within 2 seconds).
const shutdownGrace = 2 * time.Second

func (s *Server) registerIndex() {
	s.conn.Handle(protocol.MethodIndexStatus, s.indexStatus)
	s.conn.Handle(protocol.MethodIndexRebuild, s.rebuild)
}

// notifyIndexStatus sends index/progress.
func (s *Server) notifyIndexStatus(repos []protocol.RepoStatus) {
	_ = s.conn.Notify(protocol.MethodIndexProgress, protocol.IndexStatusResult{Repos: repos})
}

func (s *Server) indexStatus(context.Context, json.RawMessage) (any, error) {
	return protocol.IndexStatusResult{Repos: s.index.Status()}, nil
}

func (s *Server) rebuild(_ context.Context, raw json.RawMessage) (any, error) {
	var params protocol.RebuildParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	if err := s.index.Rebuild(params.RepoID); err != nil {
		return nil, rpc.Errorf(rpc.CodeInvalidParams, "%v", err)
	}
	return protocol.Empty{}, nil
}

func (s *Server) onRootsChanged()    { s.index.SetRoots(s.Roots()) }
func (s *Server) onSettingsChanged() { s.index.SetSettings(s.Settings()) }
func (s *Server) onShutdown()        { s.index.Close(shutdownGrace) }
