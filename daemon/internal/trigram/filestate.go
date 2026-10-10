package trigram

import (
	"path/filepath"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// WithOpenFiles returns repos with Open set from open, the absolute paths
// of the files open in the editor. repos itself is left as it is.
func WithOpenFiles(repos []Repo, open []string) []Repo {
	out := make([]Repo, len(repos))
	for i, repo := range repos {
		repo.Open = OpenIn(repo.Root, open)
		out[i] = repo
	}
	return out
}

// OpenIn returns the paths in open that lie inside root, relative to it and
// slash-separated, as the index names files. It is nil when none do.
func OpenIn(root string, open []string) map[string]bool {
	var inside map[string]bool
	for _, path := range open {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if inside == nil {
			inside = map[string]bool{}
		}
		inside[filepath.ToSlash(rel)] = true
	}
	return inside
}

// inState reports whether a file is in the state an is: value names: open
// in the editor, with uncommitted changes (never outside Git), holding
// tests, written by a tool, or vendored.
func (r *Repo) inState(doc *Doc, state string) bool {
	switch state {
	case query.StateOpen:
		return r.Open[doc.Path]
	case query.StateChanged:
		return r.History != nil && r.History.Dirty(doc.Path)
	case query.StateTest:
		return lang.IsTest(doc.Path)
	case query.StateGenerated:
		return lang.IsGenerated(doc.Path, doc.Content)
	case query.StateVendored:
		return lang.IsVendored(doc.Path)
	default:
		return false
	}
}

// ownersOf returns who the repo's CODEOWNERS file says owns the file at
// path, for its result's badge; nil when nobody does or there's no file.
func (r *Repo) ownersOf(path string) []string {
	if r.Owners == nil {
		return nil
	}
	return r.Owners.Owners(path)
}

// ownedBy reports whether the repo's CODEOWNERS file gives the file at path
// an owner that an owner: value names (see codeowners.SameOwner), or, for
// owner:none, no owner at all. It is false in a repo without the file.
func (r *Repo) ownedBy(path, name string) bool {
	if r.Owners == nil {
		return false
	}
	owners := r.ownersOf(path)
	if strings.EqualFold(name, query.OwnerNone) {
		return len(owners) == 0
	}
	for _, owner := range owners {
		if codeowners.SameOwner(owner, name) {
			return true
		}
	}
	return false
}
