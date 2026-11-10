package cli

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
)

func TestMCPHandshakeDeadlineClosesUnresponsiveWindowAndReader(t *testing.T) {
	previous := handshakeWait
	handshakeWait = 20 * time.Millisecond
	t.Cleanup(func() { handshakeWait = previous })
	for _, readRequest := range []bool{false, true} {
		for _, reader := range []bool{false, true} {
			session, closed := silentMCPPeer(t, readRequest)
			done := make(chan error, 1)
			go func() {
				if reader {
					_, err := openReader(t.Context(), session, "/repo", Options{})
					done <- err
					return
				}
				_, err := session.hello(t.Context())
				done <- err
			}()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) || !closed.Load() {
					t.Errorf("reader=%v reading=%v: error=%v closed=%v", reader, readRequest, err, closed.Load())
				}
			case <-time.After(time.Second):
				t.Fatalf("reader=%v reading=%v: handshake ignored deadline", reader, readRequest)
			}
		}
	}
}

// silentMCPPeer either never reads the handshake or drains requests without
// responding, including ignoring RPC cancellation. Both must be bounded.
func silentMCPPeer(t *testing.T, readRequest bool) (*Session, *atomic.Bool) {
	t.Helper()
	peerIn, clientOut := io.Pipe()
	clientIn, peerOut := io.Pipe()
	conn := rpc.NewConn(clientIn, clientOut)
	ctx, cancel := context.WithCancel(t.Context())
	go func() { _ = conn.Serve(ctx) }()
	if readRequest {
		go func() { _, _ = io.Copy(io.Discard, peerIn) }()
	}
	var once sync.Once
	closed := &atomic.Bool{}
	session := &Session{conn: conn, close: func() {
		once.Do(func() {
			closed.Store(true)
			cancel()
			_ = peerIn.Close()
			_ = peerOut.Close()
			_ = clientIn.Close()
			_ = clientOut.Close()
		})
	}}
	t.Cleanup(session.Close)
	return session, closed
}
