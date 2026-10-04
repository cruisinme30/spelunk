package server

// preview/get and open/resolve: what a selected result looks like, and
// where opening it goes.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cruisinme30/unified-search/daemon/internal/history"
	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

// preview answers preview/get: the lines around a result, with its matches
// marked, or a commit's diff.
func (s *Server) preview(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.PreviewParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	if ref, ok := history.ParseRef(params.Ref); ok {
		repo, err := s.historyRepoFor(ref)
		if err != nil {
			return nil, err
		}
		preview, err := history.Preview(ctx, repo, ref, s.plans.recall(ref.PlanID), params.ContextLines)
		return preview, staleAsRPCError(err)
	}
	repo, ref, err := s.repoForRef(params.Ref)
	if err != nil {
		return nil, err
	}
	preview, err := trigram.Preview(repo, ref, s.plans.recall(ref.PlanID), params.ContextLines)
	return preview, staleAsRPCError(err)
}

// openResolve answers open/resolve: the file, line and column that opening
// a result goes to, or the commit.
func (s *Server) openResolve(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.OpenResolveParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	if ref, ok := history.ParseRef(params.Ref); ok {
		repo, err := s.historyRepoFor(ref)
		if err != nil {
			return nil, err
		}
		target, err := history.OpenTarget(ctx, repo, ref)
		return target, staleAsRPCError(err)
	}
	repo, ref, err := s.repoForRef(params.Ref)
	if err != nil {
		return nil, err
	}
	target, err := trigram.OpenTarget(repo, ref)
	return target, staleAsRPCError(err)
}

// repoForRef decodes a ref and finds the open root it points into. A ref
// that is malformed or names a root that is no longer open is stale.
func (s *Server) repoForRef(text string) (*trigram.Repo, trigram.Ref, error) {
	ref, ok := trigram.ParseRef(text)
	if !ok {
		return nil, trigram.Ref{}, rpc.Errorf(protocol.CodeRefStale, "malformed ref")
	}
	for _, root := range s.Roots() {
		if root.ID == ref.RepoID {
			return &trigram.Repo{ID: root.ID, Name: root.Name, Root: root.Path}, ref, nil
		}
	}
	return nil, trigram.Ref{}, rpc.Errorf(protocol.CodeRefStale, "repo no longer open")
}

// historyRepoFor finds the open root a commit ref points into.
func (s *Server) historyRepoFor(ref history.Ref) (*history.Repo, error) {
	for _, root := range s.Roots() {
		if root.ID == ref.RepoID {
			return &history.Repo{ID: root.ID, Name: root.Name, Root: root.Path}, nil
		}
	}
	return nil, rpc.Errorf(protocol.CodeRefStale, "repo no longer open")
}

// staleAsRPCError reports a result that no longer exists with the RefStale
// code, so the client can tell it from a failure.
func staleAsRPCError(err error) error {
	if errors.Is(err, trigram.ErrStale) || errors.Is(err, history.ErrStale) {
		return rpc.Errorf(protocol.CodeRefStale, "%v", err)
	}
	return err
}
