// Unified-search-daemon is the search back end of the Unified Search VS Code
// extension. The extension host starts one per window and talks to it with
// JSON-RPC 2.0 on stdin and stdout (Contract 3 in docs/dev/implementation-plan.md).
//
// Usage:
//
//	unified-search-daemon            serve JSON-RPC on stdin/stdout
//	unified-search-daemon --version  print the version and exit
//
// Environment, for tests and debugging:
//
//	UNIFIED_SEARCH_TRACE=<file>  append every JSON-RPC message to <file>
//	UNIFIED_SEARCH_NOW=<RFC3339> freeze the clock (relative dates in tests)
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
	"github.com/cruisinme30/unified-search/daemon/internal/server"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(server.DaemonVersion)
		return
	}
	conn := rpc.NewConn(os.Stdin, os.Stdout)
	if path := os.Getenv("UNIFIED_SEARCH_TRACE"); path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			defer f.Close()
			var mu sync.Mutex
			conn.Trace = func(dir string, raw []byte) {
				mu.Lock()
				defer mu.Unlock()
				fmt.Fprintf(f, "{\"t\":%q,\"dir\":%q,\"msg\":%s}\n", time.Now().UTC().Format(time.RFC3339Nano), dir, raw)
			}
		}
	}
	opts := server.Options{}
	if v := os.Getenv("UNIFIED_SEARCH_NOW"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			opts.Now = func() time.Time { return t }
		}
	}
	s := server.New(conn, opts)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	done := make(chan error, 1)
	go func() { done <- conn.Serve(ctx) }()

	select {
	case code := <-s.Exited():
		os.Exit(code)
	case err := <-done: // stdin closed: the host is gone
		if err != nil {
			fmt.Fprintln(os.Stderr, "unified-search-daemon:", err)
			os.Exit(1)
		}
	case <-sigs:
	}
}
