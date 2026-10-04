package server

// preview/get and open/resolve: what a selected result looks like, and
// where opening it goes.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

func (s *Server) preview(_ context.Context, raw json.RawMessage) (any, error) {
	var params protocol.PreviewParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	repo, ref, err := s.lookUp(params.Ref)
	if err != nil {
		return nil, err
	}
	preview, err := trigram.Preview(repo, ref, s.plans.recall(ref.PlanID), params.ContextLines)
	return preview, staleAsRPCError(err)
}

func (s *Server) resolve(_ context.Context, raw json.RawMessage) (any, error) {
	var params protocol.OpenResolveParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	repo, ref, err := s.lookUp(params.Ref)
	if err != nil {
		return nil, err
	}
	target, err := trigram.Resolve(repo, ref)
	return target, staleAsRPCError(err)
}

// lookUp decodes a ref and finds the open root it points into.
func (s *Server) lookUp(text string) (*trigram.Repo, trigram.Ref, error) {
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

func staleAsRPCError(err error) error {
	if errors.Is(err, trigram.ErrStale) {
		return rpc.Errorf(protocol.CodeRefStale, "%v", err)
	}
	return err
}
