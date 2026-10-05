package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
)

// daemon runs serve on pipes until it returns an exit code.
type daemon struct {
	stdin  *io.PipeWriter
	stdout *io.PipeReader
	sig    chan os.Signal
	code   chan int
}

func startDaemon(t *testing.T) *daemon {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	d := &daemon{stdin: inW, stdout: outR, sig: make(chan os.Signal, 1), code: make(chan int, 1)}
	go func() { d.code <- serve(inR, outW, io.Discard, d.sig) }()
	t.Cleanup(func() {
		_ = inW.Close()  // ends the read loop; nothing to report
		_ = outR.Close() // unblocks any handler still writing
	})
	return d
}

func (d *daemon) send(t *testing.T, body string) {
	t.Helper()
	if err := rpc.WriteMessage(d.stdin, []byte(body)); err != nil {
		t.Fatal(err)
	}
}

// exitCode waits for serve to return, failing after within.
func (d *daemon) exitCode(t *testing.T, within time.Duration) int {
	t.Helper()
	select {
	case code := <-d.code:
		return code
	case <-time.After(within):
		t.Fatalf("daemon still running %v later, want it to exit", within)
		return -1
	}
}

func TestDaemonExitsWhenStdinClosesEvenIfNobodyReadsStdout(t *testing.T) {
	inputGrace = 100 * time.Millisecond
	t.Cleanup(func() { inputGrace = 2 * time.Second })
	d := startDaemon(t)
	// Responses pile up unread: the first write blocks every later one.
	for i := range 20 {
		d.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"index/status"}`, i))
	}
	_ = d.stdin.Close()
	if code := d.exitCode(t, 2*time.Second); code != 0 {
		t.Fatalf("exit code after stdin closed = %d, want 0", code)
	}
}

func TestDaemonAnswersWhatItReadBeforeStdinClosed(t *testing.T) {
	d := startDaemon(t)
	d.send(t, `{"jsonrpc":"2.0","id":1,"method":"index/status"}`)
	_ = d.stdin.Close()
	body, err := rpc.ReadMessage(bufio.NewReader(d.stdout))
	if err != nil {
		t.Fatalf("reading the answer after stdin closed: %v", err)
	}
	if want := `{"jsonrpc":"2.0","id":1,"result":{"repos":[]}}`; string(body) != want {
		t.Fatalf("answer = %s, want %s", body, want)
	}
	if code := d.exitCode(t, 3*time.Second); code != 0 {
		t.Fatalf("exit code after stdin closed = %d, want 0", code)
	}
}

func TestDaemonExitCodes(t *testing.T) {
	// @covers rpc:shutdown rpc:exit
	t.Run("framing_error_exits_1", func(t *testing.T) {
		d := startDaemon(t)
		_, _ = io.WriteString(d.stdin, "Content-Length: 10000000000\r\n\r\n")
		if code := d.exitCode(t, 2*time.Second); code != 1 {
			t.Fatalf("exit code after a 10 GB Content-Length = %d, want 1", code)
		}
	})
	t.Run("signal_exits_0", func(t *testing.T) {
		d := startDaemon(t)
		d.sig <- syscall.SIGTERM
		if code := d.exitCode(t, 3*time.Second); code != 0 {
			t.Fatalf("exit code after SIGTERM = %d, want 0", code)
		}
	})
	t.Run("shutdown_then_exit_exits_0", func(t *testing.T) {
		d := startDaemon(t)
		go func() { _, _ = io.Copy(io.Discard, d.stdout) }()
		d.send(t, `{"jsonrpc":"2.0","id":1,"method":"shutdown"}`)
		time.Sleep(50 * time.Millisecond) // a client waits for the answer before exit
		d.send(t, `{"jsonrpc":"2.0","method":"exit"}`)
		if code := d.exitCode(t, 3*time.Second); code != 0 {
			t.Fatalf("exit code after shutdown and exit = %d, want 0", code)
		}
	})
}
