package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/server"
)

// mcpInstructions tell an agent what Spelunk is for. They and the tool
// descriptions are all an agent knows about it, and agents reach for grep
// unless told when Spelunk does better.
const mcpInstructions = `Spelunk searches this repo's code and Git history from a local index built by VS Code or spelunk index.
Use spelunk_search for definitions by name, references, commits and the lines they added or removed,
past revisions, and repeated selective searches. Broad regexes can reach the search time limit.
Check the answer's shown/total counts, incomplete search notice and index freshness before treating it as complete.
Call spelunk_syntax once if you are unsure of the query language. Every tool only reads.`

// mcpDefaultLimit is how many results a search returns unless the agent asks.
const mcpDefaultLimit = 50

// mcpConcurrentCalls bounds work started by one agent process. Eight calls
// allow parallel exploration while preventing an unbounded connection and
// search burst from consuming the local indexer's CPU and memory.
const mcpConcurrentCalls = 8

// SearchInput is spelunk_search's arguments.
type SearchInput struct {
	Query string `json:"query" jsonschema:"a Spelunk query, such as: retry lang:go, symbol:ParseQuery, type:commit author:jane since:2w timeout"`
	Limit int    `json:"limit,omitempty" jsonschema:"the most results to return, even with count:all (default 50; maximum 50000)"`
	Path  string `json:"path,omitempty" jsonschema:"an absolute folder in the repo to search; default: the folder the agent runs in"`
}

// PreviewInput is spelunk_preview's arguments.
type PreviewInput struct {
	Ref          string `json:"ref" jsonschema:"the ref of a result, as spelunk_search lists it"`
	ContextLines int    `json:"contextLines,omitempty" jsonschema:"lines of context around a code result (default 10)"`
	Path         string `json:"path,omitempty" jsonschema:"the folder the search ran in, if it wasn't the default"`
}

// FolderInput is the arguments of the tools that only need a folder.
type FolderInput struct {
	Path string `json:"path,omitempty" jsonschema:"an absolute folder in the repo; default: the folder the agent runs in"`
}

// sessionConnector isolates daemon discovery from MCP transport and tool handling.
type sessionConnector func(context.Context, string, Options) (*Session, error)

// NewMCPServer is Spelunk's MCP server for agents in folder cwd.
func NewMCPServer(env Env) *mcp.Server {
	return mcpServerWithConnector(env, Connect)
}

func mcpServerWithConnector(env Env, connect sessionConnector) *mcp.Server {
	if env.Now == nil {
		env.Now = time.Now
	}
	handlers := &mcpTools{env: env, connect: connect, backendSlots: make(chan struct{}, mcpConcurrentCalls)}
	srv := mcp.NewServer(&mcp.Implementation{Name: "spelunk", Version: server.DaemonVersion}, &mcp.ServerOptions{Instructions: mcpInstructions})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(bool)}
	mcp.AddTool(srv, &mcp.Tool{
		Name: "spelunk_search",
		Description: "Search this repo's code and Git history with Spelunk's index. Use it for " +
			"definitions (symbol:), references (ref:), commits and diffs (type:commit, type:added, type:removed, author:, since:), " +
			"past revisions (at:) and repeated selective queries. Broad regexes can reach the search time limit. Returns path:line:text lines, or sha date author subject for " +
			"commits, each with a ref for spelunk_preview. Indicates incomplete searches and saved-index freshness.",
		Annotations: readOnly,
	}, handlers.search)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "spelunk_syntax",
		Description: "The Spelunk query language: words, phrases, regexes, OR, NOT, groups, and every operator, with examples.",
		Annotations: readOnly,
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return textResult(query.Guide(), false), nil, nil
	})
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "spelunk_preview",
		Description: "The code around a spelunk_search result, or a commit's message and changed lines, by the result's ref.",
		Annotations: readOnly,
	}, handlers.preview)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "spelunk_status",
		Description: "Which repo Spelunk searches here, whether a VS Code window answers live or a saved index does, and how fresh it is.",
		Annotations: readOnly,
	}, handlers.status)
	return srv
}

// ServeMCP serves agents on standard input and output until they leave.
func ServeMCP(ctx context.Context, env Env) int {
	if env.Stderr == nil {
		env.Stderr = os.Stderr
	}
	input := env.Stdin
	if input == nil {
		input = os.Stdin
	}
	output := env.Stdout
	if output == nil {
		output = os.Stdout
	}
	reader, ok := input.(io.ReadCloser)
	if !ok {
		reader = io.NopCloser(input)
	}
	transport := &mcp.IOTransport{Reader: reader, Writer: keepOpenWriter{output}}
	if err := NewMCPServer(env).Run(ctx, transport); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk mcp:", err)
		return exitError
	}
	return exitFound
}

