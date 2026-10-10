package query

import (
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
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
	// Symbols returns up to limit definition names that contain fragment
	// (lowercase), best first.
	Symbols(fragment string, limit int) []SymbolStat
	// Owners returns every owner the open repos' CODEOWNERS files give
	// files to, most files first.
	Owners() []OwnerStat
	// Tags returns the open repos' tags, newest first, for at:.
	Tags() []TagStat
}

// TagStat is a tag in one or more open repos, and when it was made (in
// the first repo that has it).
type TagStat struct {
	Name  string
	At    time.Time
	Repos []string
}

// OwnerStat is one owner in CODEOWNERS files, as it is written there
// (@acme/payments-core, @jane, jane@example.com), and how many indexed
// files it owns.
type OwnerStat struct {
	Name  string
	Files int
	Repos []string
}

// SymbolStat is one definition name and where it is defined.
type SymbolStat struct {
	Name string
	// Kind is the kind of its first definition: a name is usually one kind.
	Kind        protocol.SymbolKind
	Definitions int
	Repos       []string
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
	// Git is false for a folder outside Git, whose files is:changed never matches.
	Git bool
	// CodeOwners is where its CODEOWNERS file is (.github/CODEOWNERS), or ""
	// when it has none, so owner: matches none of its files.
	CodeOwners string
	// Unowned is how many of its indexed files CODEOWNERS gives no owner (owner:none).
	Unowned int
}

// FileStat is one indexed file.
type FileStat struct {
	Repo string // the repo's display name
	Path string // slash-separated, relative to the repo root
	Lang string // canonical language name, "" if unknown
	// LastChange is when the file last changed, as since: and until: see
	// it: in Git its newest commit (the zero time when the history window
	// has none), elsewhere its modification time. A file with uncommitted
	// changes (Changed) changed now, whatever it says.
	LastChange time.Time
	Open       bool // open in an editor tab (is:open)
	Changed    bool // has uncommitted changes (is:changed)
	// Generated says a tool wrote the file (is:generated), by its name or a
	// header only the index has read; Vendored that it is someone else's
	// code (is:vendored). Both go by the index settings that say which
	// files are which.
	Generated, Vendored bool
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

// Symbols knows no definitions.
func (noResolver) Symbols(string, int) []SymbolStat { return nil }

// Owners knows no CODEOWNERS files.
func (noResolver) Owners() []OwnerStat { return nil }

// Tags returns no tags.
func (noResolver) Tags() []TagStat { return nil }

// repoNames lists the display names of the open repos.
func repoNames(resolver Resolver) []string {
	repos := resolver.Repos()
	names := make([]string, len(repos))
	for i, repo := range repos {
		names[i] = repo.Name
	}
	return names
}
