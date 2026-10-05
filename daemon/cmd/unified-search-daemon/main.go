// Unified-search-daemon is the search back end of the Unified Search VS Code
// extension. The extension host starts one per window and talks to it with
// JSON-RPC 2.0 on stdin and stdout; the methods are defined in protocol/protocol.schema.json.
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
	"io"
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

// run serves JSON-RPC on stdin and stdout until the host says exit,
// closes stdin, or a signal arrives, and returns the process exit code.
// Deferred cleanup (the trace file) runs before main exits.
func run() int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	return serve(os.Stdin, os.Stdout, os.Stderr, signals)
}

// inputGrace is how long the daemon waits, once stdin has ended, for
// requests still running to answer. A host that closed stdin may never
// read stdout again, and a handler blocked writing to it would otherwise
// keep the daemon alive forever. A variable so tests can shorten it.
var inputGrace = 2 * time.Second

// serve is run with its streams and signals passed in, so tests can drive it.
func serve(stdin io.Reader, stdout, stderr io.Writer, signals <-chan os.Signal) int {
	input := &endSignallingReader{reader: stdin, ended: make(chan struct{})}
	conn := rpc.NewConn(input, stdout)
	if path := os.Getenv("UNIFIED_SEARCH_TRACE"); path != "" {
		closeTrace, err := traceTo(conn, path)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "unified-search-daemon: trace disabled: %v\n", err)
		} else {
			defer closeTrace()
		}
	}
	srv := server.New(conn, optionsFromEnv())
	// However the daemon ends, index work stops (within the shutdown grace)
	// before the process does.
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- conn.Serve(ctx) }()
	servedCode := func(err error) int {
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "unified-search-daemon:", err)
			return 1
		}
		return 0
	}

	select {
	case code := <-srv.Exited():
		return code
	case err := <-served:
		return servedCode(err)
	case <-input.ended: // stdin closed: the host is gone
		select {
		case code := <-srv.Exited():
			return code
		case err := <-served:
			return servedCode(err)
		case <-time.After(inputGrace):
			return 0
		}
	case <-signals:
		return 0
	}
}

// endSignallingReader closes ended when its reader first fails (usually
// io.EOF), so serve learns that stdin is gone even while Serve waits for
// handlers to finish.
type endSignallingReader struct {
	reader io.Reader
	once   sync.Once
	ended  chan struct{}
}

// Read implements io.Reader.
func (r *endSignallingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err != nil {
		r.once.Do(func() { close(r.ended) })
	}
	return n, err
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
