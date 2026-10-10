package server

import (
	"slices"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// resolver gives the parser and completions the open repos, their
// indexed files, and their histories' authors and message words. open are
// the absolute paths of the files open in the editor, for is: suggestions.
func (s *Server) resolver(open []string) query.Resolver {
	return workspaceResolver{server: s, open: open}
}

// workspaceResolver answers the parser's lookups from the server's state.
type workspaceResolver struct {
	server *Server
	open   []string
}

// messageWordsWindow is how far back msg: suggestions look: the words of
// recent commits are the ones people search for.
const messageWordsWindow = 90 * 24 * time.Hour

// Authors returns the authors of every open repo's history whose name or
// email contains fragment, most commits first.
func (r workspaceResolver) Authors(fragment string, limit int) []query.AuthorStat {
	fragment = strings.ToLower(fragment)
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
	fileCounts, unowned := map[string]int{}, map[string]int{}
	codeOwners := map[string]*codeowners.File{}
	for _, repo := range r.server.index.Repos() {
		fileCounts[repo.ID] = len(repo.Shard.Docs)
		if repo.Owners == nil {
			continue
		}
		codeOwners[repo.ID] = repo.Owners
		unowned[repo.ID] = r.server.ownership.of(&repo).unowned
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
			Git: status.History != protocol.IndexStateOff,
		}
		if owners := codeOwners[root.ID]; owners != nil {
			repos[i].CodeOwners, repos[i].Unowned = owners.Path, unowned[root.ID]
		}
	}
	return repos
}

// Owners counts the files each owner has in the published working-tree
// indexes, by their repos' CODEOWNERS files, most files first. An owner is
// one however its CODEOWNERS files spell its case.
func (r workspaceResolver) Owners() []query.OwnerStat {
	byName := map[string]*query.OwnerStat{}
	var order []*query.OwnerStat
	for _, repo := range r.server.index.Repos() {
		for _, owner := range r.server.ownership.of(&repo).owners {
			key := strings.ToLower(owner.name)
			stat, ok := byName[key]
			if !ok {
				stat = &query.OwnerStat{Name: owner.name}
				byName[key] = stat
				order = append(order, stat)
			}
			stat.Files += owner.files
			stat.Repos = append(stat.Repos, repo.Name)
		}
	}
	owners := make([]query.OwnerStat, len(order))
	for i, stat := range order {
		owners[i] = *stat
	}
	slices.SortStableFunc(owners, func(a, b query.OwnerStat) int { return b.Files - a.Files })
	return owners
}

// Files lists every current file of the published working-tree indexes
// (saved since the index was built included, deleted ones left out), when
// each last changed as since: sees it, and which of them are open in the
// editor, have uncommitted changes, were written by a tool or are
// vendored.
func (r workspaceResolver) Files() []query.FileStat {
	settings := r.server.Settings()
	kinds := lang.NewFileKinds(settings.VendoredFolders, settings.GeneratedSuffixes)
	var files []query.FileStat
	for _, repo := range r.server.index.Repos() {
		open := trigram.OpenIn(repo.Root, r.open)
		for _, doc := range repo.CurrentDocs() {
			lastChange, uncommitted := repo.LastChanged(doc)
			files = append(files, query.FileStat{
				Repo: repo.Name, Path: doc.Path, Lang: doc.Lang, LastChange: lastChange,
				Open: open[doc.Path], Changed: uncommitted,
				Generated: kinds.IsGenerated(doc.Path, doc.Content), Vendored: kinds.IsVendored(doc.Path),
			})
		}
	}
	return files
}

// Tags merges the tags of the open repos by name, newest first, for at:
// suggestions. Each repo's list is read from Git at most once every
// tagsTTL (see tagCache).
func (r workspaceResolver) Tags() []query.TagStat {
	byName := map[string]*query.TagStat{}
	var order []*query.TagStat
	roots := r.server.Roots()
	paths := make([]string, len(roots))
	for i, root := range roots {
		paths[i] = root.Path
	}
	for i, tags := range r.server.tags.lists(paths) {
		root := roots[i]
		for _, tag := range tags {
			stat, ok := byName[tag.Name]
			if !ok {
				stat = &query.TagStat{Name: tag.Name, At: tag.At}
				byName[tag.Name] = stat
				order = append(order, stat)
			}
			stat.Repos = append(stat.Repos, root.Name)
		}
	}
	tags := make([]query.TagStat, len(order))
	for i, stat := range order {
		tags[i] = *stat
	}
	slices.SortStableFunc(tags, func(a, b query.TagStat) int { return b.At.Compare(a.At) })
	return tags
}
