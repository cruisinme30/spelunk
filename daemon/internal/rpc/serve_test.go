package rpc

import (
	"context"
	"io"
	"testing"
	"time"
)

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
