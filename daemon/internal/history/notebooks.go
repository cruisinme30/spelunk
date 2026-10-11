package history

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cruisinme30/spelunk/daemon/internal/notebook"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// NotebookTextArg is the first argument that makes the daemon's executable
// print the diff text of the notebook file named by the second (see
// ServeNotebookText). Git runs it as the textconv filter of notebooks.
const NotebookTextArg = "notebook-text"

// ServeNotebookText is how an executable answers Git as a notebook's
// textconv filter: given the process's arguments, it prints the diff text
// of the notebook they name and reports true, or reports false when they
// aren't such a call. The daemon and every test binary that reads history
// call it first: Git runs the executable that asked for the diff, and a
// test binary that ran its tests instead would ask again.
func ServeNotebookText(args []string, out io.Writer) bool {
	if len(args) != 3 || args[1] != NotebookTextArg {
		return false
	}
	PrintNotebookText(args[2], out)
	return true
}

// notebookDriver names the diff driver notebooks get.
const notebookDriver = "spelunk-notebook"

// PrintNotebookText writes the diff text of the notebook file at path to
// out: its cells' source (notebook.DiffText). It never fails, since a
// textconv filter that fails stops Git's whole log: a file it can't read
// prints nothing, and one that isn't a notebook prints as it is.
func PrintNotebookText(path string, out io.Writer) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: Git names a temporary copy of a blob
	if err != nil {
		return
	}
	content, ok := trigram.DecodeBlob(raw)
	if !ok {
		_, _ = out.Write(raw)
		return
	}
	_, _ = out.Write(notebook.DiffText(content))
}

// notebookDiff holds the Git options that diff notebooks by their cells'
// source, made once (see notebookDiffOptions).
var notebookDiff struct {
	once    sync.Once
	options []string
}

// notebookDiffOptions are the -c options that make Git diff a notebook by
// its cells' source, through this executable as the textconv filter: an
// attributes file, standing in for the user's global one with the same
// lines plus one for notebooks (a repo's own .gitattributes still wins),
// and the filter. Without them (the executable or a temporary file can't
// be had) notebooks diff as JSON.
func notebookDiffOptions() []string {
	notebookDiff.once.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		attributes, err := os.CreateTemp("", "spelunk-attributes-*")
		if err != nil {
			return
		}
		defer func() { _ = attributes.Close() }()
		lines := userAttributes() + "\n*.[iI][pP][yY][nN][bB] diff=" + notebookDriver + "\n"
		if _, err := attributes.WriteString(lines); err != nil {
			return
		}
		notebookDiff.options = []string{
			"-c", "core.attributesFile=" + attributes.Name(),
			"-c", "diff." + notebookDriver + ".textconv=" + shellQuote(exe) + " " + NotebookTextArg,
		}
	})
	return notebookDiff.options
}

// userAttributes is the text of the user's global attributes file, which
// core.attributesFile names, or Git's default place for it; "" when there
// is none.
func userAttributes() string {
	path := ""
	cmd := exec.CommandContext(context.Background(), "git", "config", "--global", "--path", "--get", "core.attributesFile")
	if out, err := cmd.Output(); err == nil {
		path = strings.TrimSpace(string(out))
	}
	if path == "" {
		config := os.Getenv("XDG_CONFIG_HOME")
		if config == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			config = filepath.Join(home, ".config")
		}
		path = filepath.Join(config, "git", "attributes")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(text)
}

// shellQuote quotes s for the shell Git runs a textconv filter with.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// markNotebookCells sets each line's cell in the hunks of a commit's
// notebooks, from the notebooks as they were before the commit (for a
// removed line) and after it (for the rest). A notebook that can't be read
// at either side leaves its lines without cells.
func markNotebookCells(ctx context.Context, root, sha string, hunks []protocol.Hunk) {
	cells := map[string][2][]notebook.Cell{}
	for h := range hunks {
		path := hunks[h].Path
		if !notebook.IsNotebook(path) {
			continue
		}
		sides, read := cells[path]
		if !read {
			sides = [2][]notebook.Cell{cellsAt(ctx, root, sha+"^", path), cellsAt(ctx, root, sha, path)}
			cells[path] = sides
		}
		for l := range hunks[h].Lines {
			line := &hunks[h].Lines[l]
			side, number := 1, line.NewNo
			if line.Kind == "del" {
				side, number = 0, line.OldNo
			}
			if cell, inCell, ok := notebook.CellAt(sides[side], number); ok {
				line.Cell = &protocol.CellPlace{Number: cell.Number, Line: inCell, Kind: cell.Kind, Language: cell.Lang}
			}
		}
	}
}

// cellsAt returns the cells of the notebook at path in revision, as its
// diff text numbers them; nil when the revision doesn't have it.
func cellsAt(ctx context.Context, root, revision, path string) []notebook.Cell {
	sha, err := git(ctx, root, "rev-parse", "--verify", "--quiet", revision+"^{commit}")
	if err != nil {
		return nil
	}
	raw, err := ReadFileAt(ctx, root, strings.TrimSpace(string(sha)), path)
	if err != nil {
		return nil
	}
	content, ok := trigram.DecodeBlob(raw)
	if !ok {
		return nil
	}
	nb, ok := notebook.Read(content, false)
	if !ok {
		return nil
	}
	return nb.Cells
}
