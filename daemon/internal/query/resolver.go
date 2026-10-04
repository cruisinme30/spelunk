package query

// Resolver answers the only lookups the parser may make: who the authors
// are and what the repos are called. Everything else is pure. The history
// engine and the server implement it.
type Resolver interface {
	// Authors returns up to limit authors whose name or email contains
	// fragment (case-insensitive), most commits first.
	Authors(fragment string, limit int) []AuthorStat
	// RepoNames returns the display names of the open repos.
	RepoNames() []string
}

// AuthorStat is one author identity (after .mailmap merging) with commit stats.
type AuthorStat struct {
	Name    string
	Emails  []string
	Commits int
	Repos   []string
	LastAt  string // RFC 3339 time of the newest commit
}

// noResolver knows no authors or repos.
type noResolver struct{}

func (noResolver) Authors(string, int) []AuthorStat { return nil }
func (noResolver) RepoNames() []string              { return nil }
