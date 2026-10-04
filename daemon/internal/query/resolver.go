package query

// Resolver answers the only lookups the parser may make: who the authors
// are and what the repos are called. Everything else is pure. The history
// engine and the server implement it.
type Resolver interface {
	// Authors returns up to limit authors whose name or email contains
	// fragment (case-insensitive), most commits first.
	Authors(fragment string, limit int) []Author
	// RepoNames returns the display names of the open repos.
	RepoNames() []string
}

// Author is one identity after .mailmap merging.
type Author struct {
	Name    string
	Emails  []string
	Commits int
	Repos   []string
	LastAt  string // RFC 3339 time of the newest commit
}

// noResolver knows no authors or repos.
type noResolver struct{}

func (noResolver) Authors(string, int) []Author { return nil }
func (noResolver) RepoNames() []string          { return nil }
