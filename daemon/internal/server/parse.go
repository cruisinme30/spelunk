package server

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

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

// resolver gives the parser and completions the open repos, their
// indexed files, and their histories' authors and message words.
func (s *Server) resolver() query.Resolver {
	return workspaceResolver{server: s}
}

// workspaceResolver answers the parser's lookups from the server's state.
type workspaceResolver struct{ server *Server }

// messageWordsWindow is how far back msg: suggestions look: the words of
// recent commits are the ones people search for.
const messageWordsWindow = 90 * 24 * time.Hour

// Authors returns the authors of every open repo's history whose name or
// email contains fragment, most commits first.
func (r workspaceResolver) Authors(fragment string, limit int) []query.AuthorStat {
	var found []query.AuthorStat
	for _, author := range r.mergedAuthors() {
		if strings.Contains(strings.ToLower(author.Name+" "+strings.Join(author.Emails, " ")), fragment) {
			found = append(found, author)
		}
	}
	slices.SortFunc(found, func(a, b query.AuthorStat) int {
		if a.Commits != b.Commits {
			return b.Commits - a.Commits
		}
		return strings.Compare(a.Name, b.Name)
	})
	return found[:min(len(found), limit)]
}

// mergedAuthors merges the authors of every open repo's history by name.
func (r workspaceResolver) mergedAuthors() []query.AuthorStat {
	byName := map[string]*query.AuthorStat{}
	lastAt := map[string]time.Time{}
	for _, repo := range r.server.index.HistoryRepos() {
		for _, author := range repo.Store.Authors() {
			stat, ok := byName[author.Name]
			if !ok {
				stat = &query.AuthorStat{Name: author.Name}
				byName[author.Name] = stat
			}
			stat.Commits += author.Commits
			stat.Repos = append(stat.Repos, repo.Name)
			for _, email := range author.Emails {
				if !slices.Contains(stat.Emails, email) {
					stat.Emails = append(stat.Emails, email)
				}
			}
			if author.LastAt.After(lastAt[author.Name]) {
				lastAt[author.Name] = author.LastAt
			}
		}
	}
	authors := make([]query.AuthorStat, 0, len(byName))
	for name, stat := range byName {
		stat.LastAt = lastAt[name].UTC().Format(time.RFC3339)
		authors = append(authors, *stat)
	}
	return authors
}

// MessageWords merges the recent subject words of every open repo's history
// and returns those that start with fragment, most used first. A phrase is
// offered only when more than one commit used it.
func (r workspaceResolver) MessageWords(fragment string, limit int) []query.WordStat {
	merged := map[string]query.WordStat{}
	after := r.server.now().Add(-messageWordsWindow)
	for _, repo := range r.server.index.HistoryRepos() {
		for text, count := range repo.Store.Words(after) {
			stat := merged[text]
			stat.Text = text
			stat.Commits += count.Commits
			if count.LastAt.After(stat.LastAt) {
				stat.LastAt = count.LastAt
			}
			merged[text] = stat
		}
	}
	var found []query.WordStat
	for text, stat := range merged {
		if strings.HasPrefix(text, fragment) && (!strings.Contains(text, " ") || stat.Commits > 1) {
			found = append(found, stat)
		}
	}
	slices.SortFunc(found, func(a, b query.WordStat) int {
		if a.Commits != b.Commits {
			return b.Commits - a.Commits
		}
		return strings.Compare(a.Text, b.Text)
	})
	return found[:min(len(found), limit)]
}

// Symbols returns the definition names in the published working-tree
// indexes that contain fragment, exact names first, then most defined.
func (r workspaceResolver) Symbols(fragment string, limit int) []query.SymbolStat {
	found := r.symbolsContaining(fragment)
	slices.SortFunc(found, func(a, b query.SymbolStat) int {
		if aExact, bExact := strings.EqualFold(a.Name, fragment), strings.EqualFold(b.Name, fragment); aExact != bExact {
			if aExact {
				return -1
			}
			return 1
		}
		if a.Definitions != b.Definitions {
			return b.Definitions - a.Definitions
		}
		return strings.Compare(a.Name, b.Name)
	})
	return found[:min(len(found), limit)]
}

// symbolsContaining counts the definitions of each name that contains
// fragment, and the repos that define it.
func (r workspaceResolver) symbolsContaining(fragment string) []query.SymbolStat {
	byName := map[string]*query.SymbolStat{}
	var order []*query.SymbolStat
	for _, repo := range r.server.index.Repos() {
		for i := range repo.Shard.Docs {
			for _, symbol := range repo.Shard.Docs[i].Symbols {
				if !strings.Contains(strings.ToLower(symbol.Name), fragment) {
					continue
				}
				stat, ok := byName[symbol.Name]
				if !ok {
					stat = &query.SymbolStat{Name: symbol.Name, Kind: symbol.Kind}
					byName[symbol.Name] = stat
					order = append(order, stat)
				}
				stat.Definitions++
				if !slices.Contains(stat.Repos, repo.Name) {
					stat.Repos = append(stat.Repos, repo.Name)
				}
			}
		}
	}
	found := make([]query.SymbolStat, len(order))
	for i, stat := range order {
		found[i] = *stat
	}
	return found
}

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
