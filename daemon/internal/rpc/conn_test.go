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

// connectedPair returns two Conns wired to each other through pipes, both
// serving until the test ends.
func connectedPair(t *testing.T) (server, client *Conn) {
	t.Helper()
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	server = NewConn(serverIn, serverOut)
	client = NewConn(clientIn, clientOut)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	go func() { _ = server.Serve(ctx); done <- struct{}{} }()
	go func() { _ = client.Serve(ctx); done <- struct{}{} }()
	t.Cleanup(func() {
		cancel()
		clientOut.Close()
		serverOut.Close()
		<-done
		<-done
	})
	return server, client
}

func wantCode(t *testing.T, call string, err error, want int) {
	t.Helper()
	var rpcErr *Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != want {
		t.Fatalf("%s error = %v, want code %d", call, err, want)
	}
}

func TestWriteMessageThenReadMessageRoundTrips(t *testing.T) {
	var wire bytes.Buffer
	if err := WriteMessage(&wire, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if got, want := wire.String(), "Content-Length: 7\r\n\r\n{\"a\":1}"; got != want {
		t.Fatalf("WriteMessage wrote %q, want %q", got, want)
	}
	body, err := ReadMessage(bufio.NewReader(&wire))
	if err != nil || string(body) != `{"a":1}` {
		t.Fatalf("ReadMessage = %q, %v; want {\"a\":1}, nil", body, err)
	}
}

func TestRequestsAndNotifications(t *testing.T) {
	server, client := connectedPair(t)
	server.Handle("echo", func(_ context.Context, params json.RawMessage) (any, error) {
		var v map[string]any
		_ = json.Unmarshal(params, &v)
		return v, nil
	})
	received := make(chan string, 1)
	server.OnNotify("ping", func(params json.RawMessage) { received <- string(params) })

	t.Run("call_returns_the_result", func(t *testing.T) {
		var got map[string]any
		if err := client.Call(context.Background(), "echo", map[string]any{"x": 1.0}, &got); err != nil {
			t.Fatal(err)
		}
		if got["x"] != 1.0 {
			t.Fatalf("Call(echo, {x:1}) = %v, want map[x:1]", got)
		}
	})
	t.Run("notification_reaches_its_handler", func(t *testing.T) {
		if err := client.Notify("ping", 7); err != nil {
			t.Fatal(err)
		}
		if got := <-received; got != "7" {
			t.Fatalf("ping handler got %s, want 7", got)
		}
	})
	t.Run("unknown_method_returns_method_not_found", func(t *testing.T) {
		wantCode(t, "Call(nope)", client.Call(context.Background(), "nope", nil, nil), CodeMethodNotFound)
	})
}

// @covers rpc:$/cancelRequest
func TestCancellingACallCancelsTheHandler(t *testing.T) {
	server, client := connectedPair(t)
	started := make(chan struct{})
	server.Handle("slow", func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() { errs <- client.Call(ctx, "slow", nil, nil) }()
	<-started
	cancel()
	select {
	case err := <-errs:
		wantCode(t, "Call(slow) after cancel", err, CodeRequestCancelled)
	case <-time.After(2 * time.Second):
		t.Fatal("Call(slow) still waiting 2s after cancel, want RequestCancelled")
	}
}

func TestCancelledCallGivesUpWhenThePeerNeverAnswers(t *testing.T) {
	cancelGrace = 50 * time.Millisecond
	t.Cleanup(func() { cancelGrace = 2 * time.Second })
	server, client := connectedPair(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	server.Handle("stuck", func(context.Context, json.RawMessage) (any, error) {
		<-release // ignores cancellation entirely
		return nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := client.Call(ctx, "stuck", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call(stuck) error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Call(stuck) took %v, want about 70ms", elapsed)
	}
}

func TestHandlerPanicBecomesInternalError(t *testing.T) {
	server, client := connectedPair(t)
	server.Handle("boom", func(context.Context, json.RawMessage) (any, error) { panic("x") })
	wantCode(t, "Call(boom)", client.Call(context.Background(), "boom", nil, nil), CodeInternalError)
}
