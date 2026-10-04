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
//	UNIFIED_SEARCH_TRACE=<file>  append every JSON-RPC message to <file>, one JSON object per line
//	UNIFIED_SEARCH_NOW=<RFC3339> freeze the clock (relative dates in tests)
package main

import (
	"context"
	"encoding/json"
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
	os.Exit(run())
}

// run serves JSON-RPC until the host says exit, closes stdin, or a signal
// arrives, and returns the process exit code. Deferred cleanup (the trace
// file) runs before main exits.
func run() int {
	conn := rpc.NewConn(os.Stdin, os.Stdout)
	if path := os.Getenv("UNIFIED_SEARCH_TRACE"); path != "" {
		closeTrace, err := traceTo(conn, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unified-search-daemon: trace disabled: %v\n", err)
		} else {
			defer closeTrace()
		}
	}
	srv := server.New(conn, optionsFromEnv())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	served := make(chan error, 1)
	go func() { served <- conn.Serve(ctx) }()

	select {
	case code := <-srv.Exited():
		return code
	case err := <-served: // stdin closed: the host is gone
		if err != nil {
			fmt.Fprintln(os.Stderr, "unified-search-daemon:", err)
			return 1
		}
		return 0
	case <-signals:
		return 0
	}
}

// traceEntry is one line of the UNIFIED_SEARCH_TRACE file.
type traceEntry struct {
	Time      string          `json:"t"`
	Direction string          `json:"dir"`
	Message   json.RawMessage `json:"msg"`
}

// traceTo appends every message on conn to the file at path.
func traceTo(conn *rpc.Conn, path string) (closeTrace func(), err error) {
	traceFile, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // G304: the developer chose this path in UNIFIED_SEARCH_TRACE
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	encoder := json.NewEncoder(traceFile)
	var failed sync.Once
	conn.Trace = func(direction string, body []byte) {
		mu.Lock()
		defer mu.Unlock()
		entry := traceEntry{Time: time.Now().UTC().Format(time.RFC3339Nano), Direction: direction, Message: body}
		if err := encoder.Encode(entry); err != nil {
			failed.Do(func() { fmt.Fprintf(os.Stderr, "unified-search-daemon: trace write failed: %v\n", err) })
		}
	}
	closeTrace = func() {
		if err := traceFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "unified-search-daemon: closing trace: %v\n", err)
		}
	}
	return closeTrace, nil
}

// optionsFromEnv reads UNIFIED_SEARCH_NOW.
func optionsFromEnv() server.Options {
	value := os.Getenv("UNIFIED_SEARCH_NOW")
	if value == "" {
		return server.Options{}
	}
	frozen, err := time.Parse(time.RFC3339, value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unified-search-daemon: ignoring UNIFIED_SEARCH_NOW: %v\n", err)
		return server.Options{}
	}
	return server.Options{Now: func() time.Time { return frozen }}
}
