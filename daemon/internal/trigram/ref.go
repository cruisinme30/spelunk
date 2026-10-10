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
	// EndLine and EndColumn are where a match that spans lines ends (Length
	// is then the rest of line Line); EndLine is 0 for any other result.
	EndLine   int
	EndColumn int
}

// refPrefix starts every working-tree ref; history refs use another.
const refPrefix = "tree"

// IsFile reports whether the ref is a file-name result.
func (r Ref) IsFile() bool { return r.Line == 0 }

// Spans reports whether the ref's match runs onto later lines.
func (r Ref) Spans() bool { return r.EndLine > r.Line }

// String encodes the ref; the path goes last because it may contain "|".
func (r Ref) String() string {
	return strings.Join([]string{
		refPrefix, strconv.Itoa(r.PlanID), r.RepoID,
		strconv.Itoa(r.Line), strconv.Itoa(r.Column), strconv.Itoa(r.Length),
		strconv.Itoa(r.EndLine), strconv.Itoa(r.EndColumn), r.Path,
	}, "|")
}

// ParseRef decodes a ref built by String. ok is false for anything else.
func ParseRef(text string) (ref Ref, ok bool) {
	parts := strings.SplitN(text, "|", 9)
	if len(parts) != 9 || parts[0] != refPrefix || parts[2] == "" || parts[8] == "" {
		return Ref{}, false
	}
	numbers := make([]int, 0, 6)
	for _, field := range []string{parts[1], parts[3], parts[4], parts[5], parts[6], parts[7]} {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return Ref{}, false
		}
		numbers = append(numbers, n)
	}
	// A ref names a file inside its repo; "../" or an absolute path would
	// let a crafted ref read any file on disk.
	if !filepath.IsLocal(filepath.FromSlash(parts[8])) {
		return Ref{}, false
	}
	return Ref{
		PlanID: numbers[0], RepoID: parts[2], Line: numbers[1],
		Column: numbers[2], Length: numbers[3], EndLine: numbers[4], EndColumn: numbers[5], Path: parts[8],
	}, true
}
