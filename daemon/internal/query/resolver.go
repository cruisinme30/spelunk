package query

import (
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// Resolver answers the only lookups the parser and completions may make:
// who the authors are, what the repos are, and which files the index holds.
// Everything else is pure. The server implements it.
type Resolver interface {
	// Authors returns up to limit authors whose name or email contains
	// fragment (case-insensitive), most commits first.
	Authors(fragment string, limit int) []AuthorStat
	// Repos returns the open repos, in workspace order.
	Repos() []RepoStat
	// Files returns every file in the working-tree index of the open repos.
	// Value suggestions summarize them: file types and folders for f:,
	// languages for lang:, and how many files changed in each since: window.
	Files() []FileStat
	// MessageWords returns up to limit words and two-word phrases of recent
	// commit subjects that start with fragment (lowercase), most used first.
	MessageWords(fragment string, limit int) []WordStat
}

// WordStat is a word or phrase of commit subjects, and how much it is used.
type WordStat struct {
	Text    string // lowercase; a phrase has a space
	Commits int
	LastAt  time.Time
}

// AuthorStat is one author identity (after .mailmap merging) with commit stats.
type AuthorStat struct {
	Name    string
	Emails  []string
	Commits int
	Repos   []string
	LastAt  string // RFC 3339 time of the newest commit
}

// RepoStat is one open repo, as repo: suggestions describe it.
type RepoStat struct {
	Name  string // display name, the value repo: matches
	Path  string // absolute path of the workspace folder
	Files int    // files in its working-tree index
	State protocol.IndexState
	// Progress is how far indexing has got, 0 to 1, while State is indexing.
	Progress float64
}

// FileStat is one indexed file.
type FileStat struct {
	Repo    string // the repo's display name
	Path    string // slash-separated, relative to the repo root
	Lang    string // canonical language name, "" if unknown
	ModTime time.Time
}

// noResolver knows no authors, repos or files.
type noResolver struct{}

// Authors finds no authors.
func (noResolver) Authors(string, int) []AuthorStat { return nil }

// Repos knows no repos.
func (noResolver) Repos() []RepoStat { return nil }

// Files knows no files.
func (noResolver) Files() []FileStat { return nil }

// MessageWords knows no commits.
func (noResolver) MessageWords(string, int) []WordStat { return nil }

// repoNames lists the display names of the open repos.
func repoNames(resolver Resolver) []string {
	repos := resolver.Repos()
	names := make([]string, len(repos))
	for i, repo := range repos {
		names[i] = repo.Name
	}
	return names
}
