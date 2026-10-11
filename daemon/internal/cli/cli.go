package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// Exit codes, as ripgrep and grep use them.
const (
	exitFound   = 0 // something matched
	exitNothing = 1 // nothing matched
	exitError   = 2 // a bad query, a folder with no index, a failure
)

// defaultCount is how many results search prints unless asked.
const defaultCount = 50

// Usage is the command's help.
const Usage = `Spelunk searches code and Git history from the index the Spelunk VS Code
extension builds. It never builds or changes an index itself.

Usage:
  spelunk search [options] QUERY...   search the repo that holds this folder
  spelunk status                      say which index answers here, and how fresh it is
  spelunk help query                  the query language, with examples
  spelunk mcp                         serve AI agents (Model Context Protocol, stdin/stdout)
  spelunk mcp add claude              add Spelunk to Claude Code for every project
  spelunk --version

Search options:
  -n, --count N    show up to N results (default 50)
  --json           one JSON object per result, then a summary object
  --vimgrep        path:line:column:text, for editors
  --in DIR         search the repo that holds DIR instead
  --timeout SECS   stop after SECS seconds and print what was found
  --               end of options: everything after is the query

Answers come from the VS Code window that has the folder open, when
Spelunk: Agents › Share This Window is on, so they include its latest
edits. Otherwise a background reader serves the saved index and re-reads
files changed since; it exits after 10 idle minutes.

Exit status: 0 when something matched, 1 when nothing did, 2 on an error.
`

// Env is what Main runs with, so tests can drive it.
type Env struct {
	Args           []string // after the program name
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Cwd            string
	Options        Options
	Now            func() time.Time
}

// Main runs the command and returns its exit code.
func Main(ctx context.Context, env Env) int {
	if env.Now == nil {
		env.Now = time.Now
	}
	if len(env.Args) == 0 {
		_, _ = fmt.Fprint(env.Stdout, Usage)
		return exitFound
	}
	switch env.Args[0] {
	case "search":
		return runSearch(ctx, env, env.Args[1:])
	case "status":
		return runStatus(ctx, env)
	case "help", "-h", "--help":
		if len(env.Args) > 1 && env.Args[1] == "query" {
			_, _ = fmt.Fprint(env.Stdout, query.Guide())
		} else {
			_, _ = fmt.Fprint(env.Stdout, Usage)
		}
		return exitFound
	case "mcp":
		if len(env.Args) > 1 && env.Args[1] == "add" {
			return runMCPAdd(ctx, env, env.Args[2:])
		}
		return ServeMCP(ctx, env)
	default:
		_, _ = fmt.Fprintf(env.Stderr, "spelunk: unknown command %q. Run spelunk help.\n", env.Args[0])
		return exitError
	}
}

// searchArgs are search's options and query.
type searchArgs struct {
	count   int
	format  Format
	folder  string
	timeout time.Duration
	query   string
}

// parseSearchArgs reads search's options. Anything else is the query, so
// a query word like -test or -file:vendor/ isn't taken for an option.
func parseSearchArgs(args []string, cwd string) (searchArgs, error) {
	parsed := searchArgs{count: defaultCount, folder: cwd}
	var words []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--":
			words = append(words, args[i+1:]...)
			i = len(args)
			continue
		case "--json":
			parsed.format = FormatJSON
			continue
		case "--vimgrep":
			parsed.format = FormatVimgrep
			continue
		case "-n", "--count", "--in", "--timeout":
		default:
			words = append(words, arg)
			continue
		}
		if i+1 >= len(args) {
			return parsed, fmt.Errorf("%s needs a value", arg)
		}
		i++
		if err := parsed.setOption(arg, args[i], cwd); err != nil {
			return parsed, err
		}
	}
	parsed.query = strings.TrimSpace(strings.Join(words, " "))
	if parsed.query == "" {
		return parsed, errors.New("search needs a query, such as spelunk search retryPolicy")
	}
	return parsed, nil
}

// setOption sets the option that takes a value.
func (parsed *searchArgs) setOption(name, value, cwd string) error {
	switch name {
	case "--in":
		parsed.folder = value
		if !filepath.IsAbs(value) {
			parsed.folder = filepath.Join(cwd, value)
		}
	case "--timeout":
		seconds, err := strconv.ParseFloat(value, 64)
		if err != nil || seconds <= 0 {
			return fmt.Errorf("--timeout needs a number of seconds, not %q", value)
		}
		parsed.timeout = time.Duration(seconds * float64(time.Second))
	default: // -n, --count
		count, err := strconv.Atoi(value)
		if err != nil || count < 1 {
			return fmt.Errorf("%s needs a positive number, not %q", name, value)
		}
		parsed.count = count
	}
	return nil
}

