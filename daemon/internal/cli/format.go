package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// Format is how search prints results.
type Format int

// The formats: ripgrep's path:line:text, Vim's path:line:column:text, and
// one JSON object per line.
const (
	FormatText Format = iota
	FormatVimgrep
	FormatJSON
)

// Result is a search result as the command and agents see it: a stable,
// documented shape, with no internal references.
type Result struct {
	Type   string `json:"type"` // file, line, symbol or commit
	Repo   string `json:"repo"`
	Path   string `json:"path,omitempty"` // relative to the root
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"` // 1-based, in bytes
	Text   string `json:"text,omitempty"`
	Kind   string `json:"kind,omitempty"` // a symbol's kind
	Name   string `json:"name,omitempty"` // a symbol's name
	// Within is, for a line found inside the definitions a query asked
	// for (kind:function ref:Parse), the definition that holds it.
	Within  *Within `json:"within,omitempty"`
	SHA     string  `json:"sha,omitempty"`
	Date    string  `json:"date,omitempty"` // a commit's date, YYYY-MM-DD
	Author  string  `json:"author,omitempty"`
	Subject string  `json:"subject,omitempty"`
	// Ref names the result for a preview (the MCP preview tool).
	Ref string `json:"ref,omitempty"`
}

// Within is the definition a line result sits in.
type Within struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Line int    `json:"line"` // its first line
}

// Summary ends a search's output: totals and where the answer came from.
type Summary struct {
	Type      string `json:"type"` // "summary"
	Total     int    `json:"total"`
	Shown     int    `json:"shown"`
	Truncated bool   `json:"truncated"`
	Ms        int    `json:"ms"`
	// Source is "vscode" when a VS Code window answered, "saved" when the
	// background reader did, from the saved index.
	Source  string `json:"source"`
	SavedAt string `json:"savedAt,omitempty"`
	Reread  int    `json:"reread,omitempty"`
}

// toResult converts a daemon result item.
func toResult(repo string, item *protocol.ResultItem) Result {
	result := Result{Type: item.Kind, Repo: repo, Path: item.Path, Line: item.Line}
	switch item.Kind {
	case protocol.ResultItemKindLine:
		result.Text = item.Text
		if len(item.Hits) > 0 {
			result.Column = byteColumn(item.Text, item.Hits[0].Start)
		}
		result.Ref = item.Ref
		if in := item.Within; in != nil {
			result.Within = &Within{Kind: in.Kind, Name: in.Name, Line: in.Line}
		}
	case protocol.ResultItemKindSymbol:
		result.Kind, result.Name, result.Ref = item.SymbolKind, item.Name, item.Ref
	case protocol.ResultItemKindCommit:
		result.SHA, result.Subject, result.Author, result.Ref = item.SHA, item.Subject, item.Author.Name, item.Ref
		if len(item.At) >= len("2006-01-02") {
			result.Date = item.At[:len("2006-01-02")]
		}
	}
	return result
}

// byteColumn is the 1-based byte column of the UTF-16 offset in text.
func byteColumn(text string, offset int) int {
	units := 0
	for i, r := range text {
		if units >= offset {
			return i + 1
		}
		units += len(utf16.Encode([]rune{r}))
	}
	return len(text) + 1
}

// printResults writes results in format. Paths are relative to cwd, so a
// terminal or editor can open them; root is where they are relative to.
func printResults(w io.Writer, format Format, results []Result, root, cwd string) error {
	encoder := json.NewEncoder(w)
	for i := range results {
		result := results[i]
		if format == FormatJSON {
			result.Ref = ""
			if err := encoder.Encode(result); err != nil {
				return err
			}
			continue
		}
		path := displayPath(root, result.Path, cwd)
		var line string
		switch result.Type {
		case protocol.ResultItemKindCommit:
			sha := result.SHA
			if len(sha) > 10 {
				sha = sha[:10]
			}
			line = fmt.Sprintf("%s %s %s  %s", sha, result.Date, result.Author, result.Subject)
		case protocol.ResultItemKindFile:
			line = path
		case protocol.ResultItemKindSymbol:
			line = fmt.Sprintf("%s:%d:%s %s", path, result.Line, result.Kind, result.Name)
		default:
			text := strings.TrimRight(result.Text, "\r\n")
			if format == FormatVimgrep {
				line = fmt.Sprintf("%s:%d:%d:%s", path, result.Line, max(result.Column, 1), text)
			} else {
				line = fmt.Sprintf("%s:%d:%s", path, result.Line, text)
			}
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// displayPath is a root-relative path made relative to cwd when cwd is
// inside the root, as ripgrep prints paths.
func displayPath(root, path, cwd string) string {
	abs := filepath.Join(root, filepath.FromSlash(path))
	if rel, err := filepath.Rel(cwd, abs); err == nil && filepath.IsLocal(rel) {
		return rel
	}
	return abs
}

// summaryLine is the human summary of a search, for standard error.
func summaryLine(summary *Summary, now time.Time) string {
	parts := []string{fmt.Sprintf("%d results", summary.Total)}
	if summary.Total == 1 {
		parts[0] = "1 result"
	}
	if summary.Shown < summary.Total {
		parts[0] += fmt.Sprintf(" (showing %d)", summary.Shown)
	}
	parts = append(parts, fmt.Sprintf("%d ms", summary.Ms), sourceText(summary.Source, summary.SavedAt, summary.Reread, now))
	if summary.Truncated {
		parts = append(parts, "stopped at the time limit, so there may be more")
	}
	return strings.Join(parts, " · ")
}

// sourceText says where an answer came from, and how fresh it is.
func sourceText(source, savedAt string, reread int, now time.Time) string {
	if source == "vscode" {
		return "live, from VS Code"
	}
	text := "saved index"
	if at, err := time.Parse(time.RFC3339, savedAt); err == nil {
		text = "index saved " + ago(now.Sub(at))
	}
	if reread == 1 {
		text += ", 1 changed file re-read"
	} else if reread > 1 {
		text += fmt.Sprintf(", %d changed files re-read", reread)
	}
	return text
}

// ago says how long ago d was, roughly.
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "min") + " ago"
	case d < 48*time.Hour:
		return plural(int(d/time.Hour), "h") + " ago"
	default:
		return plural(int(d/(24*time.Hour)), "day") + " ago"
	}
}

// plural is n with its unit; a word unit gets an s after 1.
func plural(n int, unit string) string {
	if unit == "day" && n != 1 {
		unit = "days"
	}
	return fmt.Sprintf("%d %s", n, unit)
}
