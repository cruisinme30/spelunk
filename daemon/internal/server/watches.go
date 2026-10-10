package server

// Query watches: after a history update, every watched query that searches
// history runs over the commits the update brought in, and what it matched
// is announced as watch/matched.

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/engine"
	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// registerWatches registers watch/set and listens for new commits.
func (s *Server) registerWatches() {
	s.onNotify(protocol.MethodWatchSet, s.setWatches)
	s.index.OnNewCommits(s.announceWatches)
}

// setWatches handles watch/set.
func (s *Server) setWatches(raw json.RawMessage) {
	var params protocol.WatchSetParams
	if decode(raw, &params) == nil {
		s.applyWatches(params.Queries)
	}
}

// applyWatches stores the watched queries: trimmed, without blanks or repeats.
func (s *Server) applyWatches(queries []string) {
	watches := make([]string, 0, len(queries))
	for _, text := range queries {
		if text = strings.TrimSpace(text); text != "" && !slices.Contains(watches, text) {
			watches = append(watches, text)
		}
	}
	s.mu.Lock()
	s.watches = watches
	s.mu.Unlock()
}

// Watches returns a copy of the watched queries.
func (s *Server) Watches() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.watches)
}

// announceWatches runs each watched query over the commits an update
// brought into repo, leaving out the ones committed under the repo's own
// user.email, and announces each query that matched any.
func (s *Server) announceWatches(repo history.Repo, commits []history.Commit) {
	watches := s.Watches()
	if len(watches) == 0 || s.shuttingDown() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), engine.Budget)
	defer cancel()
	commits = othersCommits(commits, history.UserEmail(ctx, repo.Root))
	if len(commits) == 0 {
		return
	}
	fresh := history.Repo{ID: repo.ID, Name: repo.Name, Root: repo.Root, Store: history.StoreOf(repo.Store.Head, commits)}
	for _, text := range watches {
		if matched, ok := s.watchMatches(text, fresh); ok {
			_ = s.conn.Notify(protocol.MethodWatchMatched, matched)
		}
	}
}

// othersCommits drops the commits whose author email is email (case aside):
// a watch tells you what others did. An empty email keeps them all.
func othersCommits(commits []history.Commit, email string) []history.Commit {
	if email == "" {
		return commits
	}
	return slices.DeleteFunc(slices.Clone(commits), func(c history.Commit) bool {
		return strings.EqualFold(c.AuthorEmail, email)
	})
}

// watchMatches runs one watched query over repo's commits. ok is false when
// the query doesn't search history, has errors, or matched nothing.
// Each query has its own engine.Budget, so slow ones don't starve the rest.
func (s *Server) watchMatches(text string, repo history.Repo) (protocol.WatchMatchedParams, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), engine.Budget)
	defer cancel()
	plan, _, err := query.NewPlan(query.Parse(text, s.resolver(nil)), s.Settings(), s.now(), "")
	if err != nil || plan.Mode != protocol.ModeHistory {
		return protocol.WatchMatchedParams{}, false
	}
	var commits []protocol.WatchCommit
	stats, err := history.Search(ctx, plan, []history.Repo{repo}, 0, func(item protocol.ResultItem) {
		commits = append(commits, protocol.WatchCommit{SHA: item.SHA, Subject: item.Subject, Author: item.Author.Name})
	})
	if err != nil || stats.Total == 0 {
		return protocol.WatchMatchedParams{}, false
	}
	return protocol.WatchMatchedParams{
		Query: text, RepoID: repo.ID, RepoName: repo.Name, Total: stats.Total, Commits: commits,
	}, true
}
