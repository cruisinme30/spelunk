package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

func pipePair(t *testing.T) (server, client *Conn, stop func()) {
	t.Helper()
	sr, cw := io.Pipe()
	cr, sw := io.Pipe()
	server = NewConn(sr, sw)
	client = NewConn(cr, cw)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	go func() { _ = server.Serve(ctx); done <- struct{}{} }()
	go func() { _ = client.Serve(ctx); done <- struct{}{} }()
	return server, client, func() {
		cancel()
		cw.Close()
		sw.Close()
		<-done
		<-done
	}
}

func TestFraming(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "Content-Length: 7\r\n\r\n{\"a\":1}" {
		t.Fatalf("framing = %q", got)
	}
	body, err := ReadMessage(bufio.NewReader(&buf))
	if err != nil || string(body) != `{"a":1}` {
		t.Fatalf("read = %q, %v", body, err)
	}
}

func TestCallAndNotify(t *testing.T) {
	server, client, stop := pipePair(t)
	defer stop()
	server.Handle("echo", func(ctx context.Context, p json.RawMessage) (any, error) {
		var v map[string]any
		_ = json.Unmarshal(p, &v)
		return v, nil
	})
	got := make(chan string, 1)
	server.OnNotify("ping", func(p json.RawMessage) { got <- string(p) })

	var out map[string]any
	if err := client.Call(context.Background(), "echo", map[string]any{"x": 1.0}, &out); err != nil {
		t.Fatal(err)
	}
	if out["x"] != 1.0 {
		t.Fatalf("echo = %v", out)
	}
	if err := client.Notify("ping", 7); err != nil {
		t.Fatal(err)
	}
	if v := <-got; v != "7" {
		t.Fatalf("notify = %s", v)
	}
	var rerr *Error
	if err := client.Call(context.Background(), "nope", nil, nil); !errors.As(err, &rerr) || rerr.Code != CodeMethodNotFound {
		t.Fatalf("want method not found, got %v", err)
	}
}

// @covers rpc:$/cancelRequest
func TestCancel(t *testing.T) {
	server, client, stop := pipePair(t)
	defer stop()
	started := make(chan struct{})
	server.Handle("slow", func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- client.Call(ctx, "slow", nil, nil) }()
	<-started
	cancel()
	select {
	case err := <-errc:
		var rerr *Error
		if !errors.As(err, &rerr) || rerr.Code != CodeRequestCancelled {
			t.Fatalf("want RequestCancelled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not finish the request")
	}
}

func TestHandlerPanicBecomesInternalError(t *testing.T) {
	server, client, stop := pipePair(t)
	defer stop()
	server.Handle("boom", func(context.Context, json.RawMessage) (any, error) { panic("x") })
	var rerr *Error
	if err := client.Call(context.Background(), "boom", nil, nil); !errors.As(err, &rerr) || rerr.Code != CodeInternalError {
		t.Fatalf("got %v", err)
	}
}