// keepOpenWriter lets the MCP transport finish without closing the caller's output.
type keepOpenWriter struct{ io.Writer }

// Close leaves the stream owned by the caller open.
func (keepOpenWriter) Close() error { return nil }

// textResult is a tool result of one text.
func textResult(text string, isError bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: isError}
}

// searchText is a search's answer for an agent: a summary line, then one
// line per result with its ref, paths relative to the repo's root, and
// for a line inside the definitions the query asked for, the one it is in.
func searchText(session *Session, results []Result, summary *Summary, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repo %s (%s): %s\n", session.Repo.Name, session.Repo.Root, summaryLine(summary, now))
	for i := range results {
		var line strings.Builder
		_ = printResults(&line, FormatText, results[i:i+1], session.Repo.Root, session.Repo.Root)
		text := strings.TrimRight(line.String(), "\n")
		if in := results[i].Within; in != nil {
			text += fmt.Sprintf("  [in %s %s]", in.Kind, in.Name)
		}
		fmt.Fprintf(&b, "%s  [ref %s]\n", text, results[i].Ref)
	}
	if summary.Total == 0 {
		if summary.Truncated {
			b.WriteString("No results found before the search stopped. Narrow the query and retry.\n")
		} else {
			b.WriteString("No results. Check the query with spelunk_syntax, or widen it.\n")
		}
	}
	return b.String()
}

// previewText is a preview for an agent: numbered lines, or a commit.
func previewText(preview *protocol.Preview) string {
	var b strings.Builder
	if preview.Kind == protocol.PreviewKindCommit {
		fmt.Fprintf(&b, "commit %s\nAuthor: %s\nDate: %s\n\n%s\n", preview.SHA, preview.Author, preview.At, preview.Subject)
		if preview.Body != "" {
			b.WriteString("\n" + preview.Body + "\n")
		}
		for _, file := range preview.Files {
			fmt.Fprintf(&b, "  %s\n", file.Path)
		}
		return b.String()
	}
	fmt.Fprintf(&b, "%s\n", preview.Path)
	for i, line := range preview.Lines {
		number := preview.FirstLine + i
		marker := " "
		if number >= preview.FocusLine && number <= max(preview.FocusLine, preview.FocusEndLine) {
			marker = ">"
		}
		fmt.Fprintf(&b, "%s%6d  %s\n", marker, number, line)
	}
	return b.String()
}

// claudeAddArgs are Claude Code's own command for adding Spelunk to every
// project.
func claudeAddArgs(exe string) []string {
	return []string{"mcp", "add", "--scope", "user", "spelunk", "--", exe, "mcp"}
}

// runMCPAdd is spelunk mcp add claude: it runs Claude Code's own command.
func runMCPAdd(ctx context.Context, env Env, args []string) int {
	if len(args) != 1 || args[0] != "claude" {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk: usage: spelunk mcp add claude")
		return exitError
	}
	exe, err := commandPath()
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk:", err)
		return exitError
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk: Claude Code's claude command isn't on the PATH. Install Claude Code, then run this again.")
		return exitError
	}
	argv := claudeAddArgs(exe)
	_, _ = fmt.Fprintf(env.Stdout, "Running: claude %s\n", strings.Join(argv, " "))
	cmd := exec.CommandContext(ctx, claude, argv...) //nolint:gosec // G204: Claude Code's own command, with our path
	cmd.Stdout, cmd.Stderr = env.Stdout, env.Stderr
	if err := cmd.Run(); err != nil {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk: claude mcp add failed:", err)
		return exitError
	}
	_, _ = fmt.Fprintln(env.Stdout, "Added Spelunk to Claude Code for every project.")
	return exitFound
}

// writeJSON writes v as one line of JSON.
func writeJSON(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// commandPath is how other programs should run this command: spelunk on
// the PATH when it is there (the link the extension installs, which
// survives extension updates), otherwise this executable.
func commandPath() (string, error) {
	if path, err := exec.LookPath("spelunk"); err == nil {
		if abs, err := filepath.Abs(path); err == nil {
			return abs, nil
		}
	}
	return os.Executable()
}
