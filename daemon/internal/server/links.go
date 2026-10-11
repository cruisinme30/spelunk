package server

// repo/remotes and link/resolve: a repo's Git remotes, and what a
// permalink to a result names, for the result menu's link items.

import (
	"context"
	"encoding/json"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/permalink"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
)

// registerLinks registers the handlers behind the result menu's links.
func (s *Server) registerLinks() {
	s.handle(protocol.MethodRepoRemotes, s.repoRemotes)
	s.handle(protocol.MethodLinkResolve, s.linkResolve)
}

// repoRemotes answers repo/remotes: an open repo's remotes and the one
// links go to.
func (s *Server) repoRemotes(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.RepoRemotesParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	for _, root := range s.Roots() {
		if root.ID == params.RepoID {
			return permalink.Remotes(ctx, root.Path)
		}
	}
	return nil, rpc.Errorf(protocol.CodeRefStale, "repo no longer open")
}

// linkResolve answers link/resolve: the remote, commit, path and lines a
// permalink to a result names, or why it has none.
func (s *Server) linkResolve(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.LinkParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	if ref, ok := history.ParseRef(params.Ref); ok {
		repo, err := s.historyRepoFor(ref)
		if err != nil {
			return nil, err
		}
		return permalink.ForCommit(ctx, repo.Root, ref.SHA, params.Remote)
	}
	// repoForRef keeps the path inside its folder, links and all.
	repo, ref, err := s.repoForRef(params.Ref)
	if err != nil {
		return nil, err
	}
	file := permalink.File{
		Root: repo.Root, Path: ref.Path, Line: ref.Line, EndLine: ref.EndLine,
		Revision: ref.Revision, Remote: params.Remote,
	}
	return permalink.ForFile(ctx, file)
}