// connectFor connects for folder, explaining failures on standard error.
func connectFor(ctx context.Context, env Env, folder string) (*Session, int) {
	opts := env.Options
	if opts.Progress == nil {
		opts.Progress = func(message string) { _, _ = fmt.Fprintln(env.Stderr, message) }
	}
	session, err := Connect(ctx, filepath.Clean(folder), opts)
	switch {
	case errors.Is(err, ErrNotIndexed):
		_, _ = fmt.Fprintln(env.Stderr, "spelunk:", notIndexedText(err))
		return nil, exitError
	case err != nil:
		_, _ = fmt.Fprintln(env.Stderr, "spelunk:", err)
		return nil, exitError
	}
	return session, exitFound
}

// notIndexedText explains a NotIndexedError in a sentence or two.
func notIndexedText(err error) string {
	var notIndexed *NotIndexedError
	if !errors.As(err, &notIndexed) {
		return err.Error()
	}
	reason := notIndexed.Reason
	if reason == "" {
		reason = "Not indexed yet. Open this folder in VS Code with Spelunk to index it."
	}
	return notIndexed.Root + ": " + reason
}

// runSearch is spelunk search.
func runSearch(ctx context.Context, env Env, args []string) int {
	parsed, err := parseSearchArgs(args, env.Cwd)
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk:", err)
		return exitError
	}
	session, code := connectFor(ctx, env, parsed.folder)
	if session == nil {
		return code
	}
	defer session.Close()
	results, summary, err := search(ctx, session, parsed.query, SearchOptions{Count: parsed.count, TimeLimitMs: int(parsed.timeout / time.Millisecond)})
	if message, ok := QueryError(err); ok {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk:", message)
		_, _ = fmt.Fprintln(env.Stderr, "Run spelunk help query for the query language.")
		return exitError
	}
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk:", err)
		return exitError
	}
	if err := printResults(env.Stdout, parsed.format, results, session.Repo.Root, env.Cwd); err != nil {
		return exitError
	}
	if parsed.format == FormatJSON {
		_ = writeJSON(env.Stdout, summary)
	} else {
		_, _ = fmt.Fprintln(env.Stderr, summaryLine(&summary, env.Now()))
	}
	if summary.Total == 0 {
		return exitNothing
	}
	return exitFound
}

// search runs a query and converts what the daemon returns.
func search(ctx context.Context, session *Session, text string, opts SearchOptions) ([]Result, Summary, error) {
	result, items, err := session.Search(ctx, text, opts)
	if err != nil {
		return nil, Summary{}, err
	}
	results := make([]Result, 0, len(items))
	for i := range items {
		results = append(results, toResult(session.Repo.Name, &items[i]))
	}
	summary := Summary{Type: "summary", Total: result.Total, Shown: len(results), Truncated: result.Truncated, Ms: result.Ms, Source: "vscode"}
	if !session.Live {
		// The search may have re-read files changed since the save.
		session.refresh(ctx)
		summary.Source, summary.SavedAt, summary.Reread = "saved", session.Repo.SavedAt, session.Repo.Reread
	}
	return results, summary, nil
}

// runStatus is spelunk status.
func runStatus(ctx context.Context, env Env) int {
	session, code := connectFor(ctx, env, env.Cwd)
	if session == nil {
		return code
	}
	defer session.Close()
	repo := session.Repo
	answeredBy := "the background reader, from the saved index"
	if session.Live {
		answeredBy = "VS Code, live"
	}
	_, _ = fmt.Fprintf(env.Stdout, "Repo:      %s (%s)\n", repo.Name, repo.Root)
	_, _ = fmt.Fprintf(env.Stdout, "Answered:  by %s\n", answeredBy)
	if !session.Live {
		_, _ = fmt.Fprintf(env.Stdout, "Freshness: %s\n", sourceText("saved", repo.SavedAt, repo.Reread, env.Now()))
	}
	_, _ = fmt.Fprintf(env.Stdout, "Files:     %s\n", repo.Tree)
	_, _ = fmt.Fprintf(env.Stdout, "History:   %s\n", repo.History)
	if repo.Message != "" {
		_, _ = fmt.Fprintf(env.Stdout, "Note:      %s\n", repo.Message)
	}
	return exitFound
}

// Cwd is the current folder, for Env.
func Cwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "/"
	}
	return cwd
}
