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
	parsed := query.Parse(params.Text, resolver)
	// A query without errors is also planned, so warnings only the planner
	// can give (such as a history regex that scans every commit) show too.
	if _, warnings, err := query.NewPlan(parsed, s.Settings(), s.now(), ""); err == nil {
		parsed.Diagnostics = append(parsed.Diagnostics, warnings...)
	}
	return protocol.ParseResult{
		Query:       parsed,
		Completions: query.Complete(params.Text, params.Cursor, resolver, s.now()),
	}, nil
}

// resolver gives the parser and completions the open repos and their
// indexed files. It knows no authors: history search is not built yet.
func (s *Server) resolver() query.Resolver {
	return workspaceResolver{server: s}
}

// workspaceResolver answers the parser's lookups from the server's state.
type workspaceResolver struct{ server *Server }

// Authors returns nothing: history search is not built yet.
func (workspaceResolver) Authors(string, int) []query.AuthorStat { return nil }

// Repos describes the open workspace roots with their index state.
func (r workspaceResolver) Repos() []query.RepoStat {
	fileCounts := map[string]int{}
	for _, repo := range r.server.index.Repos() {
		fileCounts[repo.ID] = len(repo.Shard.Docs)
	}
	statuses := map[string]protocol.RepoStatus{}
	for _, status := range r.server.index.Status() {
		statuses[status.RepoID] = status
	}
	roots := r.server.Roots()
	repos := make([]query.RepoStat, len(roots))
	for i, root := range roots {
		status := statuses[root.ID]
		repos[i] = query.RepoStat{
			Name: root.Name, Path: root.Path, Files: fileCounts[root.ID], State: status.Tree, Progress: status.Progress,
		}
	}
	return repos
}

// Files lists every file in the published working-tree indexes.
func (r workspaceResolver) Files() []query.FileStat {
	var files []query.FileStat
	for _, repo := range r.server.index.Repos() {
		for i := range repo.Shard.Docs {
			doc := &repo.Shard.Docs[i]
			files = append(files, query.FileStat{Repo: repo.Name, Path: doc.Path, Lang: doc.Lang, ModTime: doc.ModTime})
		}
	}
	return files
}
