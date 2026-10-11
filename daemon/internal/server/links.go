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

// maxLinks bounds one link/resolve: an export asks for at most this many.
const maxLinks = 10_000

// linkResolve answers link/resolve: for each ref, the remote, commit, path
// and lines a permalink names, or why it has none. One Linker serves them
// all, so the refs of one repo and one file share its git work.
func (s *Server) linkResolve(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.LinkParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	if len(params.Refs) > maxLinks {
		return nil, rpc.Errorf(rpc.CodeInvalidParams, "at most %d refs", maxLinks)
	}
	linker := permalink.NewLinker(ctx, params.Remote)
	links := make([]protocol.Link, 0, len(params.Refs))
	for _, text := range params.Refs {
		link, err := s.linkOne(linker, text)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return protocol.Links{Links: links}, nil
}

// linkOne links one ref with linker.
func (s *Server) linkOne(linker *permalink.Linker, text string) (protocol.Link, error) {
	if ref, ok := history.ParseRef(text); ok {
		repo, err := s.historyRepoFor(ref)
		if err != nil {
			return protocol.Link{}, err
		}
		return linker.Commit(repo.Root, ref.SHA)
	}
	// repoForRef keeps the path inside its folder, links and all.
	repo, ref, err := s.repoForRef(text)
	if err != nil {
		return protocol.Link{}, err
	}
	return linker.File(permalink.File{
		Root: repo.Root, Path: ref.Path, Line: ref.Line, EndLine: ref.EndLine, Revision: ref.Revision,
	})
}
