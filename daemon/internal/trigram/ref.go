package trigram

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Ref locates one working-tree result. Its string form is the opaque ref
// handed to clients, which pass it back unchanged (preview/get,
// open/resolve). Only this package builds refs; the server parses them.
type Ref struct {
	// PlanID names the search that produced the result, so a preview can
	// highlight every term of that query while the plan is still known.
	PlanID int
	RepoID string
	Path   string // slash-separated, relative to the repo root
	// Line is 1-based; 0 for a file-name result.
	Line int
	// Column and Length are the first match, in UTF-16 units (Column 0-based).
	Column int
	Length int
}

// refPrefix starts every working-tree ref; history refs use another.
const refPrefix = "tree"

// IsFile reports whether the ref is a file-name result.
func (r Ref) IsFile() bool { return r.Line == 0 }

// String encodes the ref; the path goes last because it may contain "|".
func (r Ref) String() string {
	return strings.Join([]string{
		refPrefix, strconv.Itoa(r.PlanID), r.RepoID,
		strconv.Itoa(r.Line), strconv.Itoa(r.Column), strconv.Itoa(r.Length), r.Path,
	}, "|")
}

// ParseRef decodes a ref built by String. ok is false for anything else.
func ParseRef(text string) (ref Ref, ok bool) {
	parts := strings.SplitN(text, "|", 7)
	if len(parts) != 7 || parts[0] != refPrefix || parts[2] == "" || parts[6] == "" {
		return Ref{}, false
	}
	numbers := make([]int, 0, 4)
	for _, field := range []string{parts[1], parts[3], parts[4], parts[5]} {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return Ref{}, false
		}
		numbers = append(numbers, n)
	}
	// A ref names a file inside its repo; "../" or an absolute path would
	// let a crafted ref read any file on disk.
	if !filepath.IsLocal(filepath.FromSlash(parts[6])) {
		return Ref{}, false
	}
	return Ref{
		PlanID: numbers[0], RepoID: parts[2], Line: numbers[1],
		Column: numbers[2], Length: numbers[3], Path: parts[6],
	}, true
}
