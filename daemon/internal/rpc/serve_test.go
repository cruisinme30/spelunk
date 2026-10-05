package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"
)

// rawPeer drives a serving Conn byte by byte, as a misbehaving host could.
type rawPeer struct {
	in     *io.PipeWriter // what the Conn reads
	out    *bufio.Reader  // what the Conn writes
	served chan error     // Serve's result
}

// newRawPeer serves conn's handlers (registered by setup) over pipes the
// test writes and reads directly.
func newRawPeer(t *testing.T, setup func(*Conn)) *rawPeer {
	t.Helper()
	connIn, peerOut := io.Pipe()
	peerIn, connOut := io.Pipe()
	conn := NewConn(connIn, connOut)
	setup(conn)
	peer := &rawPeer{in: peerOut, out: bufio.NewReader(peerIn), served: make(chan error, 1)}
	go func() { peer.served <- conn.Serve(context.Background()) }()
	t.Cleanup(func() {
		_ = peerOut.Close() // ends Serve's read loop; nothing to report
		go func() { _, _ = io.Copy(io.Discard, peerIn) }()
		<-peer.served
	})
	return peer
}

// send writes body as one framed message.
func (p *rawPeer) send(t *testing.T, body string) {
	t.Helper()
	if err := WriteMessage(p.in, []byte(body)); err != nil {
		t.Fatal(err)
	}
}

// receive reads the next message the Conn writes, failing after 2s.
func (p *rawPeer) receive(t *testing.T) map[string]any {
	t.Helper()
	type read struct {
		body []byte
		err  error
	}
	got := make(chan read, 1)
	go func() {
		body, err := ReadMessage(p.out)
		got <- read{body, err}
	}()
	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("reading the Conn's output: %v", r.err)
		}
		var m map[string]any
		if err := json.Unmarshal(r.body, &m); err != nil {
			t.Fatalf("Conn wrote %s: %v", r.body, err)
		}
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no message from the Conn within 2s")
		return nil
	}
}

// errorCode returns a response's error code, or 0 for a success.
func errorCode(m map[string]any) int {
	e, ok := m["error"].(map[string]any)
	if !ok {
		return 0
	}
	return int(e["code"].(float64))
}

func TestServeAnswersMessagesThatAreNotJSONRPCAndCarriesOn(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
		wantID   any // the id the response carries
	}{
		{name: "invalid_json", body: `{nope`, wantCode: CodeParseError},
		{name: "bare_number", body: `42`, wantCode: CodeInvalidRequest},
		{name: "bare_null", body: `null`, wantCode: CodeInvalidRequest},
		{name: "batch", body: `[{"jsonrpc":"2.0","id":1,"method":"echo"}]`, wantCode: CodeInvalidRequest},
		{name: "empty_batch", body: `[]`, wantCode: CodeInvalidRequest},
		{name: "empty_object", body: `{}`, wantCode: CodeInvalidRequest},
		{name: "object_id", body: `{"jsonrpc":"2.0","id":{"a":1},"method":"echo"}`, wantCode: CodeInvalidRequest},
		{name: "boolean_id", body: `{"jsonrpc":"2.0","id":true,"method":"echo"}`, wantCode: CodeInvalidRequest},
		{name: "method_of_the_wrong_type", body: `{"jsonrpc":"2.0","id":1,"method":5}`, wantCode: CodeInvalidRequest},
		{name: "cancel_sent_as_a_request", body: `{"jsonrpc":"2.0","id":7,"method":"$/cancelRequest","params":{"id":1}}`, wantCode: CodeMethodNotFound, wantID: 7.0},
		{name: "notification_method_sent_as_a_request", body: `{"jsonrpc":"2.0","id":8,"method":"note"}`, wantCode: CodeMethodNotFound, wantID: 8.0},
	}
	peer := newRawPeer(t, func(c *Conn) {
		c.Handle("echo", func(_ context.Context, params json.RawMessage) (any, error) { return params, nil })
		c.OnNotify("note", func(json.RawMessage) {})
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer.send(t, tt.body)
			got := peer.receive(t)
			if errorCode(got) != tt.wantCode || got["id"] != tt.wantID {
				t.Fatalf("response to %s = %v, want code %d with id %v", tt.body, got, tt.wantCode, tt.wantID)
			}
			// The connection still answers.
			peer.send(t, `{"jsonrpc":"2.0","id":"after","method":"echo","params":1}`)
			if got := peer.receive(t); got["id"] != "after" || got["result"] != 1.0 {
				t.Fatalf("echo after %s = %v, want result 1 for id after", tt.body, got)
			}
		})
	}
}

