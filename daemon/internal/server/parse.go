package server

import (
	"context"
	"encoding/json"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
)

// parse answers query/parse: the parsed query for chips and diagnostics,
// plus completions at the cursor.
func (s *Server) parse(_ context.Context, raw json.RawMessage) (any, error) {
	var params protocol.ParseParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	resolver := s.resolver()
	return protocol.ParseResult{
		Query:       query.Parse(params.Text, resolver),
		Completions: query.Complete(params.Text, params.Cursor, resolver, s.now()),
	}, nil
}

// resolver gives the parser the open repos' names and, once history is
// indexed, their authors.
func (s *Server) resolver() query.Resolver {
	return workspaceResolver{server: s}
}

type workspaceResolver struct{ server *Server }

// Authors returns nothing until the history index exists (M3).
func (workspaceResolver) Authors(string, int) []query.AuthorStat { return nil }

func (r workspaceResolver) RepoNames() []string {
	roots := r.server.Roots()
	names := make([]string, len(roots))
	for i, root := range roots {
		names[i] = root.Name
	}
	return names
}
