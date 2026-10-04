package server

// Test helpers shared by this package's test files.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

// testClient talks to a Server over in-process pipes.
type testClient struct {
	server *Server
	conn   *rpc.Conn
}

// newTestClient starts a Server and a client connected to it, both
// stopped when the test ends.
func newTestClient(t *testing.T) *testClient {
	t.Helper()
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	serverConn := rpc.NewConn(serverIn, serverOut)
	clientConn := rpc.NewConn(clientIn, clientOut)
	srv := New(serverConn, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = serverConn.Serve(ctx) }()
	go func() { _ = clientConn.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = clientOut.Close() // ends the peers' read loops; nothing to report
		_ = serverOut.Close()
	})
	return &testClient{server: srv, conn: clientConn}
}

// call makes a request and decodes its result into out (which may be nil),
// giving up after 10s.
func (c *testClient) call(method string, params, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.conn.Call(ctx, method, params, out)
}

// mustInitialize starts a session on roots, with the index in a temp
// directory, and waits until every root is indexed.
func (c *testClient) mustInitialize(t *testing.T, roots ...protocol.Root) {
	t.Helper()
	settings := DefaultSettings()
	settings.Location = t.TempDir()
	params := protocol.InitializeParams{Protocol: protocol.Version, Roots: roots, Settings: settings}
	if err := c.call(protocol.MethodInitialize, params, nil); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		ready := true
		for _, status := range c.server.index.Status() {
			ready = ready && status.Tree == protocol.IndexStateReady
		}
		if ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("roots not indexed after 5s: %+v", c.server.index.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// batchLog records every search/batch item the client receives.
type batchLog struct {
	mu       sync.Mutex
	items    []protocol.ResultItem
	searchOf []string // the search ID of each item
}

// collectBatches starts recording the search/batch notifications client
// receives.
func collectBatches(client *testClient) *batchLog {
	log := &batchLog{}
	client.conn.OnNotify(protocol.MethodSearchBatch, func(raw json.RawMessage) {
		var batch protocol.SearchBatchParams
		_ = json.Unmarshal(raw, &batch)
		log.mu.Lock()
		defer log.mu.Unlock()
		for _, item := range batch.Items {
			log.items = append(log.items, item)
			log.searchOf = append(log.searchOf, batch.SearchID)
		}
	})
	return log
}

// all returns every item received so far.
func (l *batchLog) all() []protocol.ResultItem {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]protocol.ResultItem(nil), l.items...)
}

// of returns the items of one search.
func (l *batchLog) of(searchID string) []protocol.ResultItem {
	l.mu.Lock()
	defer l.mu.Unlock()
	var items []protocol.ResultItem
	for i, item := range l.items {
		if l.searchOf[i] == searchID {
			items = append(items, item)
		}
	}
	return items
}

// mustWriteFile writes content to path, failing the test on error.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// wantRPCCode fails the test unless err is an RPC error with code want.
func wantRPCCode(t *testing.T, call string, err error, want int) {
	t.Helper()
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != want {
		t.Fatalf("%s error = %v, want code %d", call, err, want)
	}
}
