package server

// index/status, index/rebuild and the index/progress notification.

import (
	"context"
	"encoding/json"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
)

// registerIndex registers the index/* handlers.
func (s *Server) registerIndex() {
	s.handle(protocol.MethodIndexStatus, s.indexStatus)
	s.handle(protocol.MethodIndexRebuild, s.rebuild)
}

// notifyIndexStatus sends index/progress.
func (s *Server) notifyIndexStatus(repos []protocol.RepoStatus) {
	_ = s.conn.Notify(protocol.MethodIndexProgress, protocol.IndexStatusResult{Repos: repos})
}

// indexStatus answers index/status: every root's index state.
func (s *Server) indexStatus(context.Context, json.RawMessage) (any, error) {
	return protocol.IndexStatusResult{Repos: s.index.Status()}, nil
}

// rebuild answers index/rebuild: it rebuilds one root's index, or every
// root's when no repo ID is given.
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
