package trigram

import (
	"path/filepath"
	"strings"

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

// inState reports whether the file at path is in the state an is: value
// names: open in the editor, with uncommitted changes (never outside Git),
// or holding tests.
func (r *Repo) inState(path, state string) bool {
	switch state {
	case query.StateOpen:
		return r.Open[path]
	case query.StateChanged:
		return r.History != nil && r.History.Dirty(path)
	case query.StateTest:
		return lang.IsTest(path)
	default:
		return false
	}
}
