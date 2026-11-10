package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MCPUsage explains both interactive setup and the stdio server mode.
const MCPUsage = `Connect an AI agent to Spelunk's read-only search tools.

Usage:
  spelunk mcp                         serve MCP on stdin/stdout in the client's current folder
  spelunk mcp --in DIR                serve MCP with DIR as the default search folder
  spelunk mcp config [--in DIR]       print MCP client JSON configuration
  spelunk mcp add claude              register with Claude Code for every project
  spelunk mcp --help                  show this help and exit

First run spelunk index --in DIR, or open the repository in VS Code with
Spelunk. MCP only reads indexes; it does not create them. The path argument
on each tool can select another absolute folder.

Paste the config command's mcpServers entry into a client that accepts
that format. For clients with a different settings format, use the same
command and args fields. Paths with spaces need no extra quoting in JSON.
`

func runMCP(ctx context.Context, env Env, args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		_, _ = fmt.Fprint(env.Stdout, MCPUsage)
		return exitFound
	}
	if len(args) > 0 && args[0] == "add" {
		return runMCPAdd(ctx, env, args[1:])
	}
	config := len(args) > 0 && args[0] == "config"
	if config {
		args = args[1:]
	}
	folder, err := mcpFolder(args, env.Cwd)
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk:", err, "Run spelunk mcp --help.")
		return exitError
	}
	if config {
		exe, pathErr := commandPath()
		if pathErr != nil {
			_, _ = fmt.Fprintln(env.Stderr, "spelunk:", pathErr)
			return exitError
		}
		if err := writeJSON(env.Stdout, mcpConfig(exe, folder)); err != nil {
			_, _ = fmt.Fprintln(env.Stderr, "spelunk: write MCP configuration:", err)
			return exitError
		}
		return exitFound
	}
	if folder != "" {
		env.Cwd = folder
	}
	return ServeMCP(ctx, env)
}

// mcpFolder resolves an explicit default folder. No argument keeps the
// client's working directory, so one registration can serve many projects.
func mcpFolder(args []string, cwd string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) != 2 || args[0] != "--in" || args[1] == "" {
		return "", errors.New("expected --in DIR or a supported MCP subcommand")
	}
	folder := args[1]
	if !filepath.IsAbs(folder) {
		folder = filepath.Join(cwd, folder)
	}
	folder, err := filepath.Abs(folder)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(folder)
	if err != nil {
		return "", fmt.Errorf("MCP folder: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("MCP folder %q is not a directory", folder)
	}
	return folder, nil
}

// mcpConfig uses argv entries, not shell quoting, so spaces and quotes in
// executable or repository paths survive a client's subprocess launch.
func mcpConfig(executable, folder string) map[string]any {
	args := []string{"mcp"}
	if folder != "" {
		args = append(args, "--in", folder)
	}
	return map[string]any{"mcpServers": map[string]any{"spelunk": map[string]any{"command": executable, "args": args}}}
}
