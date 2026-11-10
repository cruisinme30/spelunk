package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/indexer"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/server"
)

// indexArgs are the explicitly requested index operation, separate from the
// read-only search, status and MCP commands.
type indexArgs struct {
	folder  string
	rebuild bool
}

func parseIndexArgs(args []string, cwd string) (indexArgs, error) {
	parsed := indexArgs{folder: cwd}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--rebuild":
			parsed.rebuild = true
		case "--in":
			i++
			if i == len(args) || args[i] == "" {
				return parsed, errors.New("--in needs a directory")
			}
			parsed.folder = args[i]
			if !filepath.IsAbs(parsed.folder) {
				parsed.folder = filepath.Join(cwd, parsed.folder)
			}
		default:
			return parsed, fmt.Errorf("unknown index option %q", args[i])
		}
	}
	info, err := os.Stat(parsed.folder)
	if err != nil || !info.IsDir() {
		return parsed, fmt.Errorf("folder not found: %s", parsed.folder)
	}
	parsed.folder = filepath.Clean(parsed.folder)
	return parsed, nil
}

// runIndex builds or refreshes saved indexes directly, without VS Code or a
// socket. All searches and MCP tools continue to use the read-only connections.
func runIndex(ctx context.Context, env Env, args []string) int {
	parsed, err := parseIndexArgs(args, env.Cwd)
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk:", err)
		return exitError
	}
	settings := env.Options.Config.Settings
	if settings.Location == "" {
		settings = server.DefaultSettings()
	}
	path := indexer.SavedRoot(settings.Location, parsed.folder)
	root := protocol.Root{ID: path, Path: path, Name: filepath.Base(path)}
	_, _ = fmt.Fprintln(env.Stderr, "Indexing", path+"…")
	started := time.Now()
	status, err := indexer.BuildOnce(ctx, settings, root, parsed.rebuild)
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, "spelunk:", err)
		return exitError
	}
	_, _ = fmt.Fprintf(env.Stdout, "Indexed %s · files %s · history %s · %s\n", path, status.Tree, status.History, time.Since(started).Round(time.Millisecond))
	return exitFound
}