func TestServeAnswersEveryValidIDForm(t *testing.T) {
	peer := newRawPeer(t, func(c *Conn) {
		c.Handle("echo", func(_ context.Context, params json.RawMessage) (any, error) { return params, nil })
	})
	for _, id := range []string{`null`, `"abc"`, `0`, `-3`, `1.5`, `""`} {
		peer.send(t, `{"jsonrpc":"2.0","id":`+id+`,"method":"echo","params":"x","unknown":{"extra":1}}`)
		got := peer.receive(t)
		encoded, _ := json.Marshal(got["id"])
		if string(encoded) != id || got["result"] != "x" {
			t.Fatalf("response to id %s = %v, want result x with the same id", id, got)
		}
	}
	// A notification for a request method has no response; the next
	// request's response is the next message.
	peer.send(t, `{"jsonrpc":"2.0","method":"echo","params":"ignored"}`)
	peer.send(t, `{"jsonrpc":"2.0","id":9,"method":"echo","params":"next"}`)
	if got := peer.receive(t); got["id"] != 9.0 {
		t.Fatalf("message after a notification for echo = %v, want the response to id 9", got)
	}
}

func TestDuplicateInFlightIDIsRefusedAndTheFirstStaysCancellable(t *testing.T) {
	// @covers rpc:$/cancelRequest
	started := make(chan struct{}, 2)
	peer := newRawPeer(t, func(c *Conn) {
		c.Handle("slow", func(ctx context.Context, _ json.RawMessage) (any, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		})
	})
	peer.send(t, `{"jsonrpc":"2.0","id":1,"method":"slow"}`)
	<-started
	peer.send(t, `{"jsonrpc":"2.0","id":1,"method":"slow"}`)
	if got := peer.receive(t); errorCode(got) != CodeInvalidRequest || got["id"] != 1.0 {
		t.Fatalf("second request with in-flight id 1 = %v, want InvalidRequest", got)
	}
	peer.send(t, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":1}}`)
	if got := peer.receive(t); errorCode(got) != CodeRequestCancelled || got["id"] != 1.0 {
		t.Fatalf("response after cancelling id 1 = %v, want RequestCancelled", got)
	}
	// Once answered, the id is free again.
	peer.send(t, `{"jsonrpc":"2.0","id":1,"method":"slow"}`)
	<-started
	peer.send(t, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":1}}`)
	if got := peer.receive(t); errorCode(got) != CodeRequestCancelled {
		t.Fatalf("reused id 1 after cancel = %v, want RequestCancelled", got)
	}
}

func TestPanickingNotificationHandlerDoesNotEndTheConnection(t *testing.T) {
	peer := newRawPeer(t, func(c *Conn) {
		c.OnNotify("boom", func(json.RawMessage) { panic("notification handler bug") })
		c.Handle("echo", func(_ context.Context, params json.RawMessage) (any, error) { return params, nil })
	})
	peer.send(t, `{"jsonrpc":"2.0","method":"boom"}`)
	peer.send(t, `{"jsonrpc":"2.0","id":2,"method":"echo","params":"alive"}`)
	if got := peer.receive(t); got["result"] != "alive" {
		t.Fatalf("echo after a panicking notification = %v, want alive", got)
	}
}

func TestServeEndsOnFramingErrors(t *testing.T) {
	for name, wire := range map[string]string{
		"header_without_body": "Content-Length: 10\r\n\r\n",
		"truncated_body":      "Content-Length: 100\r\n\r\n{\"jsonrpc\"",
		"huge_length":         "Content-Length: 10000000000\r\n\r\n",
		"missing_length":      "Foo: bar\r\n\r\n{}",
	} {
		t.Run(name, func(t *testing.T) {
			connIn, peerOut := io.Pipe()
			conn := NewConn(connIn, io.Discard)
			served := make(chan error, 1)
			go func() { served <- conn.Serve(context.Background()) }()
			go func() {
				_, _ = io.WriteString(peerOut, wire)
				_ = peerOut.Close()
			}()
			select {
			case err := <-served:
				if err == nil {
					t.Fatalf("Serve(%q) = nil, want a framing error", wire)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("Serve(%q) still running 2s after the input ended", wire)
			}
		})
	}
}

// failingWriter fails every write after the first n bytes.
type failingWriter struct {
	mu   sync.Mutex
	left int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) > w.left {
		n := w.left
		w.left = 0
		return n, errors.New("broken pipe")
	}
	w.left -= len(p)
	return len(p), nil
}

func TestWriteFailureStopsServeAndNothingIsWrittenAfterIt(t *testing.T) {
	connIn, peerOut := io.Pipe()
	out := &failingWriter{left: 10} // fails inside the first response's header
	conn := NewConn(connIn, out)
	conn.Handle("echo", func(_ context.Context, params json.RawMessage) (any, error) { return params, nil })
	served := make(chan error, 1)
	go func() { served <- conn.Serve(context.Background()) }()
	defer func() { _ = peerOut.Close() }()
	go func() {
		for i := range 5 {
			_ = WriteMessage(peerOut, fmt.Appendf(nil, `{"jsonrpc":"2.0","id":%d,"method":"echo"}`, i))
		}
	}()
	select {
	case err := <-served:
		if err == nil {
			t.Fatal("Serve after a failed write = nil, want the write error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve still running 2s after its output failed")
	}
	if err := conn.Notify("late", nil); err == nil {
		t.Fatal("Notify after a failed write = nil, want the write error")
	}
}

func TestConcurrentCallsAndCancelsLeaveNoGoroutinesBehind(t *testing.T) {
	// @covers rpc:$/cancelRequest
	before := runtime.NumGoroutine()
	func() {
		server, client := connectedPair(t)
		server.Handle("work", func(ctx context.Context, params json.RawMessage) (any, error) {
			var n int
			_ = json.Unmarshal(params, &n)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(n%5) * time.Millisecond):
				return n, nil
			}
		})
		var wg sync.WaitGroup
		for i := range 500 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				switch i % 4 {
				case 0:
					cancel() // cancelled before it is sent
				case 1:
					time.AfterFunc(time.Duration(i%3)*time.Millisecond, cancel)
				}
				var got int
				err := client.Call(ctx, "work", i, &got)
				if err == nil && got != i {
					t.Errorf("Call(work, %d) = %d, want %d", i, got, i)
				}
				// Cancels for ids that are unknown, finished or malformed.
				_ = client.Notify("$/cancelRequest", map[string]any{"id": 1_000_000 + i})
				_ = client.Notify("$/cancelRequest", map[string]any{"id": i})
				_ = client.Notify("$/cancelRequest", nil)
			}()
		}
		wg.Wait()
	}()
	// Every handler has answered; only the pair's two Serve loops remain
	// until the test's cleanup closes the pipes.
	waitForGoroutines(t, before+2)
}

// waitForGoroutines fails unless the goroutine count drops to at most
// limit within 2s.
func waitForGoroutines(t *testing.T, limit int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > limit {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines still running, want at most %d:\n%s", runtime.NumGoroutine(), limit, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}
