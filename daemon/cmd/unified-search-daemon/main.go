// Command unified-search-daemon answers Contract 3 over stdin/stdout.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"unifiedsearch/daemon/rpc"
	"unifiedsearch/daemon/server"
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
